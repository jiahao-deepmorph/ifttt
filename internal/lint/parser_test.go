package lint

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name, file, text string
		regions, errors  int
	}{
		{"labeled", "a.go", "// LINT.IfChange(x)\na\n// LINT.ThenChange(//b.py:x, :y, docs/file name.md)\n", 1, 0},
		{"unlabeled target only", "a.go", "// LINT.IfChange\na\n// LINT.ThenChange()", 1, 0},
		{"nested", "a.go", "// LINT.IfChange\n// LINT.IfChange\n// LINT.ThenChange()", 1, 1},
		{"orphan", "a.go", "// LINT.ThenChange()", 0, 1},
		{"unclosed", "a.go", "// LINT.IfChange", 0, 1},
		{"duplicate label", "a.go", "// LINT.IfChange(x)\n// LINT.ThenChange()\n// LINT.IfChange(x)\n// LINT.ThenChange()", 2, 1},
		{"invalid label", "a.go", "// LINT.IfChange(two words)", 0, 1},
		{"empty label", "a.go", "// LINT.IfChange()", 0, 1},
		{"missing parentheses", "a.go", "// LINT.ThenChange", 0, 1},
		{"unclosed list", "a.go", "// LINT.IfChange\n// LINT.ThenChange(a", 0, 2},
		{"multiline list", "a.py", "# LINT.IfChange(x)\nx\n# LINT.ThenChange(\n# a.py:x,\n# b.md\n# )", 1, 0},
		{"block lines", "a.c", "/*\n * LINT.IfChange(x)\n */\nx\n/* LINT.ThenChange() */", 1, 0},
		{"html", "a.html", "<!-- LINT.IfChange(x) -->\nx\n<!-- LINT.ThenChange() -->", 1, 0},
		{"sql", "a.sql", "-- LINT.IfChange(x)\nx\n-- LINT.ThenChange()", 1, 0},
		{"ini", "a.ini", "; LINT.IfChange(x)\nx\n; LINT.ThenChange()", 1, 0},
		{"tex", "a.tex", "% LINT.IfChange(x)\nx\n% LINT.ThenChange()", 1, 0},
		{"vb quote", "a.vb", "' LINT.IfChange(x)\nx\n' LINT.ThenChange()", 1, 0},
		{"vb rem", "a.vb", "rem LINT.IfChange(x)\nx\nREM LINT.ThenChange()", 1, 0},
		{"string", "a.go", "var s = \"// LINT.IfChange(x)\"", 0, 0},
		{"raw string", "a.go", "var s = `\n// LINT.IfChange(x)\n`", 0, 0},
		{"triple string", "a.py", "s = \"\"\"\n# LINT.IfChange(x)\n\"\"\"", 0, 0},
		{"template string", "a.js", "const s = `\n// LINT.IfChange(x)\n`;", 0, 0},
		{"inline comment", "a.go", "x := 1 // LINT.IfChange(x)", 0, 0},
		{"wrong leader", "a.py", "// LINT.IfChange(x)", 0, 0},
		{"unknown extension", "a.unknown", "// LINT.IfChange(x)", 0, 0},
		{"binary", "a.go", "\x00\n// LINT.IfChange(x)", 0, 0},
		{"crlf", "a.go", "// LINT.IfChange(x)\r\nx\r\n// LINT.ThenChange()\r\n", 1, 0},
		{"makefile", "Makefile", "# LINT.IfChange(x)\nx\n# LINT.ThenChange()", 1, 0},
		{"markdown prose", "a.md", "Don't ignore this.\n<!-- LINT.IfChange(x) -->\nwords\n<!-- LINT.ThenChange() -->", 1, 0},
		{"markdown fence", "a.md", "```html\n<!-- LINT.IfChange(fake) -->\n```\n<!-- LINT.IfChange(real) -->\nx\n<!-- LINT.ThenChange() -->", 1, 0},
		{"html prose", "a.html", "Don't ignore this.\n<!-- LINT.IfChange(x) -->\nwords\n<!-- LINT.ThenChange() -->", 1, 0},
		{"html attribute string", "a.html", "<div title=\"\n<!-- LINT.IfChange(fake) -->\n\">", 0, 0},
		{"yaml scalar", "a.yaml", "script: |\n  # LINT.IfChange(fake)\n# LINT.IfChange(real)\na: b\n# LINT.ThenChange()", 1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			regions, errors := Parse(tt.file, tt.text)
			if len(regions) != tt.regions || len(errors) != tt.errors {
				t.Fatalf("got regions=%+v errors=%+v; want %d regions, %d errors", regions, errors, tt.regions, tt.errors)
			}
		})
	}
}

func TestTargetGrammar(t *testing.T) {
	for _, input := range []string{"", "../a.go", "a/../b.go", "/a.go", "///a.go", "a\\b", "a:", "a:bad label", "a:x:y", ".", "a\n.go", "a(b)"} {
		t.Run(input, func(t *testing.T) {
			if _, err := parseTarget("source.go", input); err == nil {
				t.Fatalf("accepted %q", input)
			}
		})
	}
	for input, want := range map[string]Target{
		":here": {"source.go", "here"}, "//a.go:x": {"a.go", "x"}, "./a.go": {"a.go", ""}, "docs/file name.md": {"docs/file name.md", ""},
	} {
		got, err := parseTarget("source.go", input)
		if err != nil || got != want {
			t.Fatalf("%s: got %+v, %v; want %+v", input, got, err, want)
		}
	}
}

func TestContinuationBoundariesAndDedup(t *testing.T) {
	regions, diagnostics := Parse("a.go", "// LINT.IfChange(x)\nbody\n// LINT.ThenChange(\n// b.go, b.go\n// )\n")
	if len(diagnostics) != 0 || len(regions) != 1 {
		t.Fatalf("%+v, %+v", regions, diagnostics)
	}
	r := regions[0]
	if r.Open != 1 || r.Close != 3 || r.End != 5 || len(r.Targets) != 1 {
		t.Fatal(r)
	}
}

func FuzzParse(f *testing.F) {
	f.Add("// LINT.IfChange(x)\nbody\n// LINT.ThenChange(a.go)")
	f.Add("/* LINT.ThenChange(\n")
	f.Fuzz(func(t *testing.T, text string) {
		regions, _ := Parse("a.go", text)
		for _, r := range regions {
			if r.Open >= r.Close || r.Close > r.End || r.End > strings.Count(text, "\n")+1 {
				t.Fatalf("invalid region: %+v", r)
			}
		}
	})
}
