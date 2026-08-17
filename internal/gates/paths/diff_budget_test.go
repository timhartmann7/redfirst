package paths_test

import (
	"slices"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/gates/paths"
)

// defaultBudget is the built-in one: wide enough for an ordinary human PR.
var defaultBudget = domain.Budget{MaxFiles: 20, MaxAddedLines: 800, MaxDeletedLines: 400}

// budgetConfig fills the two keys the gate reads.
func budgetConfig(t *testing.T, b domain.Budget, tests []string) domain.Config {
	t.Helper()

	return domain.Config{Budget: b, Tests: domain.Tests{Patterns: patternSet(t, tests)}}
}

func TestDiffBudget_DeclaresItsIdentityAndRequirements(t *testing.T) {
	t.Parallel()

	g := paths.NewDiffBudget(domain.Config{})

	if got := g.ID(); got != domain.GateDiffBudget {
		t.Errorf("ID = %q, want %q", got, domain.GateDiffBudget)
	}
	if got := g.Requires(); !slices.Equal(got, []domain.Capability{domain.CapDiff}) {
		t.Errorf("Requires = %v, want the diff alone", got)
	}
}

// TestDiffBudget_CountsFilesAndLinesAgainstEveryLimit covers the three limits
// and the boundary between them: a diff sitting exactly on a limit is inside
// it, one file past is not.
func TestDiffBudget_CountsFilesAndLinesAgainstEveryLimit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		budget   domain.Budget
		files    []domain.FileChange
		wantMsg  string
		wantFail []string
	}{
		{
			name:    "EmptyDiffSpendsNothing",
			budget:  defaultBudget,
			wantMsg: "0/20 files, +0/800, -0/400",
		},
		{
			name:    "ADiffInsideEveryLimitPasses",
			budget:  defaultBudget,
			files:   []domain.FileChange{modified("src/total.js"), added("src/order.js")},
			wantMsg: "2/20 files, +16/800, -4/400",
		},
		{
			name:    "ADiffSittingExactlyOnTheLimitsPasses",
			budget:  domain.Budget{MaxFiles: 2, MaxAddedLines: 16, MaxDeletedLines: 4},
			files:   []domain.FileChange{modified("src/total.js"), added("src/order.js")},
			wantMsg: "2/2 files, +16/16, -4/4",
		},
		{
			name:     "OneFilePastTheFileLimitRefuses",
			budget:   domain.Budget{MaxFiles: 1, MaxAddedLines: 800, MaxDeletedLines: 400},
			files:    []domain.FileChange{modified("src/total.js"), added("src/order.js")},
			wantMsg:  "2/1 files, +16/800, -4/400",
			wantFail: []string{"2 files, the budget allows 1"},
		},
		{
			name:     "AddedLinesPastTheLimitRefuse",
			budget:   domain.Budget{MaxFiles: 20, MaxAddedLines: 10, MaxDeletedLines: 400},
			files:    []domain.FileChange{modified("src/total.js"), added("src/order.js")},
			wantMsg:  "2/20 files, +16/10, -4/400",
			wantFail: []string{"16 added lines, the budget allows 10"},
		},
		{
			name:     "DeletedLinesPastTheLimitRefuse",
			budget:   domain.Budget{MaxFiles: 20, MaxAddedLines: 800, MaxDeletedLines: 3},
			files:    []domain.FileChange{modified("src/total.js"), added("src/order.js")},
			wantMsg:  "2/20 files, +16/800, -4/3",
			wantFail: []string{"4 deleted lines, the budget allows 3"},
		},
		{
			name:    "EveryLimitPassedAtOnceIsNamedInOneRun",
			budget:  domain.Budget{MaxFiles: 1, MaxAddedLines: 10, MaxDeletedLines: 3},
			files:   []domain.FileChange{modified("src/total.js"), added("src/order.js")},
			wantMsg: "2/1 files, +16/10, -4/3",
			wantFail: []string{
				"2 files, the budget allows 1",
				"16 added lines, the budget allows 10",
				"4 deleted lines, the budget allows 3",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := budgetConfig(t, tc.budget, nil)
			res := runGate(t, paths.NewDiffBudget(cfg), cfg, tc.files...)

			if res.Message != tc.wantMsg {
				t.Errorf("message = %q, want %q", res.Message, tc.wantMsg)
			}
			if len(tc.wantFail) == 0 {
				assertPass(t, res)
				return
			}
			assertOverrun(t, res, tc.wantFail)
		})
	}
}

