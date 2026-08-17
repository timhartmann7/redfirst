package paths_test

import (
	"context"
	"slices"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/gates/paths"
)

// gate is the shape the runner consumes. The stub tests speak through it so a
// slice replacing a stub keeps answering the same three questions.
type gate interface {
	ID() domain.GateID
	Requires() []domain.Capability
	Run(ctx context.Context, in domain.Input) (domain.GateResult, error)
}

func TestPathsStubs_ReadOnlyTheDiffAndSkipUntilImplemented(t *testing.T) {
	t.Parallel()

	cfg := domain.Config{}
	cases := []struct {
		name string
		gate gate
		want domain.GateID
	}{
		{"ProtectedPaths", paths.NewProtectedPaths(cfg), domain.GateProtectedPaths},
		{"DiffBudget", paths.NewDiffBudget(cfg), domain.GateDiffBudget},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.gate.ID(); got != tc.want {
				t.Errorf("ID = %q, want %q", got, tc.want)
			}
			if got := tc.gate.Requires(); !slices.Equal(got, []domain.Capability{domain.CapDiff}) {
				t.Errorf("Requires = %v, want the diff alone", got)
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
