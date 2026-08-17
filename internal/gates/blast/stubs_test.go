package blast_test

import (
	"context"
	"slices"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/gates/blast"
)

func TestBlastRadius_RequiresAStackTraceAndSkipsUntilImplemented(t *testing.T) {
	t.Parallel()

	cfg := domain.Config{}
	g := blast.NewBlastRadius(cfg)

	if got := g.ID(); got != domain.GateBlastRadius {
		t.Errorf("ID = %q, want %q", got, domain.GateBlastRadius)
	}

	want := []domain.Capability{domain.CapDiff, domain.CapStackTrace}
	if got := g.Requires(); !slices.Equal(got, want) {
		t.Errorf("Requires = %v, want %v", got, want)
	}

	res, err := g.Run(context.Background(), domain.Input{Config: cfg})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ID != domain.GateBlastRadius || res.Status != domain.StatusSkip {
		t.Errorf("got %q / %q, want %q / skip", res.ID, res.Status, domain.GateBlastRadius)
	}
	if res.Reason != "gate not implemented yet" {
		t.Errorf("reason = %q, want the stub reason", res.Reason)
	}
}
