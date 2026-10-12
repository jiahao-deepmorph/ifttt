package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ifttt/internal/testutil"
)

func region(label, body, target string) string {
	open := "// LINT.IfChange"
	if label != "" {
		open += "(" + label + ")"
	}
	return open + "\n" + body + "\n// LINT.ThenChange(" + target + ")\n"
}

func TestCheckFixtures(t *testing.T) {
	source := region("a", "old", "b.go:b")
	target := region("b", "one\ntwo", "")
	tests := []struct {
		name          string
		before, after map[string]string
		deleted       []string
		findings      int
		errors        int
	}{
		{"unchanged", nil, nil, nil, 0, 0},
		{"empty source deleted", map[string]string{"a.go": "// LINT.IfChange(a)\n// LINT.ThenChange(b.go:b)\n"}, nil, []string{"a.go"}, 0, 0},
		{"empty source markers replaced", map[string]string{"a.go": "// LINT.IfChange(a)\n// LINT.ThenChange(b.go:b)\n"}, map[string]string{"a.go": "// LINT.IfChange(a) \n// LINT.ThenChange(b.go)\n"}, nil, 0, 0},
		{"empty target markers changed", map[string]string{"b.go": "// LINT.IfChange(b)\n// LINT.ThenChange()\n"}, map[string]string{"a.go": region("a", "new", "b.go:b"), "b.go": "// LINT.IfChange(b) \n// LINT.ThenChange() \n"}, nil, 1, 0},
		{"missing counterpart", nil, map[string]string{"a.go": region("a", "new", "b.go:b")}, nil, 1, 0},
		{"both bodies edited", nil, map[string]string{"a.go": region("a", "new", "b.go:b"), "b.go": region("b", "changed", "")}, nil, 0, 0},
		{"target deletion only", nil, map[string]string{"a.go": region("a", "new", "b.go:b"), "b.go": region("b", "two", "")}, nil, 0, 0},
		{"target outside region", nil, map[string]string{"a.go": region("a", "new", "b.go:b"), "b.go": target + "outside\n"}, nil, 1, 0},
		{"source outside region", nil, map[string]string{"a.go": source + "outside\n"}, nil, 0, 0},
		{"target marker only", nil, map[string]string{"a.go": region("a", "new", "b.go:b"), "b.go": strings.Replace(target, "// LINT.ThenChange", "//  LINT.ThenChange", 1)}, nil, 1, 0},
		{"source marker only", nil, map[string]string{"a.go": region("a", "old", "b.go")}, nil, 0, 0},
		{"whitespace triggers", nil, map[string]string{"a.go": region("a", "old ", "b.go:b")}, nil, 1, 0},
		{"comment triggers", map[string]string{"a.go": region("a", "// old", "b.go:b")}, map[string]string{"a.go": region("a", "// new", "b.go:b")}, nil, 1, 0},
		{"new source exempt", nil, map[string]string{"new.go": region("new", "body", "b.go:b")}, nil, 0, 0},
		{"new wrapper exempt", map[string]string{"a.go": "old\n"}, map[string]string{"a.go": region("a", "new", "b.go:b")}, nil, 0, 0},
		{"remove source file", nil, nil, []string{"a.go"}, 1, 0},
		{"remove source region", nil, map[string]string{"a.go": "outside\n"}, nil, 1, 0},
		{"remove markers only", nil, map[string]string{"a.go": "old\n"}, nil, 0, 0},
		{"remove both files", nil, nil, []string{"a.go", "b.go"}, 0, 0},
		{"deleted target stale link", nil, map[string]string{"a.go": region("a", "new", "b.go:b")}, []string{"b.go"}, 0, 1},
		{"untouched incoming links not validated", nil, nil, []string{"b.go"}, 0, 0},
		{"missing file marker only", nil, map[string]string{"a.go": region("a", "old", "missing.go")}, nil, 0, 1},
		{"missing label marker only", nil, map[string]string{"a.go": region("a", "old", "b.go:missing")}, nil, 0, 1},
		{"new broken region validated", nil, map[string]string{"new.go": region("new", "body", "missing.go")}, nil, 0, 1},
		{"old malformed annotation repair", map[string]string{"a.go": "// LINT.IfChange(a)\nold\n"}, map[string]string{"a.go": source}, nil, 0, 0},
		{"delete old malformed annotation", map[string]string{"a.go": "// LINT.IfChange(a)\nold\n"}, nil, []string{"a.go"}, 0, 0},
		{"bad target syntax", nil, map[string]string{"a.go": region("a", "old", "../b.go")}, nil, 0, 1},
		{"whole file target", map[string]string{"a.go": region("a", "old", "b.go")}, map[string]string{"a.go": region("a", "new", "b.go"), "b.go": target + "outside\n"}, nil, 0, 0},
		{"retain old obligations", nil, map[string]string{"a.go": region("a", "new", "")}, nil, 1, 0},
		{"union of obligations", nil, map[string]string{"a.go": region("a", "new", "c.go"), "c.go": "x\n"}, nil, 1, 0},
		{"source insertion only", nil, map[string]string{"a.go": region("a", "old\ninserted", "b.go:b")}, nil, 1, 0},
		{"source body emptied", nil, map[string]string{"a.go": "// LINT.IfChange(a)\n// LINT.ThenChange(b.go:b)\n"}, nil, 1, 0},
		{"source insertion into empty body", map[string]string{"a.go": "// LINT.IfChange(a)\n// LINT.ThenChange(b.go:b)\n"}, map[string]string{"a.go": region("a", "new", "b.go:b")}, nil, 1, 0},
		{"unlabeled insertion", map[string]string{"a.go": region("", "old", "b.go:b")}, map[string]string{"a.go": region("", "old\ninserted", "b.go:b")}, nil, 1, 0},
		{"rename label insertion", nil, map[string]string{"a.go": region("renamed", "old\ninserted", "b.go:b")}, nil, 1, 0},
		{"rename label only", nil, map[string]string{"a.go": region("renamed", "old", "b.go:b")}, nil, 0, 0},
		{"region shifted by prefix", nil, map[string]string{"a.go": "prefix\n" + source}, nil, 0, 0},
		{"same file missing", map[string]string{"a.go": region("a", "old", ":b") + target}, map[string]string{"a.go": region("a", "new", ":b") + target}, nil, 1, 0},
		{"same file satisfied", map[string]string{"a.go": region("a", "old", ":b") + target}, map[string]string{"a.go": region("a", "new", ":b") + region("b", "changed", "")}, nil, 0, 0},
		{"one directional", nil, map[string]string{"b.go": region("b", "changed", "")}, nil, 0, 0},
		{"cycle", map[string]string{"b.go": region("b", "old", "a.go:a")}, map[string]string{"a.go": region("a", "new", "b.go:b"), "b.go": region("b", "new", "a.go:a")}, nil, 0, 0},
		{"per target findings", map[string]string{"a.go": region("a", "old", "b.go, c.go, d.go"), "c.go": "c", "d.go": "d"}, map[string]string{"a.go": region("a", "new", "b.go, c.go, d.go"), "b.go": "new", "c.go": "new"}, nil, 1, 0},
		{"dedup both versions", nil, map[string]string{"a.go": region("a", "new", "b.go:b,b.go:b")}, nil, 1, 0},
		{"rename source is deletion", nil, map[string]string{"renamed.go": source}, []string{"a.go"}, 1, 0},
		{"rename target stale link", nil, map[string]string{"a.go": region("a", "new", "b.go:b"), "renamed.go": target}, []string{"b.go"}, 0, 1},
		{"binary whole target", map[string]string{"a.go": region("a", "old", "b.bin"), "b.bin": "\x00a"}, map[string]string{"a.go": region("a", "new", "b.bin"), "b.bin": "\x00b"}, nil, 0, 0},
		{"malformed unchanged labeled target", map[string]string{"b.go": "// LINT.IfChange(b)\n"}, map[string]string{"a.go": source + "outside\n"}, nil, 0, 2},
		{"unmatched new unlabeled before existing", map[string]string{"a.go": region("", "old", "b.go:b")}, map[string]string{"a.go": region("", "brand new", "b.go:b") + region("", "old", "b.go:b")}, nil, 0, 0},
		{"continuation changes only", map[string]string{"a.go": "// LINT.IfChange\nold\n// LINT.ThenChange(\n// b.go:b\n// )\n"}, map[string]string{"a.go": "// LINT.IfChange\nold\n// LINT.ThenChange(\n// b.go\n// )\n"}, nil, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			files := map[string]string{"a.go": source, "b.go": target}
			for k, v := range tt.before {
				files[k] = v
			}
			r := testutil.NewRepo(t, files)
			r.Write(tt.after)
			r.Remove(tt.deleted...)
			r.Commit()
			result, err := Check(Options{Dir: r.Dir, Base: r.Base})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Findings) != tt.findings || len(result.Errors) != tt.errors {
				t.Fatalf("got %d findings, %d errors; want %d, %d\n%+v", len(result.Findings), len(result.Errors), tt.findings, tt.errors, result)
			}
			for _, f := range result.Findings {
				if f.Start > f.End || f.Start < 1 {
					t.Fatalf("invalid finding coordinates: %+v", f)
				}
			}
		})
	}
}

