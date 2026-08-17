// Package testkit assembles what the test suite needs: git repositories built
// inside t.TempDir(), golden file comparison and a fake test runner. Fixture
// repositories get built rather than committed, because a nested .git inside a
// repository is not a repository.
package testkit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fixedTime keeps commit hashes stable between runs, so a golden file may
// carry one.
const fixedTime = "2026-01-01T00:00:00+00:00"

// Repo is a git repository built for one test.
type Repo struct {
	t *testing.T
	// Dir is the repository root.
	Dir string
}

// NewRepo initialises an empty repository on branch main inside t.TempDir().
func NewRepo(t *testing.T) *Repo {
	t.Helper()

	r := &Repo{t: t, Dir: t.TempDir()}
	r.Git("init", "--initial-branch=main")
	return r
}

// Git runs a git command in the repository and returns its trimmed output.
func (r *Repo) Git(args ...string) string {
	r.t.Helper()

	cmd := exec.Command("git", append([]string{"-C", r.Dir}, args...)...)
	cmd.Env = gitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// gitEnv shuts out the developer's own git configuration and fixes identity
// and timestamps, so a fixture built here looks the same everywhere.
func gitEnv() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=redfirst fixture",
		"GIT_AUTHOR_EMAIL=fixture@redfirst.test",
		"GIT_COMMITTER_NAME=redfirst fixture",
		"GIT_COMMITTER_EMAIL=fixture@redfirst.test",
		"GIT_AUTHOR_DATE=" + fixedTime,
		"GIT_COMMITTER_DATE=" + fixedTime,
		"TZ=UTC",
	}
}

// Write creates or replaces a text file, parent directories included.
func (r *Repo) Write(path, content string) {
	r.t.Helper()
	r.WriteBinary(path, []byte(content))
}

// WriteBinary creates or replaces a file with raw bytes.
func (r *Repo) WriteBinary(path string, content []byte) {
	r.t.Helper()

	full := filepath.Join(r.Dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		r.t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(full, content, 0o644); err != nil {
		r.t.Fatalf("write %s: %v", path, err)
	}
}

// Remove deletes a tracked file.
func (r *Repo) Remove(path string) {
	r.t.Helper()
	r.Git("rm", "-q", "--", path)
}

// Rename moves a tracked file, so the diff carries status R.
func (r *Repo) Rename(from, to string) {
	r.t.Helper()

	full := filepath.Join(r.Dir, filepath.FromSlash(to))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		r.t.Fatalf("mkdir for %s: %v", to, err)
	}
	r.Git("mv", "--", from, to)
}

// Commit stages everything in the worktree and commits it, returning the sha.
func (r *Repo) Commit(message string) string {
	r.t.Helper()

	r.Git("add", "-A")
	r.Git("commit", "--quiet", "--allow-empty", "-m", message)
	return r.Head()
}

// Branch creates a branch off the current commit and switches to it.
func (r *Repo) Branch(name string) {
	r.t.Helper()
	r.Git("switch", "--quiet", "--create", name)
}

// Checkout switches to an existing ref.
func (r *Repo) Checkout(ref string) {
	r.t.Helper()
	r.Git("checkout", "--quiet", ref)
}

// Head is the sha the current branch points at.
func (r *Repo) Head() string {
	r.t.Helper()
	return r.Git("rev-parse", "HEAD")
}
