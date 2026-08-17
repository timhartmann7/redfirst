package tests_test

import (
	"slices"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/gates/tests"
)

// requireNew is the config of whoever judges an agent branch: the built-in
// defaults leave this gate off.
func requireNew(t *testing.T, on bool) domain.Config {
	t.Helper()

	return testsConfig(t, rules{
		patterns:   []string{"**/*.test.js", "**/*_test.go", "tests/**"},
		fixtures:   []string{"**/*.snap"},
		requireNew: on,
	})
}

// TestNewTestPresent_CountsAddedLinesInAnExistingTestFile is the third
// acceptance criterion of the slice. A Go regression test goes into the
// existing total_test.go and a pytest one into the existing test_order.py:
// demanding a new file would mean one test file per fix.
func TestNewTestPresent_CountsAddedLinesInAnExistingTestFile(t *testing.T) {
	t.Parallel()

	cfg := requireNew(t, true)
	cases := []struct {
		name string
		file domain.FileChange
	}{
		{"AppendedToAnExistingFile", modified("src/total_test.go", 0, "func TestDiscount(t *testing.T) {}")},
		{"WrittenIntoANewFile", added("src/discount.test.js", "test('applies a discount', () => {})")},
		{
			// A rename carries an existing file to a new path, and the lines
			// the diff added to it on the way count like any other.
			name: "AddedWhileTheFileMoved",
			file: renamed("src/total.test.js", "src/sum.test.js", 0, "test('sums', () => {})"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			res := runGate(t, tests.NewNewTestPresent(cfg), cfg,
				modified("src/total.js", 2, "return sum * rate"), tc.file)

			assertPass(t, res)
			if res.Message != tc.file.Path {
				t.Errorf("message = %q, want the file that satisfied the gate, %q", res.Message, tc.file.Path)
			}
		})
	}
}

func TestNewTestPresent_RefusesADiffWithNoAddedTestLine(t *testing.T) {
	t.Parallel()

	cfg := requireNew(t, true)
	cases := []struct {
		name  string
		files []domain.FileChange
	}{
		{"EmptyDiff", nil},
		{"SourceFilesOnly", []domain.FileChange{modified("src/total.js", 2, "return sum * rate")}},
		{"ATestFileDeleted", []domain.FileChange{removed("src/total.test.js", 20)}},
		{"ATestFileMovedAndNothingWritten", []domain.FileChange{renamed("src/a.test.js", "src/b.test.js", 0)}},
		{
			// An empty file tests nothing, whatever its name says. The gate
			// asks for a line, and the file that holds none has not got one.
			name:  "AnEmptyNewTestFile",
			files: []domain.FileChange{added("src/discount.test.js")},
		},
		{
			// A snapshot is test surface rather than a test: it holds no case,
			// so it cannot be the new one.
			name:  "ASnapshotAlone",
			files: []domain.FileChange{added("src/__snapshots__/total.snap", "exports[`total`] = `15`;")},
		},
		{
			name:  "ABinaryFileNamedLikeATest",
			files: []domain.FileChange{binary("tests/report.test.js", domain.FileAdded)},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			res := runGate(t, tests.NewNewTestPresent(cfg), cfg, tc.files...)

			assertRefusal(t, res, domain.RemediationAddTest)
			if want := "the diff adds no line to a file under tests.patterns"; res.Message != want {
				t.Errorf("message = %q, want %q", res.Message, want)
			}
		})
	}
}

// TestNewTestPresent_SkipsWhileRequireNewIsFalse holds the reason the gate ships
// off: on tier 1 it faces human PRs, where a refactor with no new test is a
// legitimate diff, and a gate that fires on one gets the tool deleted.
func TestNewTestPresent_SkipsWhileRequireNewIsFalse(t *testing.T) {
	t.Parallel()

	cfg := requireNew(t, false)

	res := runGate(t, tests.NewNewTestPresent(cfg), cfg, modified("src/total.js", 2, "return sum * rate"))

	assertStatus(t, res, domain.StatusSkip)
	if want := "tests.require_new is false"; res.Reason != want {
		t.Errorf("reason = %q, want %q: a skip says which choice switched the gate off", res.Reason, want)
	}
	if res.Remediation != "" {
		t.Errorf("remediation = %q, want none on a gate that did not run", res.Remediation)
	}
}

func TestNewTestPresent_ReadsTheDiffAlone(t *testing.T) {
	t.Parallel()

	cfg := requireNew(t, true)

	got := tests.NewNewTestPresent(cfg).Requires()
	if !slices.Equal(got, []domain.Capability{domain.CapDiff}) {
		t.Errorf("Requires = %v, want the diff alone: the gate is static", got)
	}
}
