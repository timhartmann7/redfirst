package paths

import (
	"context"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// DiffBudget keeps the number of changed files and lines inside [budget]. S1
// implements it.
type DiffBudget struct{}

// NewDiffBudget builds the gate. The config arrives at construction rather than
// at Run: Requires() has to stay fixed for a whole run.
func NewDiffBudget(domain.Config) *DiffBudget { return &DiffBudget{} }

// ID names the gate in the config, the report and the JSON.
func (g *DiffBudget) ID() domain.GateID { return domain.GateDiffBudget }

// Requires declares what the gate needs before the runner lets it speak.
func (g *DiffBudget) Requires() []domain.Capability {
	return []domain.Capability{domain.CapDiff}
}

// Run reports the stub state. S1 replaces this file whole.
func (g *DiffBudget) Run(context.Context, domain.Input) (domain.GateResult, error) {
	return domain.GateResult{
		ID:     domain.GateDiffBudget,
		Status: domain.StatusSkip,
		Reason: "gate not implemented yet",
	}, nil
}
