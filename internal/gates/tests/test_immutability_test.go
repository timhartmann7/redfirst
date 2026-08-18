package tests_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/gates/tests"
)

// testRules is the shape most cases share: one pattern list, one fixture list.
func testRules(mode, fixturesMode domain.Immutability) rules {
	return rules{
		patterns:     []string{"**/*.test.js", "tests/**"},
		fixtures:     []string{"**/__snapshots__/**", "**/*.snap", "**/testdata/**"},
		immutability: mode,
		fixturesMode: fixturesMode,
	}
}

// TestTestImmutability_AppendOnlyPassesZeroDeletedLinesAndFailsOnOne is the
// first acceptance criterion of the slice, in both directions: the same file,
// the same mode, one deleted line between a pass and a refusal.
func TestTestImmutability_AppendOnlyPassesZeroDeletedLinesAndFailsOnOne(t *testing.T) {
	t.Parallel()

	cfg := testsConfig(t, testRules(domain.ImmutabilityAppendOnly, domain.ImmutabilityWarn))
	appended := "test('applies a discount', () => {})"

	t.Run("ZeroDeletedLinesIsAnAppend", func(t *testing.T) {
		t.Parallel()

		res := runGate(t, tests.NewTestImmutability(cfg), cfg, modified("src/total.test.js", 0, appended))

		assertPass(t, res)
		if want := "append-only · 0 removed"; res.Message != want {
			t.Errorf("message = %q, want %q", res.Message, want)
		}
	})

	t.Run("OneDeletedLineIsNot", func(t *testing.T) {
		t.Parallel()

		res := runGate(t, tests.NewTestImmutability(cfg), cfg, modified("src/total.test.js", 1, appended))

		assertRefusal(t, res, domain.RemediationRevertPaths)
		if want := "append-only · 1 shortened"; res.Message != want {
			t.Errorf("message = %q, want %q", res.Message, want)
		}
		assertEvidence(t, res, "src/total.test.js", "test file lost 1 line, append-only allows none")
	})
}

// TestTestImmutability_SnapshotEditWarnsByDefaultAndFailsUnderStrict is the
// second acceptance criterion. The diff is the same edit to the same snapshot;
// only tests.fixtures_immutability moves.
func TestTestImmutability_SnapshotEditWarnsByDefaultAndFailsUnderStrict(t *testing.T) {
	t.Parallel()

	// The cheapest way to turn a suite green while fixing nothing: the snapshot
	// stops disagreeing with the output.
	snapshot := modified("src/__snapshots__/total.test.js.snap", 3, "exports[`total`] = `0`;")

	cases := []struct {
		name       string
		mode       domain.Immutability
		want       domain.GateStatus
		wantPrefix string
	}{
		{"DefaultsOnlyFlagIt", domain.ImmutabilityWarn, domain.StatusWarn, "warn · 1 edited"},
		{"StrictRefusesIt", domain.ImmutabilityStrict, domain.StatusFail, "strict · 1 edited"},
		{"AppendOnlyRefusesTheRewrittenLines", domain.ImmutabilityAppendOnly, domain.StatusFail, "append-only · 1 shortened"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := testsConfig(t, testRules(domain.ImmutabilityAppendOnly, tc.mode))
			res := runGate(t, tests.NewTestImmutability(cfg), cfg, snapshot)

			assertStatus(t, res, tc.want)
			if res.Message != tc.wantPrefix {
				t.Errorf("message = %q, want %q", res.Message, tc.wantPrefix)
			}
			if got := evidenceFiles(res); !slices.Equal(got, []string{snapshot.Path}) {
				t.Errorf("evidence = %v, want the snapshot: a warning that names no file is not a warning", got)
			}
			// A warning is not a refusal, however loud it reads.
			wantRemediation := domain.RemediationRevertPaths
			if tc.want == domain.StatusWarn {
				wantRemediation = ""
			}
			if res.Remediation != wantRemediation {
				t.Errorf("remediation = %q, want %q", res.Remediation, wantRemediation)
			}
		})
	}
}

func TestTestImmutability_StrictRefusesEveryTouchOfAnExistingTestFile(t *testing.T) {
	t.Parallel()

	cfg := testsConfig(t, testRules(domain.ImmutabilityStrict, domain.ImmutabilityStrict))
	cases := []struct {
		name   string
		file   domain.FileChange
		detail string
	}{
		{
			"Modified",
			modified("src/total.test.js", 0, "test('x', () => {})"),
			"test file modified, tests.immutability is strict",
		},
		{"Deleted", removed("src/total.test.js", 12), "test file deleted"},
		{
			"RenamedInsideTheTestSet",
			renamed("src/total.test.js", "src/sum.test.js", 0),
			"test file renamed to src/sum.test.js, tests.immutability is strict",
		},
		{
			"BinaryFixture",
			binary("src/testdata/logo.png", domain.FileModified),
			"fixture modified, tests.fixtures_immutability is strict",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			res := runGate(t, tests.NewTestImmutability(cfg), cfg, tc.file)

			assertRefusal(t, res, domain.RemediationRevertPaths)
			assertEvidence(t, res, existing(tc.file), tc.detail)
		})
	}
}

// TestTestImmutability_LeavesEveryFileTheDiffAddedAlone holds the line the gate
// may not cross: writing a test is legal under every mode, so a file that was
// not in the repository at the merge base has nothing for the gate to protect.
func TestTestImmutability_LeavesEveryFileTheDiffAddedAlone(t *testing.T) {
	t.Parallel()

	for _, mode := range []domain.Immutability{domain.ImmutabilityStrict, domain.ImmutabilityAppendOnly} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()

			cfg := testsConfig(t, testRules(mode, mode))
			res := runGate(t, tests.NewTestImmutability(cfg), cfg,
				added("src/discount.test.js", "test('applies a discount', () => {})"),
				copied("src/total.test.js", "src/subtotal.test.js"),
				added("src/__snapshots__/discount.test.js.snap", "exports[`discount`] = `5`;"),
			)

			assertPass(t, res)
		})
	}
}

