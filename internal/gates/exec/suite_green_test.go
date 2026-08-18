package exec_test

import (
	"errors"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

// TestSuiteGreen_CleanFixPasses is the shape of a green run: every case the
// suite holds passed, and the line says how many.
func TestSuiteGreen_CleanFixPasses(t *testing.T) {
	t.Parallel()

	v := fixture{head: honestFix}.judge(t)

	got := v.assertPass(t, domain.GateSuiteGreen)
	if got.Message != "2 passed" {
		t.Errorf("message = %q, want the count the suite kept", got.Message)
	}
	if len(got.Warnings) != 0 {
		t.Errorf("warnings = %v, want none", got.Warnings)
	}
}

// TestSuiteGreen_ForeignFlakeIsAWarningAndNotARefusal covers flaky-untouched.
// The agent answers for what it wrote; without this split the first flake it
// inherited costs the orchestrator three attempts and reaches a human as a
// problem that was never the diff's.
func TestSuiteGreen_ForeignFlakeIsAWarningAndNotARefusal(t *testing.T) {
	t.Parallel()

	v := fixture{
		base: func(r *testkit.Repo) { r.Write("src/legacy.test.js", legacyFlaky) },
		head: honestFix,
	}.judge(t)

	got := v.assertPass(t, domain.GateSuiteGreen)
	if v.exit != 0 {
		t.Errorf("exit code = %d, want 0: the flake is not the agent's", v.exit)
	}
	if len(got.Warnings) != 1 {
		t.Fatalf("warnings = %v, want the flake reported once", got.Warnings)
	}
	w := got.Warnings[0]
	if w.Kind != domain.WarnFlaky || w.File != "src/legacy.test.js" {
		t.Errorf("warning = %+v, want a flaky line naming the untouched file", w)
	}
	if got.Message != "2 passed, 1 flaky (untouched)" {
		t.Errorf("message = %q, want the flake counted beside the passes", got.Message)
	}
}

// TestSuiteGreen_TheAgentsOwnFlakeIsARefusal is the other side of the same
// split: a failing case in a file the diff wrote blocks whatever the retry says,
// because a test that passes only sometimes is not a test that passed.
func TestSuiteGreen_TheAgentsOwnFlakeIsARefusal(t *testing.T) {
	t.Parallel()

	v := fixture{head: func(r *testkit.Repo) {
		r.Write("src/total.js", totalFixed)
		r.Write("src/total.test.js", totalTest+flakyCase)
	}}.judge(t)

	got := v.assertRefusal(t, domain.GateSuiteGreen, wantRefusal{
		message:     "1 test red on head, 1 passed",
		remediation: domain.RemediationFixCode,
		exit:        1,
	})
	if len(got.Evidence) != 1 || got.Evidence[0].Detail != "touched by the diff" {
		t.Errorf("evidence = %v, want the case charged to the file the diff wrote", got.Evidence)
	}
}

// TestSuiteGreen_SuiteRedOnBaseTooIsABrokenRepository covers broken-base-suite.
// Exit code 2 exists for this: an orchestrator that charged the agent for a
// repository broken before it arrived would spend its retries on somebody
// else's commit.
func TestSuiteGreen_SuiteRedOnBaseTooIsABrokenRepository(t *testing.T) {
	t.Parallel()

	v := fixture{
		base: func(r *testkit.Repo) { r.Write("src/legacy.test.js", legacyBroken) },
		head: honestFix,
	}.judge(t)

	if !errors.Is(v.err, domain.ErrHarness) {
		t.Fatalf("err = %v, want a harness failure", v.err)
	}
	if v.exit != 2 {
		t.Errorf("exit code = %d, want 2, not 1: the agent did not write this", v.exit)
	}
}

// TestSuiteGreen_KnownFlakyIsOutsideTheGateEntirely covers the escape hatch,
// and where it comes from: the list is read off base, so the agent cannot add
// its own test to it.
func TestSuiteGreen_KnownFlakyIsOutsideTheGateEntirely(t *testing.T) {
	t.Parallel()

	v := fixture{
		cfg:  func(c *domain.Config) { c.Tests.KnownFlaky = []string{"legacy rounding"} },
		base: func(r *testkit.Repo) { r.Write("src/legacy.test.js", legacyBroken) },
		head: honestFix,
	}.judge(t)

	got := v.assertPass(t, domain.GateSuiteGreen)
	if len(got.Warnings) != 0 {
		t.Errorf("warnings = %v, want none: the case is outside the gate", got.Warnings)
	}
	if v.exit != 0 {
		t.Errorf("exit code = %d, want 0", v.exit)
	}
}

// TestSuiteGreen_ATestTheDiffBrokeWithoutTouchingItStillBlocks is the last row
// of the classification table, and the one that keeps the split honest: the
// case survived the retry and was green on base, so the diff broke it even
// though it never wrote a line of the file it sits in.
func TestSuiteGreen_ATestTheDiffBrokeWithoutTouchingItStillBlocks(t *testing.T) {
	t.Parallel()

	v := fixture{
		base: func(r *testkit.Repo) {
			r.Write("src/legacy.js", legacySource)
			r.Write("src/legacy.test.js", legacyRounding)
		},
		head: func(r *testkit.Repo) {
			honestFix(r)
			r.Write("src/legacy.js", legacyRewritten)
		},
	}.judge(t)

	got := v.assertRefusal(t, domain.GateSuiteGreen, wantRefusal{
		message:     "1 test red on head, 2 passed",
		remediation: domain.RemediationFixCode,
		exit:        1,
	})
	if len(got.Evidence) != 1 || got.Evidence[0].Detail != "not touched by the diff, green on base" {
		t.Errorf("evidence = %v, want the case placed on the diff rather than on the repository", got.Evidence)
	}
}
