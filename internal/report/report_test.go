package report

import (
	"bytes"
	"strings"
	"testing"

	"ifttt/internal/lint"
)

func TestOverride(t *testing.T) {
	for _, tt := range []struct{ body, want string }{
		{"", ""}, {"NO_IFTTT=", ""}, {"NO_IFTTT= \t\r\nnext line", ""},
		{"Reason: NO_IFTTT=done in #123\nother text", "done in #123"},
		{"NOT_NO_IFTTT=skip", ""}, {"NO_IFTTT= \nNO_IFTTT=second", "second"},
	} {
		if got := Override(tt.body); got != tt.want {
			t.Errorf("%q: %q != %q", tt.body, got, tt.want)
		}
	}
}

func TestExitCodes(t *testing.T) {
	finding := lint.Result{Findings: []lint.Finding{{}}}
	broken := lint.Result{Findings: finding.Findings, Errors: []lint.Diagnostic{{}}}
	for _, tt := range []struct {
		result lint.Result
		reason string
		fail   bool
		want   int
	}{
		{lint.Result{}, "", true, 0}, {finding, "", false, 0}, {finding, "", true, 1},
		{finding, "reason", true, 0}, {broken, "reason", true, 2}, {broken, "", false, 2},
	} {
		if got := ExitCode(tt.result, tt.reason, tt.fail); got != tt.want {
			t.Errorf("got %d, want %d", got, tt.want)
		}
	}
}

func TestAnnotationsAndSummary(t *testing.T) {
	r := lint.Result{
		Errors:   []lint.Diagnostic{{Path: "a,b.go", Line: 2, Message: "bad\n::error::injected%"}},
		Findings: []lint.Finding{{Path: "a,b.go", Start: 2, End: 4, Line: 3, Target: lint.Target{Path: "b.go"}}},
	}
	var out bytes.Buffer
	if err := Write(&out, r, "", true); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "file=a%2Cb.go,line=3") || !strings.Contains(text, "bad%0A::error::injected%25") || strings.Count(text, "\n") != 2 {
		t.Fatal(text)
	}
	out.Reset()
	if err := Write(&out, r, "override\n::error::injected", true); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "::warning") || strings.Contains(out.String(), "\n::error::injected") {
		t.Fatal(out.String())
	}
	out.Reset()
	if err := Summary(&out, r, "<script>|`\nreason"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "<script>") || !strings.Contains(out.String(), "&lt;script&gt;&#124;&#96; reason") || !strings.Contains(out.String(), "Annotation errors") {
		t.Fatal(out.String())
	}
}

func TestDeletedSourceHasNoHeadLocation(t *testing.T) {
	var out bytes.Buffer
	r := lint.Result{Findings: []lint.Finding{{Path: "gone.go", Start: 2, End: 4, Deleted: true, Target: lint.Target{Path: "b.go"}}}}
	if err := Write(&out, r, "", true); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "::warning::") {
		t.Fatal(out.String())
	}
}
