package testkit_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/testkit"
)

func TestRepo_BuildsBranchesWithStableCommits(t *testing.T) {
	t.Parallel()

	first := testkit.CleanFix(t)
	second := testkit.CleanFix(t)

	if got := first.Git("rev-parse", "--abbrev-ref", "HEAD"); got != testkit.FixtureHead {
		t.Errorf("HEAD is on %q, want %q", got, testkit.FixtureHead)
	}
	if first.Head() != second.Head() {
		t.Errorf("two builds of clean-fix gave different shas: %s and %s", first.Head(), second.Head())
	}
	if base := first.Git("rev-parse", testkit.FixtureBase); base == first.Head() {
		t.Error("base and head point at the same commit")
	}
}

func TestRepo_RenameProducesStatusR(t *testing.T) {
	t.Parallel()

	r := testkit.NewRepo(t)
	r.Write("src/total.js", "export const total = () => 0\n")
	r.Commit("feat: add total")

	r.Branch("refactor")
	r.Rename("src/total.js", "src/order/total.js")
	r.Commit("refactor: move total")

	got := r.Git("diff", "--name-status", "--find-renames", "main", "refactor")
	if !strings.HasPrefix(got, "R") {
		t.Errorf("diff status = %q, want a rename", got)
	}
}

func TestRepo_RemoveDropsTheFileFromHead(t *testing.T) {
	t.Parallel()

	r := testkit.NewRepo(t)
	r.Write("src/total.test.js", "test('x', () => {})\n")
	r.Commit("test: add a case")

	r.Branch("drop")
	r.Remove("src/total.test.js")
	r.Commit("chore: delete the test")

	if got := r.Git("diff", "--name-status", "main", "drop"); got != "D\tsrc/total.test.js" {
		t.Errorf("diff = %q, want a deletion", got)
	}
}

// TestRepo_MergeKeepsTheMergeCommit covers the fixture shape `redfirst audit`
// reads as one unit of history: a merge commit with two parents, even where
// the branch would have fast-forwarded.
func TestRepo_MergeKeepsTheMergeCommit(t *testing.T) {
	t.Parallel()

	r := testkit.NewRepo(t)
	r.Write("README.md", "# shop\n")
	r.Commit("chore: seed the tree")

	r.Branch("feature")
	r.Write("src/total.js", "export const total = () => 0\n")
	r.Commit("feat: add total")

	subject := "Merge pull request #7 from shop/total"
	r.Checkout("main")
	merge := r.Merge("feature", subject)

	parents := strings.Fields(r.Git("rev-list", "--parents", "-n", "1", merge))
	if len(parents) != 3 {
		t.Fatalf("merge commit has %d parents, want 2: %v", len(parents)-1, parents)
	}
	if got := r.Git("log", "--first-parent", "--format=%s", "-n", "1", "main"); got != subject {
		t.Errorf("first-parent subject = %q, want %q", got, subject)
	}
}

// TestRepo_CommitsSpanDays holds the property the audit header reports: a
// fixture is a history rather than one instant, and it dates the same way on
// every machine.
func TestRepo_CommitsSpanDays(t *testing.T) {
	t.Parallel()

	r := testkit.NewRepo(t)
	r.Write("README.md", "# shop\n")
	r.Commit("chore: seed the tree")
	r.Write("README.md", "# shop, the sequel\n")
	r.Commit("docs: say more")

	dates := strings.Fields(r.Git("log", "--format=%cd", "--date=format:%Y-%m-%d", "main"))
	if len(dates) != 2 {
		t.Fatalf("log printed %d dates, want 2: %v", len(dates), dates)
	}
	if dates[0] == dates[1] {
		t.Errorf("both commits landed on %s; a fixture history has to span days", dates[0])
	}
}

func TestFakeRunner_CountsItsOwnInvocations(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	runner := testkit.NewFakeRunner(t, filepath.Join(dir, "test.sh"), []testkit.FakeCase{
		{File: "src/total.test.js", Name: "adds prices"},
	})

	if got := runner.Runs(); got != 0 {
		t.Fatalf("Runs() before any invocation = %d, want 0", got)
	}
	for i := 1; i <= 3; i++ {
		runFake(t, runner, dir, "")
		if got := runner.Runs(); got != i {
			t.Fatalf("Runs() after %d invocations = %d", i, got)
		}
	}
}

func TestFakeRunner_FlakyCaseChangesOutcomeBetweenRuns(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	runner := testkit.NewFakeRunner(t, filepath.Join(dir, "test.sh"), []testkit.FakeCase{
		{
			File: "src/total.test.js",
			Name: "promo item does not break the total",
			Runs: []testkit.FakeOutcome{testkit.FakeFail, testkit.FakePass, testkit.FakeFail},
		},
	})

	want := []bool{true, false, true, true} // the last outcome repeats
	for i, wantFailed := range want {
		out, failed := runFake(t, runner, dir, "")
		if failed != wantFailed {
			t.Errorf("run %d: failed = %v, want %v", i+1, failed, wantFailed)
		}
		if got := strings.Contains(out, "<failure"); got != wantFailed {
			t.Errorf("run %d: results report a failure = %v, want %v\n%s", i+1, got, wantFailed, out)
		}
	}
}

func TestFakeRunner_FilterSelectsFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	runner := testkit.NewFakeRunner(t, filepath.Join(dir, "test.sh"), []testkit.FakeCase{
		{File: "src/total.test.js", Name: "adds prices"},
		{File: "src/order.test.js", Name: "places an order"},
	})

	out, _ := runFake(t, runner, dir, "src/order.test.js")
	if strings.Contains(out, "adds prices") {
		t.Errorf("filter did not exclude the other file:\n%s", out)
	}
	if !strings.Contains(out, "places an order") {
		t.Errorf("filter dropped the requested file:\n%s", out)
	}

	out, _ = runFake(t, runner, dir, "")
	if !strings.Contains(out, "adds prices") || !strings.Contains(out, "places an order") {
		t.Errorf("an empty filter should run everything:\n%s", out)
	}
}

// runFake executes the runner once and returns the results file and whether
// the run reported a failure.
func runFake(t *testing.T, runner *testkit.FakeRunner, dir, filter string) (string, bool) {
	t.Helper()

	results := filepath.Join(dir, "results.xml")
	cmd := exec.Command(runner.Script)
	cmd.Env = append(os.Environ(), "REDFIRST_RESULTS="+results, "REDFIRST_FILTER="+filter)
	err := cmd.Run()

	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		t.Fatalf("run fake runner: %v", err)
	}
	data, readErr := os.ReadFile(results)
	if readErr != nil {
		t.Fatalf("read results: %v", readErr)
	}
	return string(data), err != nil
}
