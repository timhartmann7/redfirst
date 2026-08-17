package tests_test

import (
	"context"
	"slices"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/gates/tests"
)

// gate is the shape the runner consumes. The stub tests speak through it so a
// slice replacing a stub keeps answering the same three questions.
type gate interface {
	ID() domain.GateID
	Requires() []domain.Capability
	Run(ctx context.Context, in domain.Input) (domain.GateResult, error)
}

func TestTestsStubs_DeclareTheirIDsAndSkipUntilImplemented(t *testing.T) {
	t.Parallel()

	cfg := domain.Config{}
	cases := []struct {
		name string
		gate gate
		want domain.GateID
	}{
		{"TestImmutability", tests.NewTestImmutability(cfg), domain.GateTestImmutability},
		{"NewTestPresent", tests.NewNewTestPresent(cfg), domain.GateNewTestPresent},
		{"NoDisabledTests", tests.NewNoDisabledTests(cfg), domain.GateNoDisabledTests},
		{"SuppressionScan", tests.NewSuppressionScan(cfg), domain.GateSuppressionScan},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.gate.ID(); got != tc.want {
				t.Errorf("ID = %q, want %q", got, tc.want)
			}

			res, err := tc.gate.Run(context.Background(), domain.Input{Config: cfg})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if res.ID != tc.want || res.Status != domain.StatusSkip {
				t.Errorf("got %q / %q, want %q / skip", res.ID, res.Status, tc.want)
			}
			if res.Reason != "gate not implemented yet" {
				t.Errorf("reason = %q, want the stub reason", res.Reason)
			}
		})
	}
}

func TestTestsStubs_LineBasedGatesReadOnlyTheDiff(t *testing.T) {
	t.Parallel()

	cfg := domain.Config{}
	cases := []struct {
		name string
		gate gate
	}{
		{"NewTestPresent", tests.NewNewTestPresent(cfg)},
		{"NoDisabledTests", tests.NewNoDisabledTests(cfg)},
		{"SuppressionScan", tests.NewSuppressionScan(cfg)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.gate.Requires(); !slices.Equal(got, []domain.Capability{domain.CapDiff}) {
				t.Errorf("Requires = %v, want the diff alone", got)
			}
		})
	}
}

func TestTestImmutability_RequiresCaseNamesOnlyInCasesMode(t *testing.T) {
	t.Parallel()

	// The mode is fixed at construction: a gate may never look at its own
	// inputs during Run to decide what it needed.
	cases := []struct {
		name string
		mode domain.Immutability
		want []domain.Capability
	}{
		{"CasesModeJudgesOutcomesAndNeedsTheNames", domain.ImmutabilityCases, []domain.Capability{
			domain.CapDiff, domain.CapCaseNames,
		}},
		{"StrictReadsLines", domain.ImmutabilityStrict, []domain.Capability{domain.CapDiff}},
		{"AppendOnlyReadsLines", domain.ImmutabilityAppendOnly, []domain.Capability{domain.CapDiff}},
		{"UnsetModeReadsLines", "", []domain.Capability{domain.CapDiff}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := domain.Config{Tests: domain.Tests{Immutability: tc.mode}}
			g := tests.NewTestImmutability(cfg)

			if got := g.Requires(); !slices.Equal(got, tc.want) {
				t.Errorf("Requires = %v, want %v", got, tc.want)
			}
		})
	}
}
