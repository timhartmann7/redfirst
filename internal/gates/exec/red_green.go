package exec

import (
	"context"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// RedGreen probes the added test cases on base and on head: they have to be red
// without the fix and green with it. S6 implements it.
type RedGreen struct{}

// NewRedGreen builds the gate. The config arrives at construction rather than
// at Run: Requires() has to stay fixed for a whole run.
func NewRedGreen(domain.Config) *RedGreen { return &RedGreen{} }

// ID names the gate in the config, the report and the JSON.
func (g *RedGreen) ID() domain.GateID { return domain.GateRedGreen }

// Requires declares what the gate needs before the runner lets it speak. The
// gate judges cases, so the diff has to hold test files and the harness has to
// name the cases it ran.
func (g *RedGreen) Requires() []domain.Capability {
	return []domain.Capability{
		domain.CapDiff,
		domain.CapHooks,
		domain.CapTestFiles,
		domain.CapCaseNames,
	}
}

// Run reports the stub state. S6 replaces this file whole.
func (g *RedGreen) Run(context.Context, domain.Input) (domain.GateResult, error) {
	return domain.GateResult{
		ID:     domain.GateRedGreen,
		Status: domain.StatusSkip,
		Reason: "gate not implemented yet",
	}, nil
}
