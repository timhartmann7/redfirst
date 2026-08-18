package tests_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/timhartmann7/redfirst/internal/config"
	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/gates/tests"
)

// Which mode governs which file, and what happens to a mode this slice cannot
// apply. The rules themselves live next door.

// TestTestImmutability_FixtureListSettlesAPathBothListsMatch pins the overlap
// rule, and the built-in defaults are what make it necessary: a jest snapshot
// is named after its test file, so `total.test.js.snap` falls under the pattern
// `**/*.test.*` as surely as the test does. Read as a test file it would leave
// tests.fixtures_immutability governing nothing.
func TestTestImmutability_FixtureListSettlesAPathBothListsMatch(t *testing.T) {
	t.Parallel()

	cfg := config.Defaults()
	cfg.Tests.Immutability = domain.ImmutabilityStrict
	cfg.Tests.FixturesImmutability = domain.ImmutabilityWarn
	snapshot := "src/__snapshots__/total.test.js.snap"

	if !cfg.Tests.Patterns.Match(snapshot) {
		t.Fatalf("%s no longer matches tests.patterns: the overlap this test pins is gone", snapshot)
	}

	res := runGate(t, tests.NewTestImmutability(cfg), cfg, modified(snapshot, 1, "exports[`total`] = `0`;"))

	assertStatus(t, res, domain.StatusWarn)
	assertEvidence(t, res, snapshot, "fixture modified, tests.fixtures_immutability is warn")
}

func TestTestImmutability_RequiresCaseNamesWhenEitherModeIsCases(t *testing.T) {
	t.Parallel()

	// The modes are fixed at construction: a gate may never look at its own
	// inputs during Run to decide what it needed.
	cases := []struct {
		name     string
		mode     domain.Immutability
		fixtures domain.Immutability
		want     []domain.Capability
	}{
		{"TestFilesJudgeOutcomes", domain.ImmutabilityCases, domain.ImmutabilityWarn, []domain.Capability{
			domain.CapDiff, domain.CapHooks, domain.CapCaseNames,
		}},
		{"TestSurfaceJudgesOutcomes", domain.ImmutabilityAppendOnly, domain.ImmutabilityCases, []domain.Capability{
			domain.CapDiff, domain.CapHooks, domain.CapCaseNames,
		}},
		{"StrictReadsLines", domain.ImmutabilityStrict, domain.ImmutabilityStrict, []domain.Capability{domain.CapDiff}},
		{"AppendOnlyReadsLines", domain.ImmutabilityAppendOnly, domain.ImmutabilityWarn, []domain.Capability{domain.CapDiff}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := testsConfig(t, testRules(tc.mode, tc.fixtures))

			if got := tests.NewTestImmutability(cfg).Requires(); !slices.Equal(got, tc.want) {
				t.Errorf("Requires = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestTestImmutability_CasesModeWithoutAnEnvironmentIsOurBugNotAPass holds the
// direction the gate may not fail in. Mode cases judges outcomes, the runner
// brings up the harness that produces them, and a gate reaching Run without one
// has been handed something impossible: exit code 4 says so, where a pass would
// acquit a diff nobody checked.
func TestTestImmutability_CasesModeWithoutAnEnvironmentIsOurBugNotAPass(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		mode     domain.Immutability
		fixtures domain.Immutability
		// touched is the file the diff carries, and it decides which of the
		// two lists claims it and so which mode gets applied.
		touched domain.FileChange
	}{
		{"TestFiles", domain.ImmutabilityCases, domain.ImmutabilityWarn, removed("src/total.test.js", 9)},
		{
			"TestSurface", domain.ImmutabilityAppendOnly, domain.ImmutabilityCases,
			removed("src/__snapshots__/total.snap", 4),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := testsConfig(t, testRules(tc.mode, tc.fixtures))
			g := tests.NewTestImmutability(cfg)

			res, err := g.Run(context.Background(), domain.Input{
				Diff:   domain.Diff{Files: []domain.FileChange{tc.touched}},
				Config: cfg,
			})
			if !errors.Is(err, domain.ErrInternal) {
				t.Fatalf("Run = %+v, %v, want an internal error", res, err)
			}
			if domain.ExitCode(err) != 4 {
				t.Errorf("exit code = %d, want 4", domain.ExitCode(err))
			}
		})
	}
}

// TestTestImmutability_ARemovalAnswersToEveryListThatClaimedTheFile closes an
// acquittal the overlap opened. tests.fixtures speaks first for a path both
// lists match, which is right for an edit and wrong for a removal: a real test
// file living under a fixtures path would otherwise leave the repository behind
// the softer of the two rules that covered it, and invariant 2 puts that
// failure above every convenience.
func TestTestImmutability_ARemovalAnswersToEveryListThatClaimedTheFile(t *testing.T) {
	t.Parallel()

	cfg := config.Defaults()
	// A real test file, and both built-in lists claim it: tests/** and
	// **/*.test.* on one side, **/fixtures/** on the other.
	path := "tests/fixtures/checkout.test.js"
	if !cfg.Tests.Patterns.Match(path) || !cfg.Tests.Fixtures.Match(path) {
		t.Fatalf("%s no longer sits in both lists: the overlap this test pins is gone", path)
	}

	cases := []struct {
		name string
		file domain.FileChange
	}{
		{"Deleted", removed(path, 12)},
		{"RenamedOutOfBothLists", renamed(path, "src/checkout.js", 0)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			res := runGate(t, tests.NewTestImmutability(cfg), cfg, tc.file)

			assertRefusal(t, res, domain.RemediationRevertPaths)
			if want := "append-only · 1 removed"; res.Message != want {
				t.Errorf("message = %q, want %q: the list that forbids the removal speaks", res.Message, want)
			}
		})
	}

	// The edit rule is the other way round, and stays that way: a snapshot
	// under both lists is still only flagged.
	t.Run("AnEditStillAnswersToTheFixtureList", func(t *testing.T) {
		t.Parallel()

		res := runGate(t, tests.NewTestImmutability(cfg), cfg, modified(path, 3, "test('checks out', () => {})"))

		assertStatus(t, res, domain.StatusWarn)
	})
}
