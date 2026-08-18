package tests

import (
	"context"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// NewTestPresent demands that the diff add at least one line to a file under
// tests.patterns.
//
// The unit is the line rather than the file, by design. A Go regression test
// goes into the existing total_test.go and a pytest one into the existing
// test_order.py, so demanding a new file would mean one test file per fix and
// would break the convention of three languages out of the five presets.
//
// Off in the defaults. On tier 1 the gate faces human PRs, where a refactor or
// a config edit is a legitimate diff with no new test, and a gate that fires on
// an ordinary diff gets the tool deleted rather than the diff fixed.
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

// Run looks for the fact of an addition and nothing more. red-green does the
// substantive check, by case rather than by file.
//
// An added file with no added line is an empty file, and an empty file tests
// nothing: the gate asks for a line either way, in a new test file or in one
// that has been there all along. A line added to the test surface is not a
// test: a snapshot holds no case, and accepting one would let a diff satisfy
// the gate by writing down the output it already produces.
func (g *NewTestPresent) Run(_ context.Context, in domain.Input) (domain.GateResult, error) {
	if !in.Config.Tests.RequireNew {
		return domain.GateResult{
			Status: domain.StatusSkip,
			Reason: "tests.require_new is false",
		}, nil
	}

	for _, f := range in.Diff.Files {
		if f.Added > 0 && holdsTestCases(in.Config, f.Path) {
			return domain.GateResult{Status: domain.StatusPass, Message: f.Path}, nil
		}
	}
	return domain.GateResult{
		Status:      domain.StatusFail,
		Message:     "the diff adds no line to a test file",
		Remediation: domain.RemediationAddTest,
	}, nil
}
