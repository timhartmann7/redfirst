// Package blast holds the gate that measures a diff against the stack trace of
// the original error.
package blast

import (
	"context"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// BlastRadius demands that every changed file be reachable from the stack trace
// of the original error. The interface is designed in v1 and the implementation
// deferred to v2, which needs a language-specific import resolver.
type BlastRadius struct{}

// NewBlastRadius builds the gate. The config arrives at construction rather
// than at Run: Requires() has to stay fixed for a whole run.
func NewBlastRadius(domain.Config) *BlastRadius { return &BlastRadius{} }

// ID names the gate in the config, the report and the JSON.
func (g *BlastRadius) ID() domain.GateID { return domain.GateBlastRadius }

// Requires declares what the gate needs before the runner lets it speak.
func (g *BlastRadius) Requires() []domain.Capability {
	return []domain.Capability{domain.CapDiff, domain.CapStackTrace}
}

// Run reports the stub state. The gate stays a stub through v1 so every report
// carries its line from day one.
func (g *BlastRadius) Run(context.Context, domain.Input) (domain.GateResult, error) {
	return domain.GateResult{
		ID:     domain.GateBlastRadius,
		Status: domain.StatusSkip,
		Reason: "gate not implemented yet",
	}, nil
}
