package gitx_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

// exportable is a repository holding the shapes a working copy has to carry
// over: a nested path, an executable file and a hook directory of its own.
func exportable(t *testing.T) *testkit.Repo {
	t.Helper()

	r := testkit.NewRepo(t)
	r.Write("src/order/total.js", "export const total = () => 0\n")
	r.Write("src/order/total.test.js", "test('adds', () => {})\n")
	r.WriteScript(".redfirst/test.sh", "#!/bin/sh\necho ran\n")
	r.WriteScript("run.sh", "#!/bin/sh\necho ran\n")
	r.Commit("chore: seed the tree")

	return r
}

func TestGitx_ExportWritesTheWholeTree(t *testing.T) {
	t.Parallel()

	r := exportable(t)
	dir := filepath.Join(t.TempDir(), "work")

	if err := openFixture(t, r).Export(t.Context(), testkit.FixtureBase, dir); err != nil {
		t.Fatalf("export: %v", err)
	}

	for _, path := range []string{"src/order/total.js", "src/order/total.test.js", ".redfirst/test.sh", "run.sh"} {
		if _, err := os.Stat(filepath.Join(dir, path)); err != nil {
			t.Errorf("the working copy is missing %s: %v", path, err)
		}
	}
	info, err := os.Stat(filepath.Join(dir, "run.sh"))
	if err != nil {
		t.Fatalf("stat run.sh: %v", err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("run.sh came out as %s; a hook that is not executable cannot run", info.Mode())
	}
}

// TestGitx_ExportWritesASubtreeAtTheRoot covers how the hooks travel: the
// contents of .redfirst/ land in a directory of their own, not under a copy of
// the repository layout.
func TestGitx_ExportWritesASubtreeAtTheRoot(t *testing.T) {
	t.Parallel()

	r := exportable(t)
	dir := filepath.Join(t.TempDir(), "hooks")

	if err := openFixture(t, r).Export(t.Context(), testkit.FixtureBase+":.redfirst", dir); err != nil {
		t.Fatalf("export: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "test.sh")); err != nil {
		t.Errorf("the hook directory is missing test.sh: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "src")); err == nil {
		t.Error("the export carried the rest of the tree along with the subtree")
	}
}

// TestGitx_ExportKeepsAFileExportIgnoreWouldDrop is why this does not go
// through `git archive`: the attribute lives in the tree under judgement, so an
// archive would let that tree decide which of its files reach the test run.
func TestGitx_ExportKeepsAFileExportIgnoreWouldDrop(t *testing.T) {
	t.Parallel()

	r := exportable(t)
	r.Branch(testkit.FixtureHead)
	r.Write(".gitattributes", "src/order/total.test.js export-ignore\n")
	r.Commit("chore: hide a test from the archive")

	dir := filepath.Join(t.TempDir(), "work")
	if err := openFixture(t, r).Export(t.Context(), testkit.FixtureHead, dir); err != nil {
		t.Fatalf("export: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "src/order/total.test.js")); err != nil {
		t.Errorf("export-ignore took a test file out of the working copy: %v", err)
	}
}

// TestGitx_ExportWritesNothingIntoTheRepository holds invariant 6 for the one
// operation that has to create files somewhere.
//
// The exported ref is deliberately not the one the repository has checked out:
// an export that wrote the repository's own index would leave every file of the
// working tree reading as changed, which is what the status below would catch.
func TestGitx_ExportWritesNothingIntoTheRepository(t *testing.T) {
	t.Parallel()

	r := exportable(t)
	r.Branch(testkit.FixtureHead)
	r.Write("src/order/total.js", "export const total = (i) => i.length\n")
	r.Write("src/order/added.js", "export const added = true\n")
	r.Commit("feat: count the items")
	before := r.Git("rev-parse", "HEAD")

	if err := openFixture(t, r).Export(t.Context(), testkit.FixtureBase, filepath.Join(t.TempDir(), "work")); err != nil {
		t.Fatalf("export: %v", err)
	}

	if got := r.Git("status", "--porcelain"); got != "" {
		t.Errorf("the working tree changed:\n%s", got)
	}
	if got := r.Git("rev-parse", "HEAD"); got != before {
		t.Errorf("HEAD moved from %s to %s", before, got)
	}
	if entries, err := os.ReadDir(filepath.Join(r.Dir, ".git", "worktrees")); err == nil {
		t.Errorf(".git/worktrees holds %d entries; the export registered a worktree", len(entries))
	}
}

func TestGitx_ExportOfAnUnknownRefIsHarnessFailure(t *testing.T) {
	t.Parallel()

	err := openFixture(t, exportable(t)).Export(t.Context(), "origin/nowhere", filepath.Join(t.TempDir(), "work"))

	if !errors.Is(err, domain.ErrHarness) {
		t.Fatalf("export of an unknown ref: %v, want a harness error", err)
	}
}