func TestMergeBaseAndExplicitHead(t *testing.T) {
	r := testutil.NewRepo(t, map[string]string{"a.go": region("a", "old", "b.go"), "b.go": "old"})
	r.Git("switch", "-qc", "feature")
	r.Write(map[string]string{"a.go": region("a", "new", "b.go")})
	head := r.Commit()
	r.Git("switch", "-q", "main")
	r.Write(map[string]string{"b.go": "base branch only"})
	r.Commit()
	result, err := Check(Options{Dir: r.Dir, Base: "main", Head: head})
	if err != nil || len(result.Findings) != 1 {
		t.Fatalf("%+v, %v", result, err)
	}
	// A dirty worktree must not satisfy the committed diff.
	r.Write(map[string]string{"a.go": "dirty", "b.go": "dirty"})
	result, err = Check(Options{Dir: r.Dir, Base: "main", Head: head})
	if err != nil || len(result.Findings) != 1 {
		t.Fatalf("%+v, %v", result, err)
	}
}

func TestPathsAndFileTypes(t *testing.T) {
	t.Run("unusual paths", func(t *testing.T) {
		name := "dir/space [x] ü.go"
		r := testutil.NewRepo(t, map[string]string{name: region("a", "old", "b.go"), "b.go": "old"})
		r.Write(map[string]string{name: region("a", "new", "b.go")})
		r.Commit()
		result, err := Check(Options{Dir: r.Dir, Base: r.Base})
		if err != nil || len(result.Findings) != 1 || result.Findings[0].Path != name {
			t.Fatalf("%+v, %v", result, err)
		}
	})
	t.Run("symlinks are not targets", func(t *testing.T) {
		r := testutil.NewRepo(t, map[string]string{"a.go": region("a", "old", "b.go"), "b.go": "old"})
		if err := os.Symlink("b.go", filepath.Join(r.Dir, "link.go")); err != nil {
			t.Skip(err)
		}
		r.Write(map[string]string{"a.go": region("a", "old", "link.go")})
		r.Commit()
		result, err := Check(Options{Dir: r.Dir, Base: r.Base})
		if err != nil || len(result.Errors) != 1 {
			t.Fatalf("%+v, %v", result, err)
		}
	})
}

