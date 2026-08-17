// Package runner turns the declared gates into a run: it computes the
// capability set, walks the registry in the mandatory order and maps the
// results onto the exit-code contract.
package runner

import (
	"context"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/gates/blast"
	"github.com/timhartmann7/redfirst/internal/gates/exec"
	"github.com/timhartmann7/redfirst/internal/gates/paths"
	"github.com/timhartmann7/redfirst/internal/gates/tests"
)

// Gate is declared at the consumer: the runner is the only caller.
type Gate interface {
	ID() domain.GateID
	Requires() []domain.Capability
	Run(ctx context.Context, in domain.Input) (domain.GateResult, error)
}

// Entry is one registry slot: an ID and the constructor for its gate. The
// constructor takes the config because Requires() may depend on it and has to
// stay fixed for a whole run.
type Entry struct {
	ID  domain.GateID
	New func(domain.Config) Gate
}

// Registry returns every declared gate in the mandatory execution order: the
// static gates first, so a refusal cuts the run off before any work with the
// environment, and red-green after suite-green, because probes buy nothing
// against a red suite on head. The slice is fresh on every call; nothing about
// a registered gate survives between runs.
func Registry() []Entry {
	return []Entry{
		{ID: domain.GateProtectedPaths, New: func(c domain.Config) Gate { return paths.NewProtectedPaths(c) }},
		{ID: domain.GateDiffBudget, New: func(c domain.Config) Gate { return paths.NewDiffBudget(c) }},
		{ID: domain.GateTestImmutability, New: func(c domain.Config) Gate { return tests.NewTestImmutability(c) }},
		{ID: domain.GateNewTestPresent, New: func(c domain.Config) Gate { return tests.NewNewTestPresent(c) }},
		{ID: domain.GateNoDisabledTests, New: func(c domain.Config) Gate { return tests.NewNoDisabledTests(c) }},
		{ID: domain.GateSuppressionScan, New: func(c domain.Config) Gate { return tests.NewSuppressionScan(c) }},
		{ID: domain.GateSuiteGreen, New: func(c domain.Config) Gate { return exec.NewSuiteGreen(c) }},
		{ID: domain.GateRedGreen, New: func(c domain.Config) Gate { return exec.NewRedGreen(c) }},
		{ID: domain.GateBlastRadius, New: func(c domain.Config) Gate { return blast.NewBlastRadius(c) }},
	}
}
