package tests_test

import (
	"testing"

	"github.com/timhartmann7/redfirst/internal/config"
	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/gates/tests"
	"github.com/timhartmann7/redfirst/internal/runner"
)

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
