// Package tests holds the gates that judge what the diff did to the tests.
package tests

import (
	"context"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// TestImmutability answers whether the agent removed existing insurance or bent
// the test surface toward its own result. S2 implements it.
type TestImmutability struct {
	mode domain.Immutability
}

// NewTestImmutability builds the gate. The mode is read here, at construction,
// because Requires() depends on it and has to stay fixed for a whole run.
func NewTestImmutability(cfg domain.Config) *TestImmutability {
	return &TestImmutability{mode: cfg.Tests.Immutability}
}

// ID names the gate in the config, the report and the JSON.
func (g *TestImmutability) ID() domain.GateID { return domain.GateTestImmutability }

// Requires declares what the gate needs before the runner lets it speak. Mode
// cases judges outcomes rather than lines, so it needs the case names from the
// inventory run; the line-based modes read the diff alone.
func (g *TestImmutability) Requires() []domain.Capability {
	if g.mode == domain.ImmutabilityCases {
		return []domain.Capability{domain.CapDiff, domain.CapCaseNames}
	}
	return []domain.Capability{domain.CapDiff}
}

// Run reports the stub state. S2 replaces this file whole.
func (g *TestImmutability) Run(context.Context, domain.Input) (domain.GateResult, error) {
	return domain.GateResult{
		ID:     domain.GateTestImmutability,
		Status: domain.StatusSkip,
		Reason: "gate not implemented yet",
	}, nil
}
