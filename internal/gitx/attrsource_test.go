package gitx_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/gitx"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

// TestGitx_LineCountsIgnoreGitattributesAddedOnHead closes the cheapest bypass
// of diff-budget there is. `*.py -diff` makes git print "-" for both counts,
// and the agent owns .gitattributes on its own branch. Invariant 5 puts rule
// lists on base, so the diff has to read attributes from the merge base.
func TestGitx_LineCountsIgnoreGitattributesAddedOnHead(t *testing.T) {
	t.Parallel()

	r := testkit.NewRepo(t)
	r.Write("src/app.py", "print(1)\n")
	r.Commit("feat: add the app")

	r.Branch(testkit.FixtureHead)
	r.Write("src/app.py", strings.Repeat("print(2)\n", 300))
	r.Write(".gitattributes", "*.py -diff\n")
	r.Commit("chore: silence the diff")

	got := fileByPath(t, diffFixture(t, r), "src/app.py")

	if got.Binary {
		t.Error("a .gitattributes line on head marked a text file binary")
	}
	if got.Added != 300 || got.Deleted != 1 {
		t.Errorf("counts = +%d -%d, want +300 -1: head must not decide the line count", got.Added, got.Deleted)
	}
}

// TestGitx_OpenAnchorsAtTheRepositoryTopLevel guards the other half of the same
// invariant. git diff names files from the repository root while ls-tree and
// show resolve against the current directory; anchored anywhere else, the run
// judges the whole diff while failing to find redfirst.toml and .redfirst/, and
// reports built-in defaults as though the project had chosen them.
func TestGitx_OpenAnchorsAtTheRepositoryTopLevel(t *testing.T) {
	t.Parallel()

	r := testkit.NewRepo(t)
	r.Write("redfirst.toml", "version = 1\n")
	r.Write("sub/src/app.js", "const a = 1\n")
	r.Commit("feat: add the app")

	repo, err := gitx.Open(t.Context(), filepath.Join(r.Dir, "sub"))
	if err != nil {
		t.Fatalf("open a subdirectory: %v", err)
	}

	found, err := repo.Exists(t.Context(), testkit.FixtureBase, "redfirst.toml")
	if err != nil {
		t.Fatalf("look for the config: %v", err)
	}
	if !found {
		t.Error("the config at the repository root went missing when opened from a subdirectory")
	}
}