func TestInvalidRefs(t *testing.T) {
	r := testutil.NewRepo(t, nil)
	for _, base := range []string{"", "missing", "--help"} {
		if _, err := Check(Options{Dir: r.Dir, Base: base}); err == nil {
			t.Fatalf("accepted %q", base)
		}
	}
}

func TestSubdirectoryAndDiffConfig(t *testing.T) {
	r := testutil.NewRepo(t, map[string]string{"sub/a.go": "before\n" + region("a", "unchanged", "b.go") + "after\n", "b.go": "old"})
	r.Git("config", "diff.interHunkContext", "100")
	r.Write(map[string]string{"sub/a.go": "BEFORE\n" + region("a", "unchanged", "b.go") + "AFTER\n"})
	r.Commit()
	result, err := Check(Options{Dir: filepath.Join(r.Dir, "sub"), Base: r.Base})
	if err != nil || len(result.Findings) != 0 || len(result.Errors) != 0 {
		t.Fatalf("%+v, %v", result, err)
	}
	r.Write(map[string]string{"sub/a.go": region("a", "changed", "b.go")})
	r.Commit()
	result, err = Check(Options{Dir: filepath.Join(r.Dir, "sub"), Base: r.Base})
	if err != nil || len(result.Findings) != 1 {
		t.Fatalf("%+v, %v", result, err)
	}
}
