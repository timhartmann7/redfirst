package domain_test

import (
	"slices"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// runs builds a probe result out of one outcome list per run.
func runs(perRun ...[]domain.Case) domain.Runs {
	out := make(domain.Runs, 0, len(perRun))
	for i, cases := range perRun {
		green := !slices.ContainsFunc(cases, func(c domain.Case) bool { return c.Outcome == domain.CaseFail })
		out = append(out, domain.Run{Index: i + 1, Green: green, Cases: cases})
	}
	return out
}

func namedCase(name string, outcome domain.CaseOutcome) domain.Case {
	return domain.Case{File: "src/total.test.js", Name: name, Outcome: outcome}
}

// TestRuns_AbsentCaseIsNotAPassingCase holds the direction every ambiguity in
// this project resolves: a run that never mentioned a case says nothing in the
// case's favour, and red-green reads that as a probe that did not agree.
func TestRuns_AbsentCaseIsNotAPassingCase(t *testing.T) {
	t.Parallel()

	got := runs(
		[]domain.Case{namedCase("applies a discount", domain.CaseFail)},
		[]domain.Case{},
		[]domain.Case{namedCase("applies a discount", domain.CaseFail)},
	).Outcomes("applies a discount")

	want := domain.Outcomes{domain.CaseFail, domain.CaseAbsent, domain.CaseFail}
	if !slices.Equal(got, want) {
		t.Errorf("outcomes = %v, want %v", got, want)
	}
	if got.All(domain.CaseFail) {
		t.Error("a probe that dropped the case in one run reads as red in all of them")
	}
}

func TestOutcomes_AllDemandsEveryRunAndAnEmptySetProvesNothing(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		outcomes domain.Outcomes
		want     bool
	}{
		{"EveryRunAgrees", domain.Outcomes{domain.CaseFail, domain.CaseFail}, true},
		{"OneRunDisagrees", domain.Outcomes{domain.CaseFail, domain.CasePass}, false},
		{"ASkipIsNotAFailure", domain.Outcomes{domain.CaseFail, domain.CaseSkip}, false},
		{"NoRunsAtAll", domain.Outcomes{}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.outcomes.All(domain.CaseFail); got != tc.want {
				t.Errorf("All(fail) = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRuns_NamesAreDeduplicatedInFirstSeenOrder(t *testing.T) {
	t.Parallel()

	// Two runs on the same input have to order the report the same way, so the
	// order is the one the runs produced rather than a map's.
	got := runs(
		[]domain.Case{namedCase("adds prices", domain.CasePass), namedCase("applies a discount", domain.CaseFail)},
		[]domain.Case{namedCase("applies a discount", domain.CaseFail), {Name: "", Outcome: domain.CasePass}},
	).Names()

	want := []string{"adds prices", "applies a discount"}
	if !slices.Equal(got, want) {
		t.Errorf("names = %v, want %v: an unnamed case is not a name", got, want)
	}
}

func TestRuns_AllGreenRejectsAProbeThatNeverRan(t *testing.T) {
	t.Parallel()

	if (domain.Runs{}).AllGreen() {
		t.Error("an empty probe reported green; a run that never happened proves nothing")
	}
	if (domain.Runs{{Green: true}, {Green: false}}).AllGreen() {
		t.Error("one red run left the probe green")
	}
	if !(domain.Runs{{Green: true}}).AllGreen() {
		t.Error("a single green run did not read as green")
	}
}

func TestRuns_NamedFollowsTheCaseNamesAndNotTheCaseCount(t *testing.T) {
	t.Parallel()

	nameless := runs([]domain.Case{{File: "src/total.test.js", Outcome: domain.CasePass}})
	if nameless.Named() {
		t.Error("results carrying a file and no case name counted as named")
	}
	if !runs([]domain.Case{namedCase("adds prices", domain.CasePass)}).Named() {
		t.Error("a named case did not count as named")
	}
}

func TestRun_FailedKeepsOnlyTheCasesThatFailed(t *testing.T) {
	t.Parallel()

	run := runs([]domain.Case{
		namedCase("adds prices", domain.CasePass),
		namedCase("applies a discount", domain.CaseFail),
		namedCase("rounds the total", domain.CaseSkip),
	})[0]

	failed := run.Failed()
	if len(failed) != 1 || failed[0].Name != "applies a discount" {
		t.Errorf("failed = %v, want the one failing case", failed)
	}
}

func TestDiff_TestSurfaceCountsBothPathsOfARename(t *testing.T) {
	t.Parallel()

	patterns, err := domain.NewPatternSet([]string{"**/*.test.js"})
	if err != nil {
		t.Fatalf("patterns: %v", err)
	}
	cfg := domain.Config{Tests: domain.Tests{Patterns: patterns}}
	diff := domain.Diff{Files: []domain.FileChange{
		{Path: "src/total.js", Status: domain.FileModified},
		// A test moved out of the surface removes the same insurance as one
		// deleted, so the old path has to count.
		{Path: "src/total.txt", OldPath: "src/total.test.js", Status: domain.FileRenamed},
	}}

	got := diff.TestSurface(cfg)
	if len(got) != 1 || got[0].OldPath != "src/total.test.js" {
		t.Errorf("test surface = %v, want the renamed test alone", got)
	}
}

func TestFileChange_BaseAndHeadPathsFollowTheStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		change     domain.FileChange
		wantBase   string
		wantOnBase bool
		wantHead   string
		wantOnHead bool
	}{
		{
			name:     "AdditionExistsOnHeadAlone",
			change:   domain.FileChange{Path: "src/discount.test.js", Status: domain.FileAdded},
			wantHead: "src/discount.test.js", wantOnHead: true,
		},
		{
			name:     "DeletionExistsOnBaseAlone",
			change:   domain.FileChange{Path: "src/total.test.js", Status: domain.FileDeleted},
			wantBase: "src/total.test.js", wantOnBase: true,
		},
		{
			name: "RenameCarriesADifferentPathOnEachSide",
			change: domain.FileChange{
				Path: "src/sum.test.js", OldPath: "src/total.test.js", Status: domain.FileRenamed,
			},
			wantBase: "src/total.test.js", wantOnBase: true,
			wantHead: "src/sum.test.js", wantOnHead: true,
		},
		{
			name:     "AStatusNobodyExpectsCountsAsExisting",
			change:   domain.FileChange{Path: "src/total.test.js", Status: domain.FileUnknown},
			wantBase: "src/total.test.js", wantOnBase: true,
			wantHead: "src/total.test.js", wantOnHead: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			base, onBase := tc.change.BasePath()
			if base != tc.wantBase || onBase != tc.wantOnBase {
				t.Errorf("BasePath = %q, %v; want %q, %v", base, onBase, tc.wantBase, tc.wantOnBase)
			}
			head, onHead := tc.change.HeadPath()
			if head != tc.wantHead || onHead != tc.wantOnHead {
				t.Errorf("HeadPath = %q, %v; want %q, %v", head, onHead, tc.wantHead, tc.wantOnHead)
			}
		})
	}
}

func TestRuns_FileAndOutcomeLookupsAnswerForACaseTheReportHasToName(t *testing.T) {
	t.Parallel()

	runs := runs(
		[]domain.Case{{Name: "adds prices", Outcome: domain.CasePass}},
		[]domain.Case{namedCase("adds prices", domain.CaseFail)},
	)

	// The first run named no file and the second did. A report line that fell
	// back to the empty one would print a case nobody can find.
	if got := runs.File("adds prices"); got != "src/total.test.js" {
		t.Errorf("File = %q, want the file some run did attribute the case to", got)
	}
	if got := runs.File("never ran"); got != "" {
		t.Errorf("File of an unknown case = %q, want empty", got)
	}
	if got := runs.Outcomes("adds prices").Strings(); len(got) != 2 || got[0] != "pass" || got[1] != "fail" {
		t.Errorf("Strings = %v, want the outcomes run by run", got)
	}
}

func TestRuns_AnyGreenSeparatesAMixedProbeFromARedOne(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		runs domain.Runs
		want bool
	}{
		{"EveryRunRed", domain.Runs{{Green: false}, {Green: false}}, false},
		{"OneRunGreen", domain.Runs{{Green: false}, {Green: true}}, true},
		{"NoRunsAtAll", domain.Runs{}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.runs.AnyGreen(); got != tc.want {
				t.Errorf("AnyGreen = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestOutcomes_AnyFindsTheOneRunThatDisagreed(t *testing.T) {
	t.Parallel()

	outcomes := domain.Outcomes{domain.CasePass, domain.CaseFail, domain.CasePass}

	if !outcomes.Any(domain.CaseFail) {
		t.Error("Any(fail) missed the run that failed")
	}
	if outcomes.Any(domain.CaseSkip) {
		t.Error("Any(skip) found a skip nobody reported")
	}
}
