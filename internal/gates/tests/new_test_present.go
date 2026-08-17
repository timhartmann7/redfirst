package tests

import (
	"context"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// NewTestPresent demands a new test file or added lines in an existing one. S2
// implements it.
type NewTestPresent struct{}

// NewNewTestPresent builds the gate. The config arrives at construction rather
// than at Run: Requires() has to stay fixed for a whole run.
func NewNewTestPresent(domain.Config) *NewTestPresent { return &NewTestPresent{} }

// ID names the gate in the config, the report and the JSON.
func (g *NewTestPresent) ID() domain.GateID { return domain.GateNewTestPresent }

// Requires declares what the gate needs before the runner lets it speak.
func (g *NewTestPresent) Requires() []domain.Capability {
	return []domain.Capability{domain.CapDiff}
}

// Run reports the stub state. S2 replaces this file whole.
func (g *NewTestPresent) Run(context.Context, domain.Input) (domain.GateResult, error) {
	return domain.GateResult{
		ID:     domain.GateNewTestPresent,
		Status: domain.StatusSkip,
		Reason: "gate not implemented yet",
	}, nil
}
