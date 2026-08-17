// Package paths holds the gates that judge which files a diff touched.
package paths

import (
	"context"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// ProtectedPaths refuses a diff that touches paths.protected, or that leaves a
// non-empty paths.allowed. S1 implements it.
type ProtectedPaths struct{}

// NewProtectedPaths builds the gate. The config arrives at construction rather
// than at Run: Requires() has to stay fixed for a whole run.
func NewProtectedPaths(domain.Config) *ProtectedPaths { return &ProtectedPaths{} }

// ID names the gate in the config, the report and the JSON.
func (g *ProtectedPaths) ID() domain.GateID { return domain.GateProtectedPaths }

// Requires declares what the gate needs before the runner lets it speak.
func (g *ProtectedPaths) Requires() []domain.Capability {
	return []domain.Capability{domain.CapDiff}
}

// Run reports the stub state. S1 replaces this file whole.
func (g *ProtectedPaths) Run(context.Context, domain.Input) (domain.GateResult, error) {
	return domain.GateResult{
		ID:     domain.GateProtectedPaths,
		Status: domain.StatusSkip,
		Reason: "gate not implemented yet",
	}, nil
}
