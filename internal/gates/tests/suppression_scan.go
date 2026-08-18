package tests

import (
	"context"
	"fmt"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// SuppressionScan flags added lines that silence a symptom. An empty catch or a
// @ts-ignore inside an autofix almost always marks a problem talked out of the
// output rather than fixed, and that is a judgement for a human: the gate fills
// the reviewer block and leaves the verdict where it found it.
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

// Run searches every added line in the diff, source and test alike: a symptom
// gets silenced wherever it was loud.
func (g *SuppressionScan) Run(_ context.Context, in domain.Input) (domain.GateResult, error) {
	suppression := in.Config.Suppression
	if !suppression.Enabled {
		return domain.GateResult{
			Status: domain.StatusSkip,
			Reason: "suppression.enabled is false",
		}, nil
	}

	hits := scanAdded(in.Diff, everyPath, suppression.Patterns)
	if len(hits) == 0 {
		return domain.GateResult{Status: domain.StatusPass}, nil
	}

	res := domain.GateResult{
		Status:  domain.StatusWarn,
		Message: fmt.Sprintf("%d added %s to review", len(hits), plural(len(hits), "line")),
	}
	for _, h := range hits {
		// The line itself, not a name for it: a reviewer decides whether this
		// particular @ts-ignore is fine, and the pattern that caught it says
		// nothing about the code it caught.
		res.Warnings = append(res.Warnings, domain.Warning{
			Kind:    domain.WarnSuppression,
			File:    h.file,
			Line:    h.line,
			Pattern: h.pattern,
			Detail:  excerpt(h.text),
		})
	}
	return res, nil
}

func everyPath(string) bool { return true }
