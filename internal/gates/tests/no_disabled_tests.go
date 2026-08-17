package tests

import (
	"context"
	"fmt"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// NoDisabledTests refuses an added line that marks a test skipped. It closes
// the trick of writing a test and disabling it in the same breath.
//
// It reads every file under tests.patterns rather than the new ones alone:
// under append-only and cases the remaining gates permit appending `.skip(` to
// a file that has been in the repository for a year.
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

// Run matches tests.forbid_disabled against the added lines of every test file.
// The head path decides membership: a file renamed into the test tree carries
// its new lines under the rules of where it landed.
func (g *NoDisabledTests) Run(_ context.Context, in domain.Input) (domain.GateResult, error) {
	hits := scanAdded(in.Diff, in.Config.Tests.Patterns.Match, in.Config.Tests.ForbidDisabled)
	if len(hits) == 0 {
		return domain.GateResult{Status: domain.StatusPass}, nil
	}

	res := domain.GateResult{
		Status: domain.StatusFail,
		Message: fmt.Sprintf("%d added %s disabling a test",
			len(hits), plural(len(hits), "line")),
		Remediation: domain.RemediationRevertPaths,
	}
	for _, h := range hits {
		res.Evidence = append(res.Evidence, domain.Evidence{
			File: h.file, Line: h.line, Detail: excerpt(h.text),
		})
	}
	return res, nil
}
