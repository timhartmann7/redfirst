package tests_test

import (
	"testing"

	"github.com/timhartmann7/redfirst/internal/config"
	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/gates/tests"
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
