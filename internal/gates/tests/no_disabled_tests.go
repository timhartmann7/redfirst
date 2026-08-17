package tests

import (
	"context"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// NoDisabledTests refuses an added line that marks a test skipped. S2
// implements it.
type NoDisabledTests struct{}

// NewNoDisabledTests builds the gate. The config arrives at construction rather
// than at Run: Requires() has to stay fixed for a whole run.
func NewNoDisabledTests(domain.Config) *NoDisabledTests { return &NoDisabledTests{} }

// ID names the gate in the config, the report and the JSON.
func (g *NoDisabledTests) ID() domain.GateID { return domain.GateNoDisabledTests }

// Requires declares what the gate needs before the runner lets it speak.
func (g *NoDisabledTests) Requires() []domain.Capability {
	return []domain.Capability{domain.CapDiff}
}

// Run reports the stub state. S2 replaces this file whole.
func (g *NoDisabledTests) Run(context.Context, domain.Input) (domain.GateResult, error) {
	return domain.GateResult{
		ID:     domain.GateNoDisabledTests,
		Status: domain.StatusSkip,
		Reason: "gate not implemented yet",
	}, nil
}
