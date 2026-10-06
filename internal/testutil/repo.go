// Package testutil builds real Git repositories for integration tests.
package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type Repo struct {
	T    *testing.T
	Dir  string
	Base string
}

func NewRepo(t *testing.T, files map[string]string) *Repo {
	t.Helper()
	r := &Repo{T: t, Dir: t.TempDir()}
	r.Git("init", "-q", "-b", "main")
	r.Write(files)
	r.Base = r.Commit()
	return r
}

func (r *Repo) Git(args ...string) string {
	r.T.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=IFTTT Test", "-c", "user.email=ifttt@example.test", "-c", "commit.gpgsign=false", "-c", "core.autocrlf=false"}, args...)...)
	cmd.Dir = r.Dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.T.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *Repo) Write(files map[string]string) {
	r.T.Helper()
	for name, content := range files {
		filename := filepath.Join(r.Dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
			r.T.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
			r.T.Fatal(err)
		}
	}
}

func (r *Repo) Remove(names ...string) {
	r.T.Helper()
	for _, name := range names {
		if err := os.Remove(filepath.Join(r.Dir, name)); err != nil {
			r.T.Fatal(err)
		}
	}
}

func (r *Repo) Commit() string {
	r.T.Helper()
	r.Git("add", "--all")
	r.Git("commit", "-qm", "fixture", "--allow-empty")
	return r.Git("rev-parse", "HEAD")
}
