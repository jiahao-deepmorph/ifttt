package lint

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

type span struct{ start, count int }

func (s span) intersects(r Region) bool {
	return r.Close > r.Open+1 && s.count > 0 && s.start < r.Close && s.start+s.count > r.Open+1
}

type hunk struct{ old, new span }
type change struct{ hunks []hunk }

func (c change) touched(r Region, old bool) bool {
	for _, h := range c.hunks {
		s := h.new
		if old {
			s = h.old
		}
		if s.intersects(r) {
			return true
		}
	}
	return false
}

// mapLine maps an unchanged old line to its head location.
func (c change) mapLine(line int) (int, bool) {
	delta := 0
	for _, h := range c.hunks {
		if h.old.count == 0 {
			if line > h.old.start {
				delta += h.new.count
			}
			continue
		}
		if line >= h.old.start && line < h.old.start+h.old.count {
			return 0, false
		}
		if line >= h.old.start+h.old.count {
			delta += h.new.count - h.old.count
		}
	}
	return line + delta, true
}

type treeEntry struct{ mode, kind, oid string }
type gitRepo struct{ dir string }

func (g gitRepo) run(args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-c", "core.quotePath=false", "-c", "diff.relative=false"}, args...)...)
	cmd.Dir = g.dir
	cmd.Env = append(os.Environ(), "GIT_LITERAL_PATHSPECS=1", "GIT_OPTIONAL_LOCKS=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return output, nil
}

func (g gitRepo) resolve(ref string) (string, error) {
	out, err := g.run("rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	return strings.TrimSpace(string(out)), err
}

func (g gitRepo) tree(ref string) (map[string]treeEntry, error) {
	out, err := g.run("ls-tree", "-r", "-z", "--full-tree", ref)
	if err != nil {
		return nil, err
	}
	entries := map[string]treeEntry{}
	for _, record := range bytes.Split(out, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		metadata, name, ok := strings.Cut(string(record), "\t")
		parts := strings.Fields(metadata)
		if !ok || len(parts) != 3 {
			return nil, fmt.Errorf("invalid git ls-tree record")
		}
		entries[name] = treeEntry{parts[0], parts[1], parts[2]}
	}
	return entries, nil
}

var hunkPattern = regexp.MustCompile(`(?m)^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

func parseHunks(diff []byte) change {
	c := change{}
	for _, m := range hunkPattern.FindAllSubmatch(diff, -1) {
		values := [4]int{0, 1, 0, 1}
		for i := range values {
			if len(m[i+1]) > 0 {
				values[i], _ = strconv.Atoi(string(m[i+1]))
			}
		}
		c.hunks = append(c.hunks, hunk{span{values[0], values[1]}, span{values[2], values[3]}})
	}
	return c
}

func diffArgs(base, head string) []string {
	return []string{"diff", "--no-renames", "--no-ext-diff", "--no-textconv", "--no-color", "--diff-algorithm=myers", "--no-indent-heuristic", "--inter-hunk-context=0", base, head}
}

func (g gitRepo) changes(base, head string) (map[string]change, []string, error) {
	out, err := g.run(append(diffArgs(base, head), "--name-only", "-z", "--")...)
	if err != nil {
		return nil, nil, err
	}
	changes := map[string]change{}
	var names []string
	for _, name := range bytes.Split(out, []byte{0}) {
		if len(name) == 0 {
			continue
		}
		filename := string(name)
		diff, err := g.run(append(diffArgs(base, head), "--unified=0", "--text", "--", filename)...)
		if err != nil {
			return nil, nil, err
		}
		names = append(names, filename)
		changes[filename] = parseHunks(diff)
	}
	return changes, names, nil
}
