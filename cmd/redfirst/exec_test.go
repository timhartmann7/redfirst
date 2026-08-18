package main

import (
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

// TestVerify_TierTwoRunsTheHarnessAndAcquitsAnHonestFix is the tier 2 shape end
// to end: base carries .redfirst/, the run discovers it, brings the environment
// up, probes the added case on both trees and accuses nobody.
func TestVerify_TierTwoRunsTheHarnessAndAcquitsAnHonestFix(t *testing.T) {
	t.Parallel()

	rep := runReport(t, testkit.HookedFix(t).Dir)

	if rep.Tier != 2 || rep.ExitCode != 0 {
		t.Fatalf("tier = %d exit = %d, want tier 2 and a pass", rep.Tier, rep.ExitCode)
	}
	for _, id := range []domain.GateID{domain.GateSuiteGreen, domain.GateRedGreen} {
		if got := gateByID(t, rep, id); got.Status != domain.StatusPass {
			t.Errorf("%s is %q: %s %s", id, got.Status, got.Reason, got.Message)
		}
	}
	// The first line of the report has to carry these: somebody digging into a
	// strange refusal should not have to guess how the services were treated.
	if rep.EnvMode != "fresh" {
		t.Errorf("env_mode = %q, want fresh: the fixture carries no env-reset.sh", rep.EnvMode)
	}
	if rep.ProbeRuns != 3 {
		t.Errorf("probe_runs = %d, want the configured 3", rep.ProbeRuns)
	}
}

// TestVerify_RenamedSymbolPassesTestImmutabilityInCasesMode is the
// renamed-symbol fixture. A refactor that renames a covered function has to
// edit the test file, and both line-based modes refuse that: the agent has no
// legal diff, a retry does not help, and a human overrides the refusal by hand.
// Mode cases judges the outcome instead, and the case name survives the rename.
//
// red-green refuses the same diff on its own terms, and that is the spec's
// answer rather than an accident here: a diff that touched test files and added
// no case has nothing to prove a bug was caught.
func TestVerify_RenamedSymbolPassesTestImmutabilityInCasesMode(t *testing.T) {
	t.Parallel()

	rep := runReport(t, testkit.RenamedSymbol(t).Dir)

	got := gateByID(t, rep, domain.GateTestImmutability)
	if got.Status != domain.StatusPass {
		t.Fatalf("test-immutability is %q on a rename: %s", got.Status, got.Message)
	}
	if got.Message != "cases · 0 removed, 0 regressed" {
		t.Errorf("message = %q, want the mode and both counts at zero", got.Message)
	}

	redGreen := gateByID(t, rep, domain.GateRedGreen)
	if redGreen.Remediation != domain.RemediationAddTest {
		t.Errorf("red-green remediation = %q, want %q", redGreen.Remediation, domain.RemediationAddTest)
	}
	if rep.ExitCode != 1 {
		t.Errorf("exit code = %d, want 1", rep.ExitCode)
	}
}

// TestVerify_NoHooksLeavesCasesModeDowngradedAndTheVerdictValid is the other
// end of the same fixture: without hooks the mode cannot run, so it falls back
// to append-only, which refuses the rename. The downgrade moves toward
// strictness, so invariant 2 holds, and it may not happen in silence.
func TestVerify_NoHooksLeavesCasesModeDowngradedAndTheVerdictValid(t *testing.T) {
	t.Parallel()

	rep := runReport(t, testkit.RenamedSymbol(t).Dir, "--no-hooks")

	got := gateByID(t, rep, domain.GateTestImmutability)
	if got.Status != domain.StatusFail {
		t.Errorf("test-immutability is %q under append-only, want the deleted lines refused", got.Status)
	}
	if configWarning(rep) == "" {
		t.Error("cases was downgraded without a line in the report")
	}
}
