package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ifttt/internal/testutil"
)

func TestCLI(t *testing.T) {
	t.Setenv("GITHUB_EVENT_PATH", "")
	t.Setenv("GITHUB_STEP_SUMMARY", "")
	t.Setenv("GITHUB_ACTIONS", "")
	source := "// LINT.IfChange(a)\nold\n// LINT.ThenChange(b.go)\n"
	r := testutil.NewRepo(t, map[string]string{"a.go": source, "b.go": "old"})
	r.Write(map[string]string{"a.go": strings.Replace(source, "old", "new", 1)})
	head := r.Commit()
	baseArgs := []string{"check", "--repo=" + r.Dir, "--base=" + r.Base}
	for _, tt := range []struct {
		name     string
		args     []string
		want     int
		contains string
	}{
		{"help", []string{"--help"}, 0, "Usage:"},
		{"check help", []string{"check", "--help"}, 0, "Usage of check"},
		{"version", []string{"--version"}, 0, "ifttt dev"},
		{"bad command", []string{"bad"}, 3, "expected 'check'"},
		{"bad flag", []string{"check", "--bad"}, 3, "flag provided but not defined"},
		{"warn", baseArgs, 0, "b.go was not"},
		{"fail", append(append([]string{}, baseArgs...), "--fail-on-findings"), 1, "b.go was not"},
		{"bad ref", []string{"check", "--repo=" + r.Dir, "--base=missing"}, 3, "rev-parse"},
		{"missing base", []string{"check", "--repo=" + r.Dir}, 3, "base ref is required"},
		{"extra argument", []string{"check", "extra"}, 3, "unexpected arguments"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			if got := run(tt.args, &out, &out); got != tt.want || !strings.Contains(out.String(), tt.contains) {
				t.Fatalf("exit %d, output %s", got, out.String())
			}
		})
	}
	eventPath := filepath.Join(t.TempDir(), "event.json")
	summary := filepath.Join(t.TempDir(), "summary.md")
	writeEvent := func(body string) {
		t.Helper()
		data, err := json.Marshal(map[string]any{"pull_request": map[string]any{"body": body, "base": map[string]string{"sha": r.Base}, "head": map[string]string{"sha": head}}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(eventPath, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeEvent("NO_IFTTT=already done")
	t.Setenv("GITHUB_EVENT_PATH", eventPath)
	t.Setenv("GITHUB_STEP_SUMMARY", summary)
	t.Setenv("GITHUB_ACTIONS", "true")
	var out bytes.Buffer
	// HEAD and worktree deliberately differ from the event's head SHA.
	r.Git("checkout", "--detach", r.Base)
	if got := run([]string{"check", "--repo=" + r.Dir, "--fail-on-findings"}, &out, &out); got != 0 || !strings.Contains(out.String(), "1 coupling finding(s) overridden") {
		t.Fatalf("exit %d, %s", got, out.String())
	}
	data, err := os.ReadFile(summary)
	if err != nil || !strings.Contains(string(data), "already done") {
		t.Fatalf("summary %s, %v", data, err)
	}
	writeEvent("")
	out.Reset()
	if got := run([]string{"check", "--repo=" + r.Dir, "--fail-on-findings"}, &out, &out); got != 1 || !strings.Contains(out.String(), "::warning file=a.go,line=2::") {
		t.Fatalf("exit %d, %s", got, out.String())
	}
	// Overrides never suppress malformed annotations.
	r.Git("checkout", "--detach", head)
	r.Write(map[string]string{"a.go": "// LINT.IfChange\n"})
	head = r.Commit()
	writeEvent("NO_IFTTT=skip")
	out.Reset()
	if got := run([]string{"check", "--repo=" + r.Dir}, &out, &out); got != 2 || !strings.Contains(out.String(), "::error") {
		t.Fatalf("exit %d, %s", got, out.String())
	}
	for _, content := range []string{"bad JSON", "{}", `{"pull_request":{"base":{"sha":"main"}}}`} {
		if err := os.WriteFile(eventPath, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if got := run([]string{"check", "--repo=" + r.Dir}, &out, &out); got != 3 {
			t.Fatalf("invalid event exit %d", got)
		}
	}
}

// A failed check/report must never look like a clean run to presubmit.
func TestCLIFailures(t *testing.T) {
	t.Setenv("GITHUB_EVENT_PATH", "")
	t.Setenv("GITHUB_STEP_SUMMARY", "")
	t.Setenv("GITHUB_ACTIONS", "")
	r := testutil.NewRepo(t, nil)
	args := []string{"check", "--repo=" + r.Dir, "--base=" + r.Base}
	for _, tt := range []struct {
		name    string
		extra   []string
		stdout  io.Writer
		message string
	}{
		{"missing event", []string{"--event=" + filepath.Join(t.TempDir(), "missing.json")}, io.Discard, "read event"},
		{"bad head", []string{"--head=missing"}, io.Discard, "rev-parse"},
		{"summary directory", []string{"--summary=" + t.TempDir()}, io.Discard, "open summary"},
		{"output failure", nil, failingWriter{}, "closed pipe"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			if got := run(append(append([]string{}, args...), tt.extra...), tt.stdout, &stderr); got != 3 || !strings.Contains(stderr.String(), tt.message) {
				t.Fatalf("exit %d, stderr %s", got, stderr.String())
			}
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
