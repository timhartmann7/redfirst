package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/harness"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

// runDoctor invokes the command the way a shell would and returns what it
// printed. A finding is an answer rather than a failure, so an error here means
// doctor could not look at all.
func runDoctor(t *testing.T, repoDir string, extra ...string) string {
	t.Helper()

	args := append([]string{"doctor", "--repo", repoDir, "--base", testkit.FixtureBase}, extra...)
	var out bytes.Buffer
	if err := run(context.Background(), args, &out, io.Discard); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	return out.String()
}

// TestDoctor_NamesTheCauseAndTheCommandForTheNextTier is the acceptance
// criterion of slice S7. A repository that reaches tier 1 has to be told what
// is missing and what to type, not that something is missing.
func TestDoctor_NamesTheCauseAndTheCommandForTheNextTier(t *testing.T) {
	t.Parallel()

	// The workflow reaches base, because that is where tier 1 is decided. The
	// stack markers reach the working tree, because that is the repository
	// somebody has in front of them when they run this.
	repo := onBase(t, testkit.CleanFix(t), "chore: run redfirst on every pull request", func(r *testkit.Repo) {
		r.Write(".github/workflows/redfirst.yml", "name: redfirst\n")
	})
	repo.Write("package.json", `{"devDependencies":{"vitest":"^2.0.0"}}`)
	repo.Commit("chore: declare the test runner")

	got := runDoctor(t, repo.Dir)

	for _, want := range []string{
		"redfirst v" + domain.Version + " · doctor · tier=",
		"red-green, suite-green unavailable",
		"node + vitest",
		"redfirst init --hooks node-unit",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the diagnosis does not carry %q:\n%s", want, got)
		}
	}
}

// TestDoctor_FindingsLeaveTheExitCodeAtZero keeps doctor out of the verdict
// business. Exit codes 1 to 5 are the contract verify has with an orchestrator,
// and a diagnostic that failed a build because it had something to say would be
// switched off on the day it was needed.
func TestDoctor_FindingsLeaveTheExitCodeAtZero(t *testing.T) {
	t.Parallel()

	repo := onBase(t, testkit.HookedFix(t), "chore: break the service layer", func(r *testkit.Repo) {
		r.WriteScript(harness.HooksDir+"/env-up.sh", brokenEnvUp)
	})

	var out bytes.Buffer
	args := []string{"doctor", "--repo", repo.Dir, "--base", testkit.FixtureBase}
	if err := run(context.Background(), args, &out, io.Discard); err != nil {
		t.Fatalf("doctor over a broken harness: %v (exit %d)", err, domain.ExitCode(err))
	}
	if !strings.Contains(out.String(), "the database refused the connection") {
		t.Errorf("the diagnosis hides what the hook said:\n%s", out.String())
	}
}

// TestDoctor_WritesNothingIntoTheRepositoryUnderExamination holds invariant 6
// for the one command besides verify that executes somebody else's scripts.
func TestDoctor_WritesNothingIntoTheRepositoryUnderExamination(t *testing.T) {
	t.Parallel()

	repo := testkit.HookedFix(t)
	before := repo.Git("status", "--porcelain=v1", "--untracked-files=all")
	head := repo.Head()

	runDoctor(t, repo.Dir)

	if after := repo.Git("status", "--porcelain=v1", "--untracked-files=all"); after != before {
		t.Errorf("the working tree changed: %q, was %q", after, before)
	}
	if after := repo.Head(); after != head {
		t.Errorf("HEAD moved to %s, was %s", after, head)
	}
}

// TestDoctor_UnknownBaseIsHarnessFailureNotAgentFault covers the one thing
// doctor refuses to answer for: it reads the rules from base, so a base it
// cannot resolve leaves it with nothing to diagnose.
func TestDoctor_UnknownBaseIsHarnessFailureNotAgentFault(t *testing.T) {
	t.Parallel()

	repo := testkit.CleanFix(t)
	args := []string{"doctor", "--repo", repo.Dir, "--base", "origin/nowhere"}

	err := run(context.Background(), args, io.Discard, io.Discard)
	if got := domain.ExitCode(err); got != 2 {
		t.Errorf("exit code = %d, want 2", got)
	}
}

// TestDoctor_NoHooksAnswersWithoutRunningAnything covers the flag for somebody
// whose environment costs minutes to bring up.
func TestDoctor_NoHooksAnswersWithoutRunningAnything(t *testing.T) {
	t.Parallel()

	repo := onBase(t, testkit.HookedFix(t), "chore: break the service layer", func(r *testkit.Repo) {
		r.WriteScript(harness.HooksDir+"/env-up.sh", brokenEnvUp)
	})

	got := runDoctor(t, repo.Dir, "--no-hooks")

	if !strings.Contains(got, "not probed · --no-hooks") {
		t.Errorf("the diagnosis does not say the hooks were left alone:\n%s", got)
	}
	if strings.Contains(got, "the database refused the connection") {
		t.Errorf("--no-hooks brought the environment up anyway:\n%s", got)
	}
}

// TestInit_GeneratedHooksReachTierTwoAndDoctorReadsThem is the seam between the
// two halves of the slice: `init --hooks` writes the scripts, the harness
// executes them, and doctor reports what they did. The blank preset refuses on
// purpose, and that refusal is what a project sees before it fills the hook in.
func TestInit_GeneratedHooksReachTierTwoAndDoctorReadsThem(t *testing.T) {
	t.Parallel()

	repo := testkit.CleanFix(t)
	repo.Checkout(testkit.FixtureBase)

	var out bytes.Buffer
	args := []string{"init", "--hooks", "blank", "--repo", repo.Dir}
	if err := run(context.Background(), args, &out, io.Discard); err != nil {
		t.Fatalf("init --hooks blank: %v", err)
	}
	if !strings.Contains(out.String(), "preset blank") {
		t.Errorf("init does not name the preset it wrote:\n%s", out.String())
	}
	repo.Commit("chore: add the redfirst hooks")

	got := runDoctor(t, repo.Dir)

	if !strings.Contains(got, "tier=2") {
		t.Errorf("the generated hooks did not reach tier 2:\n%s", got)
	}
	if !strings.Contains(got, "none · test.sh exited non-zero and wrote no case") {
		t.Errorf("the diagnosis does not say what the stub did:\n%s", got)
	}
}

// TestInit_PicksThePresetWhenNobodyNamesOne covers the auto path through the
// CLI, and the line that tells somebody which set they got.
func TestInit_PicksThePresetWhenNobodyNamesOne(t *testing.T) {
	t.Parallel()

	repo := testkit.CleanFix(t)
	repo.Write("go.mod", "module example.com/x\n")
	repo.Commit("chore: declare the module")

	var out bytes.Buffer
	args := []string{"init", "--hooks", "auto", "--repo", repo.Dir}
	if err := run(context.Background(), args, &out, io.Discard); err != nil {
		t.Fatalf("init --hooks auto: %v", err)
	}

	if !strings.Contains(out.String(), "preset go") {
		t.Errorf("init does not name the preset it picked:\n%s", out.String())
	}
	for _, want := range []string{"env-up.sh", "test.sh", "env-down.sh"} {
		if !strings.Contains(out.String(), harness.HooksDir+"/"+want) {
			t.Errorf("init never says it wrote %s:\n%s", want, out.String())
		}
	}
}
