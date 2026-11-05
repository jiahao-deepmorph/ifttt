package report

import (
	"bytes"
	"io"
	"reflect"
	"strings"
	"testing"

	"ifttt/internal/lint"
)

func TestWaivers(t *testing.T) {
	for _, tt := range []struct {
		body     string
		want     []Waiver
		unscoped bool
	}{
		{"", nil, false},
		{"Reason: NO_IFTTT(pair)=done in #123\nother text", []Waiver{{"pair", "done in #123"}}, false},
		{"NO_IFTTT( a.go:pair )= moved \nNO_IFTTT(other)=two", []Waiver{{"a.go:pair", "moved"}, {"other", "two"}}, false},
		{"NOT_NO_IFTTT(pair)=skip", nil, false},
		// A PR-wide override waives nothing; it is reported so it can be scoped.
		{"NO_IFTTT=skip", nil, true}, {"NO_IFTTT()=skip", nil, true},
		{"NO_IFTTT(pair)= \t\r\nnext line", nil, false},
		// Placeholders and hidden HTML comments (for example, a PR template
		// explaining the override) must not waive anything.
		{"NO_IFTTT=<reason>", nil, false}, {"Add `NO_IFTTT(<label>)=<reason>` if needed", nil, false},
		{"NO_IFTTT(<label>)=done", nil, false}, {"NO_IFTTT(pair)=<reason>", nil, false},
		{"<!-- Add NO_IFTTT(pair)=skip to silence IFTTT -->", nil, false},
		{"NO_<!-- hidden -->IFTTT(pair)=skip", nil, false},
		{"prefix<!-- hidden -->NO_IFTTT(pair)=skip", []Waiver{{"pair", "skip"}}, false},
		{"NO_IFTTT(pair)=<!-- hidden\ncomment -->next line", nil, false},
		{"NO_IFTTT(pair)=done<!-- hidden --> in #7", []Waiver{{"pair", "done  in #7"}}, false},
		{"<!-- unclosed\nNO_IFTTT(pair)=hidden", nil, false},
	} {
		got, unscoped := Waivers(tt.body)
		if !reflect.DeepEqual(got, tt.want) || unscoped != tt.unscoped {
			t.Errorf("%q: %q %v, want %q %v", tt.body, got, unscoped, tt.want, tt.unscoped)
		}
	}
}

var (
	pair      = lint.Finding{Path: "a.go", Label: "pair", Start: 2, End: 4, Line: 3, Target: lint.Target{Path: "b.md", Label: "pair"}}
	other     = lint.Finding{Path: "c.go", Label: "other", Start: 5, End: 6, Line: 5, Target: lint.Target{Path: "e.md"}}
	unlabeled = lint.Finding{Path: "d.yaml", Start: 1, End: 3, Line: 2, Target: lint.Target{Path: "f.md"}}
)

func TestAWaiverCoversOnlyTheLabelledRegionItNames(t *testing.T) {
	result := lint.Result{Findings: []lint.Finding{pair, other, unlabeled}}
	waivers := []Waiver{
		{"pair", "done in #7"},
		// The path must be the changed region's own file.
		{"a.go:other", "wrong file"},
		{"c.go:other", "help text only"},
		// An unlabelled region cannot be waived: label it first.
		{"d.yaml", "no label"},
		{"d.yaml:", "no label"},
		{"gone", "stale"},
	}

	got := Apply(result, waivers, false)

	if !reflect.DeepEqual(got.WaivedBy, []Waiver{waivers[0], waivers[2], {}}) {
		t.Errorf("WaivedBy %q", got.WaivedBy)
	}
	if !reflect.DeepEqual(got.Unused, []Waiver{waivers[1], waivers[3], waivers[4], waivers[5]}) {
		t.Errorf("Unused %q", got.Unused)
	}
}

func TestExitCodes(t *testing.T) {
	finding := lint.Result{Findings: []lint.Finding{pair}}
	broken := lint.Result{Findings: finding.Findings, Errors: []lint.Diagnostic{{}}}
	waived := []Waiver{{"pair", "done"}}
	for _, tt := range []struct {
		report Report
		fail   bool
		want   int
	}{
		{Apply(lint.Result{}, nil, false), true, 0},
		{Apply(finding, nil, false), false, 0},
		{Apply(finding, nil, false), true, 1},
		{Apply(finding, nil, true), true, 1},
		{Apply(finding, []Waiver{{"other", "done"}}, false), true, 1},
		{Apply(finding, waived, false), true, 0},
		// One waived finding does not excuse another.
		{Apply(lint.Result{Findings: []lint.Finding{pair, other}}, waived, false), true, 1},
		{Apply(broken, waived, false), true, 2},
		{Apply(broken, nil, false), false, 2},
	} {
		if got := ExitCode(tt.report, tt.fail); got != tt.want {
			t.Errorf("got %d, want %d", got, tt.want)
		}
	}
}

