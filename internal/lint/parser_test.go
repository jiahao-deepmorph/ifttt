package lint

import (
	"reflect"
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
		{"yaml scalar indicator in comment", "a.yaml", "key: value # example: |\n  # LINT.IfChange(real)\n  key: value\n  # LINT.ThenChange()", 1, 0},
		{"yaml sequence scalar ends at sibling", "a.yaml", "- script: |\n    # LINT.IfChange(fake)\n  # LINT.IfChange(real)\n  key: value\n  # LINT.ThenChange()", 1, 0},
		{"yaml scalar", "a.yaml", "script: |\n  # LINT.IfChange(fake)\n# LINT.IfChange(real)\na: b\n# LINT.ThenChange()", 1, 0},
		// A stray quote must not hide later directives. Single-line string
		// state ends with the line; quote characters that are not strings in a
		// language never open one.
		{"yaml apostrophe", "a.yaml", "desc: don't touch\n# LINT.IfChange(x)\nk: v\n# LINT.ThenChange()", 1, 0},
		{"yaml quoted scalar", "a.yaml", "k: 'it''s'\n# LINT.IfChange(x)\nk: v\n# LINT.ThenChange()", 1, 0},
		{"yaml template quotes", "a.yaml", "v: {{ join \",\" . | quote }}\n# LINT.IfChange(x)\nk: v\n# LINT.ThenChange()", 1, 0},
		{"yaml backtick", "a.yaml", "msg: press ` to open\n# LINT.IfChange(x)\nk: v\n# LINT.ThenChange()", 1, 0},
		{"rust lifetime", "a.rs", "fn f(x: &'static str) {}\n// LINT.IfChange(x)\nx\n// LINT.ThenChange()", 1, 0},
		{"rust quote chars", "a.rs", "let q = '\"';\nlet e = '\\'';\nlet u = '€';\n// LINT.IfChange(x)\nx\n// LINT.ThenChange()", 1, 0},
		{"rust multiline string", "a.rs", "let s = \"\n// LINT.IfChange(fake)\n\";", 0, 0},
		{"tex apostrophe", "a.tex", "It's fine.\n% LINT.IfChange(x)\nx\n% LINT.ThenChange()", 1, 0},
		{"clojure quote", "a.clj", "(def xs '(1 2 3))\n; LINT.IfChange(x)\nx\n; LINT.ThenChange()", 1, 0},
		{"clojure char literal", "a.clj", "(def q \\\")\n; LINT.IfChange(x)\nx\n; LINT.ThenChange()", 1, 0},
		{"ini apostrophe", "a.ini", "name = Bob's\n; LINT.IfChange(x)\nx\n; LINT.ThenChange()", 1, 0},
		{"shell escaped quote", "a.sh", "foo=it\\'s\n# LINT.IfChange(x)\nx\n# LINT.ThenChange()", 1, 0},
		{"shell multiline string", "a.sh", "echo '\n# LINT.IfChange(fake)\n'", 0, 0},
		{"cpp digit separator", "a.cc", "int n = 1'000;\n// LINT.IfChange(x)\nx\n// LINT.ThenChange()", 1, 0},
		{"c unterminated string", "a.c", "char *s = \"oops;\n// LINT.IfChange(x)\nx\n// LINT.ThenChange()", 1, 0},
		{"c line continuation", "a.c", "char *s = \"a\\\n// LINT.IfChange(fake)\n\";", 0, 0},
		{"matlab transpose", "a.m", "B = A';\n% LINT.IfChange(x)\nx\n% LINT.ThenChange()", 1, 0},
		{"markdown less-than", "a.md", "If a < b, don't stop.\n<!-- LINT.IfChange(x) -->\nwords\n<!-- LINT.ThenChange() -->", 1, 0},
		{"markdown lone backtick", "a.md", "Press the ` key.\n<!-- LINT.IfChange(x) -->\nwords\n<!-- LINT.ThenChange() -->", 1, 0},
		// When a string the scanner cannot close hides directives, say so.
		{"unclosed string hides directives", "a.sh", "cat <<EOF\ndon't\nEOF\n# LINT.IfChange(x)\nx\n# LINT.ThenChange()\n", 0, 1},
		{"closed string hides nothing", "a.go", "var s = `\n// LINT.IfChange(x)\n// LINT.ThenChange()\n`\n", 0, 0},
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
	f.Add("a.go", "// LINT.IfChange(x)\nbody\n// LINT.ThenChange(a.go)")
	f.Add("a.c", "/* LINT.ThenChange(\n")
	f.Add("a.yaml", "- script: |\n    # LINT.IfChange(fake)\n  # LINT.IfChange(real)\n  x: y\n  # LINT.ThenChange()")
	f.Add("a.md", "```html\n<!-- LINT.IfChange(fake) -->\n```\n")
	f.Add("a.rs", "let q = '\\'';\n// LINT.IfChange(x)\nbody\n// LINT.ThenChange()")
	f.Add("a.sh", "echo '\n# LINT.IfChange(fake)\n'")
	f.Add("a.py", "# LINT.IfChange(x)\nbody\n# LINT.ThenChange(\n# a.py:x\n# )")
	f.Fuzz(func(t *testing.T, filename, text string) {
		regions, _ := Parse(filename, text)
		for _, r := range regions {
			if r.Open >= r.Close || r.Close > r.End || r.End > strings.Count(text, "\n")+1 {
				t.Fatalf("invalid region: %+v", r)
			}
		}
	})
}

func TestYAMLScalarBoundaries(t *testing.T) {
	for _, prefix := range []string{
		"- script: |\n    # LINT.IfChange(fake)\n    # LINT.ThenChange()\n",
		"- script: >-\n    # LINT.IfChange(fake)\n    # LINT.ThenChange()\n",
		"-  script: |+2 # folded example\n     # LINT.IfChange(fake)\n     # LINT.ThenChange()\n",
		"- - script: |\n      # LINT.IfChange(fake)\n      # LINT.ThenChange()\n",
		"key: value # example: |\n",
		"key: \"example: |\"\n",
	} {
		t.Run(prefix, func(t *testing.T) {
			// Use the mapping's indentation for the real annotations.
			indent := "  "
			if strings.HasPrefix(prefix, "-  ") {
				indent = "   "
			} else if strings.HasPrefix(prefix, "- - ") {
				indent = "    "
			}
			content := prefix + indent + "# LINT.IfChange(real)\n" + indent + "key: value\n" + indent + "# LINT.ThenChange()\n"
			got, diagnostics := Parse("a.yaml", content)
			open := strings.Count(prefix, "\n") + 1
			want := []Region{{Label: "real", Open: open, Close: open + 2, End: open + 2}}
			if len(diagnostics) != 0 || !reflect.DeepEqual(got, want) {
				t.Fatalf("got %+v, %+v; want %+v", got, diagnostics, want)
			}
		})
	}
}
