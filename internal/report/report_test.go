package report

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"ifttt/internal/lint"
)

func TestOverride(t *testing.T) {
	for _, tt := range []struct{ body, want string }{
		{"", ""}, {"NO_IFTTT=", ""}, {"NO_IFTTT= \t\r\nnext line", ""},
		{"Reason: NO_IFTTT=done in #123\nother text", "done in #123"},
		{"NOT_NO_IFTTT=skip", ""}, {"NO_IFTTT= \nNO_IFTTT=second", "second"},
		// Placeholders and hidden HTML comments (for example, a PR template
		// explaining the override) must not silence the check.
		{"NO_IFTTT=<reason>", ""}, {"Add `NO_IFTTT=<reason>` if needed", ""},
		{"<!-- Add NO_IFTTT=skip to silence IFTTT -->", ""},
		{"NO_<!-- hidden -->IFTTT=skip", ""},
		{"prefix<!-- hidden -->NO_IFTTT=skip", "skip"},
		{"NO_IFTTT=<!-- hidden\ncomment -->next line", ""},
		{"NO_IFTTT=done<!-- hidden --> in #7", "done  in #7"},
		{"<!-- unclosed\nNO_IFTTT=hidden", ""},
		{"<!--\nNO_IFTTT=hidden\n-->\nNO_IFTTT=done in #7", "done in #7"},
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

func TestOverriddenFindingsStayVisible(t *testing.T) {
	waived := lint.Result{Findings: []lint.Finding{{Path: "a.go", Start: 2, End: 4, Line: 3, Label: "pair", Target: lint.Target{Path: "b.md", Label: "pair"}}}}
	for _, tt := range []struct {
		result lint.Result
		github bool
		want   string
	}{
		{waived, false, "IFTTT (waived by NO_IFTTT): a.go:2-4 (pair) changed, but b.md:pair was not.\n" +
			"IFTTT: 1 coupling finding(s) overridden.\n"},
		{waived, true, "::notice file=a.go,line=3::IFTTT (waived by NO_IFTTT): a.go:2-4 (pair) changed, but b.md:pair was not.\n" +
			"IFTTT: 1 coupling finding(s) overridden.\n"},
		// An override with nothing to waive is stale or routine, and would
		// hide a later, unrelated finding: say so without failing.
		{lint.Result{}, false, "IFTTT: NO_IFTTT is set but there are no coupling findings; remove it from the PR description.\n" +
			"IFTTT: no findings.\n"},
		{lint.Result{}, true, "::warning::IFTTT: NO_IFTTT is set but there are no coupling findings; remove it from the PR description.\n" +
			"IFTTT: no findings.\n"},
	} {
		var out bytes.Buffer
		if err := Write(&out, tt.result, "done in #7", tt.github); err != nil {
			t.Fatal(err)
		}
		if out.String() != tt.want {
			t.Errorf("got:\n%s\nwant:\n%s", out.String(), tt.want)
		}
	}

	for _, tt := range []struct {
		result lint.Result
		want   string
	}{
		{waived, "## IFTTT\n\nCoupling findings overridden: done in #7\n\n" +
			"| Changed region (waived) | Missing counterpart |\n| --- | --- |\n| a.go:2-4 (pair) | b.md:pair |\n"},
		{lint.Result{}, "## IFTTT\n\nCoupling findings overridden: done in #7\n\n" +
			"`NO_IFTTT` is set but there are no coupling findings; remove it from the PR description.\n"},
	} {
		var out bytes.Buffer
		if err := Summary(&out, tt.result, "done in #7"); err != nil {
			t.Fatal(err)
		}
		if out.String() != tt.want {
			t.Errorf("got:\n%s\nwant:\n%s", out.String(), tt.want)
		}
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

func TestOutputFailures(t *testing.T) {
	result := lint.Result{Findings: []lint.Finding{{Path: "a.go", Start: 2, End: 2, Target: lint.Target{Path: "b.go"}}}}
	for _, github := range []bool{false, true} {
		if err := Write(failingWriter{}, result, "", github); err == nil {
			t.Fatal("report output failure was ignored")
		}
	}
	if err := Summary(failingWriter{}, result, ""); err == nil {
		t.Fatal("summary output failure was ignored")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