// TestDiffBudget_LeavesGeneratedAndTestFilesOutOfTheCount holds the second S1
// acceptance criterion. A generated file nobody wrote and a test the
// neighbouring gate demands both stay outside the limits.
func TestDiffBudget_LeavesGeneratedAndTestFilesOutOfTheCount(t *testing.T) {
	t.Parallel()

	generated := domain.FileChange{Path: "api/schema.pb.go", Status: domain.FileModified, Added: 5000, Generated: true}
	testFile := domain.FileChange{Path: "src/total.test.js", Status: domain.FileAdded, Added: 4000}

	cases := []struct {
		name    string
		files   []domain.FileChange
		wantMsg string
	}{
		{
			name:    "ALinguistGeneratedFileIsNeitherCountedNorCharged",
			files:   []domain.FileChange{generated, modified("src/total.js")},
			wantMsg: "1/20 files, +6/800, -4/400",
		},
		{
			name:    "ATestFileStaysOutOfTheCount",
			files:   []domain.FileChange{testFile, modified("src/total.js")},
			wantMsg: "1/20 files, +6/800, -4/400",
		},
		{
			name:    "ADiffOfNothingButThoseTwoIsEmptyToTheBudget",
			files:   []domain.FileChange{generated, testFile},
			wantMsg: "0/20 files, +0/800, -0/400",
		},
		{
			name:    "ATestFileRenamedIntoPlaceStaysOutToo",
			files:   []domain.FileChange{renamed("src/total.js", "src/total.test.js")},
			wantMsg: "0/20 files, +0/800, -0/400",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := budgetConfig(t, defaultBudget, []string{"**/*.test.*"})
			res := runGate(t, paths.NewDiffBudget(cfg), cfg, tc.files...)

			assertPass(t, res)
			if res.Message != tc.wantMsg {
				t.Errorf("message = %q, want %q", res.Message, tc.wantMsg)
			}
		})
	}
}

// TestDiffBudget_CountsAFileWithoutLinesAsOneFile covers the two changes git
// reports no line counts for. Both spend a file and nothing else, so a diff of
// binaries cannot slip past the limits unseen.
func TestDiffBudget_CountsAFileWithoutLinesAsOneFile(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		files   []domain.FileChange
		wantMsg string
	}{
		{
			name:    "BinaryFile",
			files:   []domain.FileChange{binaryFile("assets/logo.png")},
			wantMsg: "1/20 files, +0/800, -0/400",
		},
		{
			name:    "PureRename",
			files:   []domain.FileChange{{Path: "src/b.js", OldPath: "src/a.js", Status: domain.FileRenamed}},
			wantMsg: "1/20 files, +0/800, -0/400",
		},
		{
			name:    "DeletedFileSpendsItsDeletedLines",
			files:   []domain.FileChange{deleted("src/legacy.js")},
			wantMsg: "1/20 files, +0/800, -12/400",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := budgetConfig(t, defaultBudget, nil)
			res := runGate(t, paths.NewDiffBudget(cfg), cfg, tc.files...)

			assertPass(t, res)
			if res.Message != tc.wantMsg {
				t.Errorf("message = %q, want %q", res.Message, tc.wantMsg)
			}
		})
	}
}

// assertOverrun checks a refusal that names every limit the diff passed. The
// gate always refuses; lowering it to a warning is severity's job, and the
// defaults do exactly that.
func assertOverrun(t *testing.T, res domain.GateResult, want []string) {
	t.Helper()

	if res.Status != domain.StatusFail {
		t.Fatalf("status = %q, want %q", res.Status, domain.StatusFail)
	}
	if res.Remediation != domain.RemediationReduceScope {
		t.Errorf("remediation = %q, want %q", res.Remediation, domain.RemediationReduceScope)
	}
	got := make([]string, 0, len(res.Evidence))
	for _, e := range res.Evidence {
		got = append(got, e.Detail)
	}
	if !slices.Equal(got, want) {
		t.Errorf("evidence = %v, want %v", got, want)
	}
}
