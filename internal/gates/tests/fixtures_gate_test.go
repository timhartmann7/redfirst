package tests_test

import (
	"testing"

	"github.com/timhartmann7/redfirst/internal/config"
	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/gates/tests"
	"github.com/timhartmann7/redfirst/internal/runner"
)

// TestTestImmutability_DeletedTestFixtureIsRefusedInEveryLineMode drives the
// gate from a real deletion. The defaults run append-only, where a deletion
// carries no line count of its own on the added side and still has to be a
// refusal: what left the suite matters more than how it left.
func TestTestImmutability_DeletedTestFixtureIsRefusedInEveryLineMode(t *testing.T) {
	t.Parallel()

	diff := deletedTest(t)
	fileOf(t, diff, "src/total.test.js", domain.FileDeleted)

	for _, mode := range []domain.Immutability{domain.ImmutabilityAppendOnly, domain.ImmutabilityStrict} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()

			cfg := config.Defaults()
			cfg.Tests.Immutability = mode

			res := runGateOnDiff(t, tests.NewTestImmutability(cfg), cfg, diff)

			assertRefusal(t, res, domain.RemediationRevertPaths)
			if want := string(mode) + " · 1 removed"; res.Message != want {
				t.Errorf("message = %q, want %q", res.Message, want)
			}
			assertEvidence(t, res, "src/total.test.js", "test file deleted")
		})
	}
}

// TestTestImmutability_EditedTestAppendFixturePassesAppendOnlyAndFailsStrict is
// the fixture pair that explains why the defaults are not strict: appending a
// case to the test file next to the fix is the honest shape of a change, and
// strict has no legal diff for it.
func TestTestImmutability_EditedTestAppendFixturePassesAppendOnlyAndFailsStrict(t *testing.T) {
	t.Parallel()

	diff := editedTestAppend(t)
	test := fileOf(t, diff, "src/total.test.js", domain.FileModified)
	if test.Deleted != 0 {
		t.Fatalf("the fixture deletes %d lines from the test file, want a pure append", test.Deleted)
	}

	t.Run("AppendOnly", func(t *testing.T) {
		t.Parallel()

		cfg := config.Defaults()
		assertPass(t, runGateOnDiff(t, tests.NewTestImmutability(cfg), cfg, diff))
	})

	t.Run("Strict", func(t *testing.T) {
		t.Parallel()

		cfg := config.Defaults()
		cfg.Tests.Immutability = domain.ImmutabilityStrict

		res := runGateOnDiff(t, tests.NewTestImmutability(cfg), cfg, diff)

		assertRefusal(t, res, domain.RemediationRevertPaths)
		assertEvidence(t, res, "src/total.test.js", "test file modified, tests.immutability is strict")
	})
}

// TestTestImmutability_UpdatedSnapshotFixtureWarnsInDefaultsAndFailsUnderStrict
// is the second acceptance criterion of the slice, run against the built-in
// lists rather than a config a test wrote. The snapshot matches tests.patterns
// too, because a jest snapshot is named after its test file, so this is also
// where the overlap rule earns its place.
func TestTestImmutability_UpdatedSnapshotFixtureWarnsInDefaultsAndFailsUnderStrict(t *testing.T) {
	t.Parallel()

	diff := updatedSnapshot(t)
	fileOf(t, diff, fixtureSnapshotPath, domain.FileModified)

	cases := []struct {
		name string
		mode domain.Immutability
		want domain.GateStatus
	}{
		{"DefaultsOnlyFlagIt", domain.ImmutabilityWarn, domain.StatusWarn},
		{"StrictRefusesIt", domain.ImmutabilityStrict, domain.StatusFail},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := config.Defaults()
			if got := cfg.Tests.FixturesImmutability; got != domain.ImmutabilityWarn {
				t.Fatalf("the built-in tests.fixtures_immutability is %q, want %q", got, domain.ImmutabilityWarn)
			}
			cfg.Tests.FixturesImmutability = tc.mode

			res := runGateOnDiff(t, tests.NewTestImmutability(cfg), cfg, diff)

			assertStatus(t, res, tc.want)
			assertEvidence(t, res, fixtureSnapshotPath,
				"fixture modified, tests.fixtures_immutability is "+string(tc.mode))

			// The verdict follows the status and nothing else: warn is a line
			// for a reviewer, strict is a blocked merge.
			wantCode := 1
			if tc.want == domain.StatusWarn {
				wantCode = 0
			}
			if got := domain.ExitCode(runner.Outcome([]domain.GateResult{res})); got != wantCode {
				t.Errorf("exit code = %d, want %d", got, wantCode)
			}
		})
	}
}

// TestNoDisabledTests_SkippedTestFixtureNamesTheLineGitGaveIt drives the gate
// from a real append. The number in the report comes from the head file, so a
// reviewer opening src/total.test.js at that line lands on the .skip.
func TestNoDisabledTests_SkippedTestFixtureNamesTheLineGitGaveIt(t *testing.T) {
	t.Parallel()

	cfg := config.Defaults()
	diff := skippedTest(t)
	test := fileOf(t, diff, "src/total.test.js", domain.FileModified)
	want := lineOf(t, test, "test.skip(")

	res := runGateOnDiff(t, tests.NewNoDisabledTests(cfg), cfg, diff)

	assertRefusal(t, res, domain.RemediationRevertPaths)
	if len(res.Evidence) != 1 {
		t.Fatalf("evidence = %+v, want one entry", res.Evidence)
	}
	got := res.Evidence[0]
	if got.File != "src/total.test.js" || got.Line != want {
		t.Errorf("evidence = %s:%d, want src/total.test.js:%d", got.File, got.Line, want)
	}
	if got.Detail != "test.skip('applies a discount', () => {" {
		t.Errorf("detail = %q, want the line the diff added", got.Detail)
	}
}

// TestSuppressionScan_EmptyCatchFixtureWarnsAndStillExitsZero is the fourth
// acceptance criterion against a real diff: an empty catch is almost always a
// silenced symptom, and it is still a human's call rather than the gate's.
func TestSuppressionScan_EmptyCatchFixtureWarnsAndStillExitsZero(t *testing.T) {
	t.Parallel()

	cfg := config.Defaults()
	diff := emptyCatch(t)
	source := fileOf(t, diff, "src/total.js", domain.FileModified)
	want := lineOf(t, source, "catch")

	res := runGateOnDiff(t, tests.NewSuppressionScan(cfg), cfg, diff)

	assertStatus(t, res, domain.StatusWarn)
	if code := domain.ExitCode(runner.Outcome([]domain.GateResult{res})); code != 0 {
		t.Errorf("exit code = %d, want 0: the scan asks for a reviewer, not for a retry", code)
	}
	if len(res.Warnings) != 1 {
		t.Fatalf("warnings = %+v, want one", res.Warnings)
	}
	got := res.Warnings[0]
	if got.Kind != domain.WarnSuppression || got.File != "src/total.js" || got.Line != want {
		t.Errorf("warning = %+v, want a suppression at src/total.js:%d", got, want)
	}
	if got.Detail != "} catch (e) {}" {
		t.Errorf("detail = %q, want the line the diff added", got.Detail)
	}
}
