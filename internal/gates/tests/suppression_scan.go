package tests

import (
	"context"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// SuppressionScan flags added lines that silence a symptom. It fills the
// reviewer-attention block and leaves the verdict alone. S2 implements it.
type SuppressionScan struct{}

// NewSuppressionScan builds the gate. The config arrives at construction rather
// than at Run: Requires() has to stay fixed for a whole run.
func NewSuppressionScan(domain.Config) *SuppressionScan { return &SuppressionScan{} }

// ID names the gate in the config, the report and the JSON.
func (g *SuppressionScan) ID() domain.GateID { return domain.GateSuppressionScan }

// Requires declares what the gate needs before the runner lets it speak.
func (g *SuppressionScan) Requires() []domain.Capability {
	return []domain.Capability{domain.CapDiff}
}

// Run reports the stub state. S2 replaces this file whole.
func (g *SuppressionScan) Run(context.Context, domain.Input) (domain.GateResult, error) {
	return domain.GateResult{
		ID:     domain.GateSuppressionScan,
		Status: domain.StatusSkip,
		Reason: "gate not implemented yet",
	}, nil
}
