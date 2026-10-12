package lint

import (
	"reflect"
	"testing"

	"ifttt/internal/testutil"
)

// Assert complete findings, rather than just their count: a warning on the
// wrong region or against the wrong target is also a detection failure.
func TestExactFindings(t *testing.T) {
	before := region("a", "one\ntwo", "b.go:b")
	target := region("b", "target", "")
	tests := []struct {
		name    string
		after   string
		deleted bool
		want    []Finding
	}{
		{"replacement", region("a", "ONE\ntwo", "b.go:b"), false, []Finding{{Path: "a.go", Label: "a", Start: 2, End: 3, Line: 2, Target: Target{"b.go", "b"}}}},
		{"deletion only", region("a", "two", "b.go:b"), false, []Finding{{Path: "a.go", Label: "a", Start: 2, End: 2, Target: Target{"b.go", "b"}}}},
		{"empty body", "// LINT.IfChange(a)\n// LINT.ThenChange(b.go:b)\n", false, []Finding{{Path: "a.go", Label: "a", Start: 2, End: 3, BaseLines: true, Target: Target{"b.go", "b"}}}},
		{"deleted file", "", true, []Finding{{Path: "a.go", Label: "a", Start: 2, End: 3, Deleted: true, BaseLines: true, Target: Target{"b.go", "b"}}}},
		{"removed region", "outside\n", false, []Finding{{Path: "a.go", Label: "a", Start: 2, End: 3, BaseLines: true, Target: Target{"b.go", "b"}}}},
		{"shifted replacement", "prefix\n" + region("a", "one\nTWO", "b.go:b"), false, []Finding{{Path: "a.go", Label: "a", Start: 3, End: 4, Line: 4, Target: Target{"b.go", "b"}}}},
		{"renamed label", region("renamed", "one\nTWO", "b.go:b"), false, []Finding{{Path: "a.go", Label: "renamed", Start: 2, End: 3, Line: 3, Target: Target{"b.go", "b"}}}},
		{"old and new targets", region("a", "ONE\ntwo", "c.go"), false, []Finding{
			{Path: "a.go", Label: "a", Start: 2, End: 3, Line: 2, Target: Target{"b.go", "b"}},
			{Path: "a.go", Label: "a", Start: 2, End: 3, Line: 2, Target: Target{"c.go", ""}},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := testutil.NewRepo(t, map[string]string{"a.go": before, "b.go": target, "c.go": "unchanged\n"})
			if tt.deleted {
				r.Remove("a.go")
			} else {
				r.Write(map[string]string{"a.go": tt.after})
			}
			r.Commit()
			got, err := Check(Options{Dir: r.Dir, Base: r.Base})
			if err != nil || len(got.Errors) != 0 || !reflect.DeepEqual(got.Findings, tt.want) {
				t.Fatalf("got %+v, %v; want findings %+v", got, err, tt.want)
			}
		})
	}
}

func TestValidationScope(t *testing.T) {
	for _, labeled := range []bool{false, true} {
		t.Run(map[bool]string{false: "whole file", true: "labeled"}[labeled], func(t *testing.T) {
			target := "b.go"
			if labeled {
				target += ":b"
			}
			r := testutil.NewRepo(t, map[string]string{
				"a.go": region("a", "old", target),
				"b.go": region("b", "body", "missing.go"),
			})
			r.Write(map[string]string{"a.go": region("a", "new", target)})
			r.Commit()
			got, err := Check(Options{Dir: r.Dir, Base: r.Base})
			if err != nil || len(got.Errors) != 0 || len(got.Findings) != 1 || got.Findings[0].Target != (Target{"b.go", map[bool]string{false: "", true: "b"}[labeled]}) {
				t.Fatalf("outgoing target links must not be recursively validated: %+v, %v", got, err)
			}
		})
	}
}

func TestWholeFileModeChange(t *testing.T) {
	for _, labeled := range []bool{false, true} {
		t.Run(map[bool]string{false: "whole file satisfied", true: "label unsatisfied"}[labeled], func(t *testing.T) {
			target := "b.go"
			want := 0
			if labeled {
				target += ":b"
				want = 1
			}
			r := testutil.NewRepo(t, map[string]string{"a.go": region("a", "old", target), "b.go": region("b", "body", "")})
			r.Write(map[string]string{"a.go": region("a", "new", target)})
			r.Git("add", "--all")
			r.Git("update-index", "--chmod=+x", "b.go")
			// Commit the index directly; another git add could reset its mode.
			r.Git("commit", "-qm", "mode change")
			got, err := Check(Options{Dir: r.Dir, Base: r.Base})
			if err != nil || len(got.Errors) != 0 || len(got.Findings) != want {
				t.Fatalf("got %+v, %v; want %d findings", got, err, want)
			}
		})
	}
}
