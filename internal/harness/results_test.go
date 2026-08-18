package harness_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/harness"
)

func resultsFile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "results")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write results: %v", err)
	}
	return path
}

func read(t *testing.T, content string) []harness.Case {
	t.Helper()

	cases, err := harness.ReadResults(resultsFile(t, content))
	if err != nil {
		t.Fatalf("read results: %v", err)
	}
	return cases
}

// TestResults_JUnitReadsEveryOutcomeUnderEitherRoot covers the two shapes real
// runners produce, and the three children that decide an outcome.
func TestResults_JUnitReadsEveryOutcomeUnderEitherRoot(t *testing.T) {
	t.Parallel()

	roots := map[string]string{
		"testsuites": `<testsuites><testsuite name="fake">%s</testsuite></testsuites>`,
		"testsuite":  `<testsuite name="fake">%s</testsuite>`,
	}
	const body = `
	  <testcase classname="src/total.test.js" name="adds prices"/>
	  <testcase classname="src/total.test.js" name="applies a discount"><failure message="boom"/></testcase>
	  <testcase classname="src/total.test.js" name="reaches the api"><error message="no route"/></testcase>
	  <testcase classname="src/total.test.js" name="rounds"><skipped/></testcase>`

	want := []harness.Case{
		{File: "src/total.test.js", Name: "adds prices", Outcome: harness.OutcomePass},
		{File: "src/total.test.js", Name: "applies a discount", Outcome: harness.OutcomeFail},
		{File: "src/total.test.js", Name: "reaches the api", Outcome: harness.OutcomeFail},
		{File: "src/total.test.js", Name: "rounds", Outcome: harness.OutcomeSkip},
	}
	for root, shape := range roots {
		t.Run(root, func(t *testing.T) {
			t.Parallel()

			got := read(t, `<?xml version="1.0"?>`+fmt.Sprintf(shape, body))
			if !slices.Equal(got, want) {
				t.Errorf("cases = %v\nwant %v", got, want)
			}
		})
	}
}

// TestResults_JUnitWithoutNamesLeavesTheCasesUnnamed is the no-case-names
// fixture at the level it starts: a runner that reports totals names nothing,
// and the run has no capability to offer.
func TestResults_JUnitWithoutNamesLeavesTheCasesUnnamed(t *testing.T) {
	t.Parallel()

	got := read(t, `<testsuite name="fake" tests="2" failures="0"/>`)
	if len(got) != 0 {
		t.Fatalf("cases = %v, want none", got)
	}
	res := harness.Result{Cases: got}
	if res.Named() {
		t.Error("a results file that names nothing must not report per-case names")
	}
}

func TestResults_TAPReadsTheDescriptionAfterTheNumber(t *testing.T) {
	t.Parallel()

	got := read(t, `TAP version 13
1..5
ok 1 - adds prices
not ok 2 - applies a discount
ok 3 rounds the total
ok 4 # skip nothing to round
not ok 5 - reaches the api # TODO the route is missing
# a comment nobody reads
    ok 1 - a subtest of the case above
`)

	want := []harness.Case{
		{Name: "adds prices", Outcome: harness.OutcomePass},
		{Name: "applies a discount", Outcome: harness.OutcomeFail},
		{Name: "rounds the total", Outcome: harness.OutcomePass},
		{Name: "", Outcome: harness.OutcomeSkip},
		{Name: "reaches the api", Outcome: harness.OutcomeFail},
	}
	if !slices.Equal(got, want) {
		t.Errorf("cases = %v\nwant %v", got, want)
	}
}

// TestResults_TAPKeepsAWordThatMerelyStartsWithOK holds the grammar at its one
// ambiguous point: a line of prose is not a result.
func TestResults_TAPKeepsAWordThatMerelyStartsWithOK(t *testing.T) {
	t.Parallel()

	got := read(t, "okay, moving on\nnot okay either\nok 1 - the only case here\n")
	if len(got) != 1 || got[0].Name != "the only case here" {
		t.Errorf("cases = %v, want the one real result", got)
	}
}

// TestResults_NothingToReadIsNotAFailure keeps a harness that writes no results
// out of the exit codes: it costs the run a capability, and the runner turns
// that into an unavailable gate rather than a refusal.
func TestResults_NothingToReadIsNotAFailure(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"empty file":           "",
		"whitespace only":      "\n\n   \n",
		"not a format we read": `{"tests": [{"name": "adds prices", "ok": true}]}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := read(t, content); len(got) != 0 {
				t.Errorf("cases = %v, want none", got)
			}
		})
	}

	missing := filepath.Join(t.TempDir(), "never-written")
	got, err := harness.ReadResults(missing)
	if err != nil || len(got) != 0 {
		t.Errorf("a missing results file gave %v, %v", got, err)
	}
}

// TestResults_AByteOrderMarkDoesNotHideTheCases covers the mark some runners
// write ahead of their xml. Read as content it is neither xml nor TAP, and the
// run would come back with no cases at all.
func TestResults_AByteOrderMarkDoesNotHideTheCases(t *testing.T) {
	t.Parallel()

	got := read(t, "\ufeff"+`<testsuite name="fake"><testcase classname="x.js" name="adds prices"/></testsuite>`)

	if len(got) != 1 || got[0].Name != "adds prices" {
		t.Errorf("cases = %v, want the one the file names", got)
	}
}

// TestResults_BrokenXMLIsHarnessFailure separates the two: a file that opens as
// xml claims to be results, and half a file means the runner died mid-write.
func TestResults_BrokenXMLIsHarnessFailure(t *testing.T) {
	t.Parallel()

	_, err := harness.ReadResults(resultsFile(t, `<testsuite name="fake"><testcase name="adds`))
	if !errors.Is(err, domain.ErrHarness) {
		t.Fatalf("truncated xml: %v, want a harness error", err)
	}
	if got := domain.ExitCode(err); got != 2 {
		t.Errorf("exit code = %d, want 2", got)
	}
}
