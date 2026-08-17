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
			domain.CapDiff, domain.CapCaseNames,
		}},
		{"TestSurfaceJudgesOutcomes", domain.ImmutabilityAppendOnly, domain.ImmutabilityCases, []domain.Capability{
			domain.CapDiff, domain.CapCaseNames,
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

// TestTestImmutability_ModeWithoutCaseNamesIsOurBugNotAPass holds the direction
// the gate may not fail in. Mode cases judges outcomes, the runner grants the
// capability that carries them, and a gate reaching Run without them has been
// handed something impossible: exit code 4 says so, where a pass would acquit a
// diff nobody checked.
func TestTestImmutability_ModeWithoutCaseNamesIsOurBugNotAPass(t *testing.T) {
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
		{"AModeNobodyWrote", "", "", removed("src/total.test.js", 9)},
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