func TestEveryFindingAndWaiverIsReported(t *testing.T) {
	r := Apply(lint.Result{Findings: []lint.Finding{pair, other, unlabeled}}, []Waiver{{"pair", "done in #7"}, {"gone", "stale"}}, true)
	for _, tt := range []struct {
		github bool
		want   string
	}{
		{false, "IFTTT (waived by NO_IFTTT(pair)): a.go:2-4 (pair) changed, but b.md:pair was not.\n" +
			"IFTTT: c.go:5-6 (other) changed, but e.md was not. If no update is needed, add NO_IFTTT(other)=<reason> to the PR description.\n" +
			"IFTTT: d.yaml:1-3 changed, but f.md was not. If no update is needed, label the region, LINT.IfChange(<label>), and add NO_IFTTT(<label>)=<reason> to the PR description.\n" +
			"IFTTT: NO_IFTTT(gone) matches no coupling finding; remove it from the PR description.\n" +
			"IFTTT: an unscoped NO_IFTTT=<reason> waives nothing; name the region's label: NO_IFTTT(<label>)=<reason>.\n" +
			"IFTTT: 1 coupling finding(s) waived.\n"},
		{true, "::notice file=a.go,line=3::IFTTT (waived by NO_IFTTT(pair)): a.go:2-4 (pair) changed, but b.md:pair was not.\n" +
			"::warning file=c.go,line=5::IFTTT: c.go:5-6 (other) changed, but e.md was not. If no update is needed, add NO_IFTTT(other)=<reason> to the PR description.\n" +
			"::warning file=d.yaml,line=2::IFTTT: d.yaml:1-3 changed, but f.md was not. If no update is needed, label the region, LINT.IfChange(<label>), and add NO_IFTTT(<label>)=<reason> to the PR description.\n" +
			"::warning::IFTTT: NO_IFTTT(gone) matches no coupling finding; remove it from the PR description.\n" +
			"::warning::IFTTT: an unscoped NO_IFTTT=<reason> waives nothing; name the region's label: NO_IFTTT(<label>)=<reason>.\n" +
			"IFTTT: 1 coupling finding(s) waived.\n"},
	} {
		var out bytes.Buffer
		if err := Write(&out, r, tt.github); err != nil {
			t.Fatal(err)
		}
		if out.String() != tt.want {
			t.Errorf("got:\n%s\nwant:\n%s", out.String(), tt.want)
		}
	}

	var out bytes.Buffer
	if err := Summary(&out, r); err != nil {
		t.Fatal(err)
	}
	want := "## IFTTT\n\n" +
		"| Changed region | Missing counterpart | Waived by |\n| --- | --- | --- |\n" +
		"| a.go:2-4 (pair) | b.md:pair | NO_IFTTT(pair): done in #7 |\n" +
		"| c.go:5-6 (other) | e.md |  |\n" +
		"| d.yaml:1-3 | f.md |  |\n\n" +
		"If no update is needed, add `NO_IFTTT(<label>)=<reason>` to the PR description; an unlabelled region needs a label first.\n\n" +
		"NO_IFTTT(gone) matches no coupling finding; remove it from the PR description.\n\n" +
		"An unscoped `NO_IFTTT=<reason>` waives nothing; name the region's label: `NO_IFTTT(<label>)=<reason>`.\n"
	if out.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestCleanRuns(t *testing.T) {
	var out bytes.Buffer
	if err := Write(&out, Apply(lint.Result{}, nil, false), false); err != nil {
		t.Fatal(err)
	}
	if err := Summary(&out, Apply(lint.Result{}, nil, false)); err != nil {
		t.Fatal(err)
	}
	if out.String() != "IFTTT: no findings.\n## IFTTT\n\nNo findings.\n" {
		t.Fatal(out.String())
	}
}

func TestAnnotationsAndSummaryAreEscaped(t *testing.T) {
	r := Apply(lint.Result{
		Errors:   []lint.Diagnostic{{Path: "a,b.go", Line: 2, Message: "bad\n::error::injected%"}},
		Findings: []lint.Finding{{Path: "a,b.go", Label: "x", Start: 2, End: 4, Line: 3, Target: lint.Target{Path: "b.go"}}},
	}, []Waiver{{"x", "<script>|`\nreason"}}, false)
	var out bytes.Buffer
	if err := Write(&out, r, true); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	// The waiver's reason comes from the PR body and never reaches the log.
	if !strings.Contains(text, "file=a%2Cb.go,line=3") || !strings.Contains(text, "bad%0A::error::injected%25") || strings.Contains(text, "script") {
		t.Fatal(text)
	}
	out.Reset()
	if err := Summary(&out, r); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "<script>") || !strings.Contains(out.String(), "&lt;script&gt;&#124;&#96; reason") || !strings.Contains(out.String(), "Annotation errors") {
		t.Fatal(out.String())
	}
}

func TestDeletedSourceHasNoHeadLocation(t *testing.T) {
	var out bytes.Buffer
	r := Apply(lint.Result{Findings: []lint.Finding{{Path: "gone.go", Start: 2, End: 4, Deleted: true, Target: lint.Target{Path: "b.go"}}}}, nil, false)
	if err := Write(&out, r, true); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "::warning::") {
		t.Fatal(out.String())
	}
}

func TestOutputFailures(t *testing.T) {
	r := Apply(lint.Result{Findings: []lint.Finding{{Path: "a.go", Start: 2, End: 2, Target: lint.Target{Path: "b.go"}}}}, nil, false)
	for _, github := range []bool{false, true} {
		if err := Write(failingWriter{}, r, github); err == nil {
			t.Fatal("report output failure was ignored")
		}
	}
	if err := Summary(failingWriter{}, r); err == nil {
		t.Fatal("summary output failure was ignored")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
