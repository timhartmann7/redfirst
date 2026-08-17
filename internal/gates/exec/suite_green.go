// Package exec holds the gates that run someone else's test harness.
package exec

import (
	"context"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// SuiteGreen runs the full unfiltered suite on head and classifies what failed
// by origin. S6 implements it.
type SuiteGreen struct{}

// NewSuiteGreen builds the gate. The config arrives at construction rather than
// at Run: Requires() has to stay fixed for a whole run.
func NewSuiteGreen(domain.Config) *SuiteGreen { return &SuiteGreen{} }

// ID names the gate in the config, the report and the JSON.
func (g *SuiteGreen) ID() domain.GateID { return domain.GateSuiteGreen }

// Requires declares what the gate needs before the runner lets it speak.
func (g *SuiteGreen) Requires() []domain.Capability {
	return []domain.Capability{domain.CapDiff, domain.CapHooks}
}

// Run reports the stub state. S6 replaces this file whole.
func (g *SuiteGreen) Run(context.Context, domain.Input) (domain.GateResult, error) {
	return domain.GateResult{
		ID:     domain.GateSuiteGreen,
		Status: domain.StatusSkip,
		Reason: "gate not implemented yet",
	}, nil
}
