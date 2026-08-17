package paths_test

import (
	"context"
	"slices"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/gates/paths"
)

func TestDiffBudgetStub_ReadsOnlyTheDiffAndSkipsUntilImplemented(t *testing.T) {
	t.Parallel()

	cfg := domain.Config{}
	g := paths.NewDiffBudget(cfg)

	if got := g.ID(); got != domain.GateDiffBudget {
		t.Errorf("ID = %q, want %q", got, domain.GateDiffBudget)
	}
	if got := g.Requires(); !slices.Equal(got, []domain.Capability{domain.CapDiff}) {
		t.Errorf("Requires = %v, want the diff alone", got)
	}

	res, err := g.Run(context.Background(), domain.Input{Config: cfg})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ID != domain.GateDiffBudget || res.Status != domain.StatusSkip {
		t.Errorf("got %q / %q, want %q / skip", res.ID, res.Status, domain.GateDiffBudget)
	}
	if res.Reason != "gate not implemented yet" {
		t.Errorf("reason = %q, want the stub reason", res.Reason)
	}
}
