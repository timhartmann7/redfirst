package runner

import (
	"context"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// RunEntries runs an explicit gate list. Run itself takes the registry, which
// leaves the external tests no seam for driving the order, the short-circuit
// and the capability checks with fake gates instead of the stubs.
func RunEntries(
	ctx context.Context, entries []Entry, diff domain.Diff, opts Options,
) ([]domain.GateResult, error) {
	return runEntries(ctx, entries, diff, opts)
}
