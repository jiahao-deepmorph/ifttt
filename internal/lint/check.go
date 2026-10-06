package lint

import (
	"fmt"
	"sort"
	"strings"
)

type Options struct {
	Dir  string
	Base string
	Head string
}

type parsedFile struct {
	regions []Region
	errors  []Diagnostic
}

type snapshot struct {
	git   gitRepo
	tree  map[string]treeEntry
	cache map[string]parsedFile
}

func (s *snapshot) regular(filename string) bool {
	e, ok := s.tree[filename]
	return ok && e.kind == "blob" && (e.mode == "100644" || e.mode == "100755")
}

func (s *snapshot) load(filename string) (parsedFile, error) {
	if p, ok := s.cache[filename]; ok {
		return p, nil
	}
	if !s.regular(filename) {
		return parsedFile{}, nil
	}
	out, err := s.git.run("cat-file", "blob", s.tree[filename].oid)
	if err != nil {
		return parsedFile{}, err
	}
	regions, diagnostics := Parse(filename, string(out))
	p := parsedFile{regions, diagnostics}
	s.cache[filename] = p
	return p, nil
}

// Check compares committed snapshots against their merge base. It never reads
// worktree file contents or executes repository hooks or diff drivers.
func Check(options Options) (Result, error) {
	var result Result
	g := gitRepo{options.Dir}
	root, err := g.run("rev-parse", "--show-toplevel")
	if err != nil {
		return result, err
	}
	g.dir = strings.TrimSpace(string(root))
	if options.Base == "" {
		return result, fmt.Errorf("base ref is required")
	}
	if options.Head == "" {
		options.Head = "HEAD"
	}
	base, err := g.resolve(options.Base)
	if err != nil {
		return result, err
	}
	head, err := g.resolve(options.Head)
	if err != nil {
		return result, err
	}
	out, err := g.run("merge-base", base, head)
	if err != nil {
		return result, fmt.Errorf("compute merge base (fetch full history with fetch-depth: 0): %w", err)
	}
	base = strings.TrimSpace(string(out))
	oldTree, err := g.tree(base)
	if err != nil {
		return result, err
	}
	newTree, err := g.tree(head)
	if err != nil {
		return result, err
	}
	old := snapshot{g, oldTree, map[string]parsedFile{}}
	new := snapshot{g, newTree, map[string]parsedFile{}}
	changes, filenames, err := g.changes(base, head)
	if err != nil {
		return result, err
	}
	seenErrors := map[Diagnostic]bool{}
	addErrors := func(errors ...Diagnostic) {
		for _, d := range errors {
			if !seenErrors[d] {
				result.Errors = append(result.Errors, d)
				seenErrors[d] = true
			}
		}
	}
	// Validate all head annotations in changed files, even marker-only edits.
	// Targets are inspected one hop; their outgoing links are not traversed.
	for _, filename := range filenames {
		p, err := new.load(filename)
		if err != nil {
			return result, err
		}
		addErrors(p.errors...)
		for _, r := range p.regions {
			for _, target := range r.Targets {
				if !new.regular(target.Path) {
					addErrors(Diagnostic{filename, r.Close, "missing target file (or not a regular file): " + target.Path})
					continue
				}
				if target.Label == "" {
					continue
				}
				p, err := new.load(target.Path)
				if err != nil {
					return result, err
				}
				addErrors(p.errors...)
				if findLabel(p.regions, target.Label) == nil {
					addErrors(Diagnostic{filename, r.Close, "missing target label: " + target.String()})
				}
			}
		}
	}
	for _, filename := range filenames {
		a, err := old.load(filename)
		if err != nil {
			return result, err
		}
		b, err := new.load(filename)
		if err != nil {
			return result, err
		}
		c := changes[filename]
		pairs := matchRegions(a.regions, b.regions, c)
		for i, before := range a.regions {
			var after *Region
			if j, ok := pairs[i]; ok {
				after = &b.regions[j]
			}
			if !c.touched(before, true) && (after == nil || !c.touched(*after, false)) {
				continue
			}
			// Keep old obligations when a body changes, and include new ones on
			// the matched region. Marker edits alone never trigger anything.
			targets := append([]Target(nil), before.Targets...)
			if after != nil {
				targets = append(targets, after.Targets...)
			}
			seen := map[Target]bool{}
			for _, target := range targets {
				if seen[target] {
					continue
				}
				seen[target] = true
				satisfied, err := targetSatisfied(target, changes, &old, &new)
				if err != nil {
					return result, err
				}
				if !satisfied {
					f := Finding{Path: filename, Label: before.Label, Start: before.Open + 1, End: before.Close - 1, Target: target, Deleted: !new.regular(filename), BaseLines: true}
					if after != nil {
						if after.Close > after.Open+1 {
							f.Label, f.Start, f.End, f.BaseLines = after.Label, after.Open+1, after.Close-1, false
						}
						for _, h := range c.hunks {
							if h.new.intersects(*after) {
								f.Line = max(h.new.start, after.Open+1)
								break
							}
						}
					}
					result.Findings = append(result.Findings, f)
				}
			}
		}
	}
	sort.Slice(result.Errors, func(i, j int) bool {
		a, b := result.Errors[i], result.Errors[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Message < b.Message
	})
	sort.Slice(result.Findings, func(i, j int) bool {
		a, b := result.Findings[i], result.Findings[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Start != b.Start {
			return a.Start < b.Start
		}
		return a.Target.String() < b.Target.String()
	})
	return result, nil
}

func findLabel(regions []Region, label string) *Region {
	for i := range regions {
		if regions[i].Label == label {
			return &regions[i]
		}
	}
	return nil
}

func targetSatisfied(target Target, changes map[string]change, old, new *snapshot) (bool, error) {
	c, changed := changes[target.Path]
	if !changed {
		return false, nil
	}
	if target.Label == "" || (old.regular(target.Path) && !new.regular(target.Path)) {
		return true, nil
	}
	for _, version := range []struct {
		snap *snapshot
		old  bool
	}{{old, true}, {new, false}} {
		p, err := version.snap.load(target.Path)
		if err != nil {
			return false, err
		}
		r := findLabel(p.regions, target.Label)
		if r != nil && c.touched(*r, version.old) {
			return true, nil
		}
	}
	return false, nil
}

// Match labels first, then surviving marker/body lines. New unmatched regions
// are exempt; old unmatched regions still fire if their body was removed.
func matchRegions(old, new []Region, c change) map[int]int {
	pairs, used := map[int]int{}, map[int]bool{}
	for i, a := range old {
		if a.Label == "" {
			continue
		}
		for j, b := range new {
			if !used[j] && a.Label == b.Label {
				pairs[i], used[j] = j, true
				break
			}
		}
	}
	for i, a := range old {
		if _, ok := pairs[i]; ok {
			continue
		}
		best, score := -1, 0
		for j, b := range new {
			if used[j] {
				continue
			}
			s := 0
			if line, ok := c.mapLine(a.Open); ok && line == b.Open {
				s += 4
			}
			if line, ok := c.mapLine(a.Close); ok && line == b.Close {
				s += 4
			}
			for line := a.Open + 1; line < a.Close; line++ {
				if mapped, ok := c.mapLine(line); ok && mapped > b.Open && mapped < b.Close {
					s++
					break
				}
			}
			if s > score {
				best, score = j, s
			}
		}
		if best >= 0 {
			pairs[i], used[best] = best, true
		}
	}
	return pairs
}
