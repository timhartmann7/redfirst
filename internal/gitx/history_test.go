package gitx_test

import (
	"errors"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/gitx"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

// mergedHistory is a repository that merges its branches: two pull requests
// landed as merge commits, with a plain commit on base between them.
func mergedHistory(t *testing.T) *testkit.Repo {
	t.Helper()

	r := testkit.NewRepo(t)
	r.Write("README.md", "# shop\n")
	r.Commit("chore: seed the tree")

	r.Branch("feature/total")
	r.Write("src/total.js", "export const total = () => 0\n")
	r.Commit("feat: add total")
	r.Checkout(testkit.FixtureBase)
	r.Merge("feature/total", "Merge pull request #1 from shop/total")

	r.Write("README.md", "# shop\n\nA shop.\n")
	r.Commit("docs: describe the shop")

	r.Branch("feature/discount")
	r.Write("src/discount.js", "export const discount = () => 0\n")
	r.Commit("feat: add the discount")
	r.Checkout(testkit.FixtureBase)
	r.Merge("feature/discount", "Merge pull request #2 from shop/discount")

	return r
}

func subjects(commits []gitx.Commit) []string {
	out := make([]string, 0, len(commits))
	for _, c := range commits {
		out = append(out, c.Subject)
	}
	return out
}

func firstParent(t *testing.T, r *testkit.Repo, w gitx.Walk) []gitx.Commit {
	t.Helper()

	commits, err := openFixture(t, r).FirstParent(t.Context(), testkit.FixtureBase, w)
	if err != nil {
		t.Fatalf("walk %s: %v", testkit.FixtureBase, err)
	}
	return commits
}

// TestGitx_FirstParentSkipsTheMergedBranch holds why the walk exists: the
// commits of a merged branch belong to the unit that merged them, and counting
// them alongside it would count one pull request several times.
func TestGitx_FirstParentSkipsTheMergedBranch(t *testing.T) {
	t.Parallel()

	got := subjects(firstParent(t, mergedHistory(t), gitx.Walk{}))
	want := []string{
		"Merge pull request #2 from shop/discount",
		"docs: describe the shop",
		"Merge pull request #1 from shop/total",
		"chore: seed the tree",
	}

	if len(got) != len(want) {
		t.Fatalf("walked %d commits, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("commit %d is %q, want %q", i, got[i], want[i])
		}
	}
}

func TestGitx_FirstParentReadsParentsDateAndSubject(t *testing.T) {
	t.Parallel()

	commits := firstParent(t, mergedHistory(t), gitx.Walk{})
	newest, root := commits[0], commits[len(commits)-1]

	if !newest.Merged() || len(newest.Parents) != 2 {
		t.Errorf("the newest commit has parents %v, want the two of a merge", newest.Parents)
	}
	if root.Merged() || len(root.Parents) != 0 {
		t.Errorf("the root commit has parents %v, want none", root.Parents)
	}
	if newest.SHA == "" || len(newest.SHA) < 7 {
		t.Errorf("commit sha = %q", newest.SHA)
	}
	// The fixture dates its commits a day apart, newest first.
	if !newest.Date.After(root.Date) {
		t.Errorf("the newest commit is dated %s, the root %s", newest.Date, root.Date)
	}
}

func TestGitx_FirstParentMergesOnlyDropsThePlainCommits(t *testing.T) {
	t.Parallel()

	got := subjects(firstParent(t, mergedHistory(t), gitx.Walk{Merges: true}))
	want := []string{
		"Merge pull request #2 from shop/discount",
		"Merge pull request #1 from shop/total",
	}

	if len(got) != len(want) {
		t.Fatalf("walked %d merges, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("merge %d is %q, want %q", i, got[i], want[i])
		}
	}
}

func TestGitx_FirstParentLimitCapsTheWalk(t *testing.T) {
	t.Parallel()

	got := firstParent(t, mergedHistory(t), gitx.Walk{Limit: 2})
	if len(got) != 2 {
		t.Fatalf("walked %d commits under Limit 2: %v", len(got), subjects(got))
	}
	if got[0].Subject != "Merge pull request #2 from shop/discount" {
		t.Errorf("the walk opens at %q, want the newest commit", got[0].Subject)
	}
}

// TestGitx_FirstParentSinceCutsTheOlderHistory pins the cutoff to an explicit
// offset: a bare date would mean whatever the machine's timezone says.
func TestGitx_FirstParentSinceCutsTheOlderHistory(t *testing.T) {
	t.Parallel()

	r := testkit.NewRepo(t)
	for _, subject := range []string{"one", "two", "three", "four", "five"} {
		r.Write("log.txt", subject+"\n")
		r.Commit("chore: " + subject)
	}

	// The fixture starts on 2026-01-01 and dates every commit a day later.
	got := firstParent(t, r, gitx.Walk{Since: "2026-01-04T00:00:00+00:00"})
	want := []string{"chore: five", "chore: four"}

	if len(got) != len(want) {
		t.Fatalf("walked %d commits since the cutoff, want %d: %v", len(got), len(want), subjects(got))
	}
	for i := range want {
		if got[i].Subject != want[i] {
			t.Errorf("commit %d is %q, want %q", i, got[i].Subject, want[i])
		}
	}
}

func TestGitx_FirstParentUnknownRefIsHarnessFailure(t *testing.T) {
	t.Parallel()

	repo := openFixture(t, mergedHistory(t))
	_, err := repo.FirstParent(t.Context(), "origin/nowhere", gitx.Walk{})

	if !errors.Is(err, domain.ErrHarness) {
		t.Fatalf("walk of an unknown ref: %v, want a harness error", err)
	}
}