// TestTestImmutability_RenameOutOfTheTestSetIsARemoval closes the quiet way of
// deleting a test: the file keeps every line it ever had and stops being
// collected, so a rule counting deleted lines sees an innocent diff.
func TestTestImmutability_RenameOutOfTheTestSetIsARemoval(t *testing.T) {
	t.Parallel()

	cfg := testsConfig(t, testRules(domain.ImmutabilityAppendOnly, domain.ImmutabilityWarn))
	cases := []struct {
		name   string
		file   domain.FileChange
		status domain.GateStatus
		detail string
	}{
		{
			name:   "OutOfThePatternList",
			file:   renamed("src/total.test.js", "src/total.test.js.bak", 0),
			status: domain.StatusFail,
			detail: "test file renamed to src/total.test.js.bak, outside tests.patterns",
		},
		{
			name:   "StillInsideThePatternList",
			file:   renamed("src/total.test.js", "src/sum.test.js", 0),
			status: domain.StatusPass,
		},
		{
			// The fixture list keeps its own mode, and warn only flags.
			name:   "OutOfTheFixtureList",
			file:   renamed("src/__snapshots__/total.snap", "src/total.txt", 0),
			status: domain.StatusWarn,
			detail: "fixture renamed to src/total.txt, outside tests.fixtures",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			res := runGate(t, tests.NewTestImmutability(cfg), cfg, tc.file)

			assertStatus(t, res, tc.status)
			if tc.status == domain.StatusPass {
				return
			}
			assertEvidence(t, res, existing(tc.file), tc.detail)
		})
	}
}

// TestTestImmutability_BinaryTestFileCannotSatisfyAppendOnly covers the one
// file append-only cannot measure. Git reports no line counts for a binary
// file, so zero deleted lines there means nothing was counted rather than
// nothing was removed, and a golden image swapped wholesale would walk through.
func TestTestImmutability_BinaryTestFileCannotSatisfyAppendOnly(t *testing.T) {
	t.Parallel()

	cfg := testsConfig(t, testRules(domain.ImmutabilityAppendOnly, domain.ImmutabilityAppendOnly))

	res := runGate(t, tests.NewTestImmutability(cfg), cfg, binary("src/testdata/report.golden", domain.FileModified))

	assertRefusal(t, res, domain.RemediationRevertPaths)
	assertEvidence(t, res, "src/testdata/report.golden",
		"fixture replaced, append-only cannot read an addition out of a binary file")
}

func TestTestImmutability_PassesWhenTheDiffHoldsNoTestSurface(t *testing.T) {
	t.Parallel()

	cfg := testsConfig(t, testRules(domain.ImmutabilityStrict, domain.ImmutabilityStrict))
	cases := []struct {
		name  string
		files []domain.FileChange
	}{
		{"EmptyDiff", nil},
		{
			"SourceFilesOnly",
			[]domain.FileChange{
				modified("src/total.js", 4, "return sum * rate"),
				removed("src/legacy.js", 30),
				binary("assets/logo.png", domain.FileModified),
				renamed("src/old.js", "src/new.js", 2, "export const rate = 2"),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			res := runGate(t, tests.NewTestImmutability(cfg), cfg, tc.files...)

			assertPass(t, res)
			if want := "strict · 0 removed"; res.Message != want {
				t.Errorf("message = %q, want %q", res.Message, want)
			}
		})
	}
}

// TestTestImmutability_ReportsEveryOffenceAndOpensWithTheBlockingMode covers a
// diff that breaks both rules at once: the counts hold every file, and the
// message opens with the mode that decided the verdict rather than the one that
// only asked for a look.
func TestTestImmutability_ReportsEveryOffenceAndOpensWithTheBlockingMode(t *testing.T) {
	t.Parallel()

	cfg := testsConfig(t, testRules(domain.ImmutabilityAppendOnly, domain.ImmutabilityWarn))

	res := runGate(t, tests.NewTestImmutability(cfg), cfg,
		modified("src/__snapshots__/total.snap", 2, "exports[`total`] = `0`;"),
		removed("src/total.test.js", 20),
		modified("tests/order.test.js", 3, "it('holds', () => {})"),
	)

	assertRefusal(t, res, domain.RemediationRevertPaths)
	if want := "append-only · 1 removed, 1 edited, 1 shortened"; res.Message != want {
		t.Errorf("message = %q, want %q", res.Message, want)
	}
	want := []string{"src/__snapshots__/total.snap", "src/total.test.js", "tests/order.test.js"}
	if got := evidenceFiles(res); !slices.Equal(got, want) {
		t.Errorf("evidence = %v, want %v in diff order", got, want)
	}
}

// TestTestImmutability_MessageAlwaysOpensWithAMode keeps the report line in the
// shape section 9 of the spec prints it in, whatever the diff held.
func TestTestImmutability_MessageAlwaysOpensWithAMode(t *testing.T) {
	t.Parallel()

	cfg := testsConfig(t, testRules(domain.ImmutabilityAppendOnly, domain.ImmutabilityWarn))
	res := runGate(t, tests.NewTestImmutability(cfg), cfg)

	if !strings.HasPrefix(res.Message, "append-only · ") {
		t.Errorf("message = %q, want it to open with the mode and a separator", res.Message)
	}
}
