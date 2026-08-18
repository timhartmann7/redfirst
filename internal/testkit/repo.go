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
	"time"
)

// firstCommitTime is where every fixture history starts. Fixed, so commit
// hashes stay stable between runs and a golden file may carry one.
const firstCommitTime = "2026-01-01T00:00:00+00:00"

// commitInterval is how far apart two commits of a fixture land. A history that
// happened at one instant spans zero days, and `redfirst audit` prints the span
// it covered.
const commitInterval = 24 * time.Hour

// Repo is a git repository built for one test.
type Repo struct {
	t testing.TB
	// Dir is the repository root.
	Dir string
	// commits counts what the fixture has committed so far, which dates the
	// next one. Building the same fixture twice gives the same dates.
	commits int
}

// NewRepo initialises an empty repository on branch main inside t.TempDir().
func NewRepo(t testing.TB) *Repo {
	t.Helper()

	r := &Repo{t: t, Dir: t.TempDir()}
	r.Git("init", "--initial-branch=main")
	return r
}

// Git runs a git command in the repository and returns its trimmed output.
func (r *Repo) Git(args ...string) string {
	r.t.Helper()

	cmd := exec.Command("git", append([]string{"-C", r.Dir}, args...)...)
	cmd.Env = gitEnv(r.commitTime())
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// commitTime dates the commit the fixture is about to write.
func (r *Repo) commitTime() string {
	start, err := time.Parse(time.RFC3339, firstCommitTime)
	if err != nil {
		r.t.Fatalf("parse %s: %v", firstCommitTime, err)
	}
	return start.Add(time.Duration(r.commits) * commitInterval).Format(time.RFC3339)
}

// gitEnv shuts out the developer's own git configuration and fixes identity
// and timestamps, so a fixture built here looks the same everywhere.
func gitEnv(date string) []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=redfirst fixture",
		"GIT_AUTHOR_EMAIL=fixture@redfirst.test",
		"GIT_COMMITTER_NAME=redfirst fixture",
		"GIT_COMMITTER_EMAIL=fixture@redfirst.test",
		"GIT_AUTHOR_DATE=" + date,
		"GIT_COMMITTER_DATE=" + date,
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
	r.writeFile(path, content, 0o644)
}

// WriteScript creates or replaces an executable file. A hook committed without
// the bit cannot run, and git carries the bit through the commit.
func (r *Repo) WriteScript(path, content string) {
	r.t.Helper()
	r.writeFile(path, []byte(content), 0o755)
}

func (r *Repo) writeFile(path string, content []byte, mode os.FileMode) {
	r.t.Helper()

	full := filepath.Join(r.Dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		r.t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(full, content, mode); err != nil {
		r.t.Fatalf("write %s: %v", path, err)
	}
	// WriteFile leaves the mode of an existing file alone, and a fixture that
	// replaces a script would otherwise keep whatever mode it had before.
	if err := os.Chmod(full, mode); err != nil {
		r.t.Fatalf("chmod %s: %v", path, err)
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
	r.commits++
	return r.Head()
}

// Merge merges branch into the current one and keeps the merge commit even
// where the history would fast-forward: a repository that merges this way makes
// the merge commit the unit of its history.
func (r *Repo) Merge(branch, message string) string {
	r.t.Helper()

	r.Git("merge", "--quiet", "--no-ff", "-m", message, branch)
	r.commits++
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
