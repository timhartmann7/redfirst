package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/harness"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

// brokenEnvDown is a teardown that refuses. It stands for the compose file that
// will not come down and the bind mount a container left owned by root: the
// environment is broken after everything that judges has already spoken.
const brokenEnvDown = `#!/bin/sh
echo "the compose project would not come down" >&2
exit 1
`

// TestVerify_TeardownFailureKeepsTheGateVerdict is the exit code contract read
// from the orchestrator's side.
//
// A teardown runs after every gate has spoken, so what it says about the diff
// is nothing. Turning the refusal the diff earned into a harness failure hands
// the agent a retry without penalty for a violation it committed, and the
// report that named the violation never reaches it.
func TestVerify_TeardownFailureKeepsTheGateVerdict(t *testing.T) {
	t.Parallel()

	out := filepath.Join(t.TempDir(), "report.json")
	repo := onBase(t, fakeTestFixture(t), "chore: break the teardown", breakTeardown)

	args := append(verifyArgsWithHooks(repo.Dir),
		"--report", "json", "--out", out)
	err := run(context.Background(), args, io.Discard, io.Discard)

	if got := domain.ExitCode(err); got != 1 {
		t.Errorf("exit code = %d, want 1: the gate refused and the teardown came after it (%v)", got, err)
	}

	rep := readReport(t, out)
	if rep.ExitCode != 1 || rep.Verdict != domain.VerdictFail {
		t.Errorf("report says %s/%d, want the refusal the gate reached", rep.Verdict, rep.ExitCode)
	}
	if !hasWarning(rep, domain.WarnHarness, "teardown failed") {
		t.Errorf("warnings = %v, want the leak said out loud", rep.Warnings)
	}
}

// TestVerify_TeardownFailureOnAGreenRunIsAHarnessFailure is the other half.
// Nothing refused, so there is no verdict to protect, and an environment that
// would not come down is a repository problem the caller has to hear about.
func TestVerify_TeardownFailureOnAGreenRunIsAHarnessFailure(t *testing.T) {
	t.Parallel()

	repo := onBase(t, testkit.HookedFix(t), "chore: break the teardown", breakTeardown)

	err := run(context.Background(), verifyArgsWithHooks(repo.Dir), io.Discard, io.Discard)
	if got := domain.ExitCode(err); got != 2 {
		t.Errorf("exit code = %d, want 2: nothing judged the diff badly, the environment leaked (%v)", got, err)
	}
}

// breakTeardown puts the refusing teardown on the base ref.
//
// The reused mode is what makes this the case under test: there env-down.sh
// runs once, at the close, after every gate has spoken. In fresh mode it runs
// between the probes instead, and a failure there stops the judging rather than
// following it, which is a harness failure and exit code 2 by right.
func breakTeardown(r *testkit.Repo) {
	testkit.WriteHooks(r, testkit.Hooks{Reset: true})
	r.WriteScript(harness.HooksDir+"/env-down.sh", brokenEnvDown)
}

// fakeTestFixture is a diff whose added case passes without the fix, so
// red-green refuses it with exit code 1.
func fakeTestFixture(t testing.TB) *testkit.Repo {
	t.Helper()

	r := testkit.HookedFix(t)
	r.Write("src/total.test.js", fakeCaseTest)
	r.Commit("test: add a case that passes without the fix")
	return r
}

func readReport(t *testing.T, path string) domain.Report {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var rep domain.Report
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return rep
}

func hasWarning(rep domain.Report, kind domain.WarningKind, detail string) bool {
	for _, w := range rep.Warnings {
		if w.Kind == kind && strings.Contains(w.Detail, detail) {
			return true
		}
	}
	return false
}
