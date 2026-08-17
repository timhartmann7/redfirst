package runner_test

import (
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/runner"
)

func TestRunner_WarnSeverityTurnsFailIntoWarnAndClearsRemediation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name            string
		severity        domain.Severity
		wantStatus      domain.GateStatus
		wantRemediation domain.Remediation
	}{
		{"WarnDowngradesAndDropsTheRemediation", domain.SeverityWarn, domain.StatusWarn, ""},
		{"FailKeepsTheRefusalAndItsRemediation", domain.SeverityFail, domain.StatusFail, domain.RemediationReduceScope},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gate := staticGate(domain.GateDiffBudget, failure(domain.RemediationReduceScope))
			opts := runner.Options{
				Config: domain.Config{Severity: map[domain.GateID]domain.Severity{
					domain.GateDiffBudget: tc.severity,
				}},
				Caps: runner.NewCapSet(domain.CapDiff),
			}

			got := run(t, []runner.Entry{entry(gate)}, opts)[0]

			if got.Status != tc.wantStatus {
				t.Errorf("status = %q, want %q", got.Status, tc.wantStatus)
			}
			if got.Remediation != tc.wantRemediation {
				t.Errorf("remediation = %q, want %q", got.Remediation, tc.wantRemediation)
			}
			if got.Message != "refused" {
				t.Errorf("message = %q, want the gate's own message", got.Message)
			}
		})
	}
}

// TestRunner_WarnSeverityCannotEraseHumanRequired guards the one downgrade that
// would produce a silent acquittal: no diff fixes a human-required refusal, so
// turning it into a report line would hand back exit code 0 for a run with no
// legal move.
func TestRunner_WarnSeverityCannotEraseHumanRequired(t *testing.T) {
	t.Parallel()

	gate := staticGate(domain.GateDiffBudget, failure(domain.RemediationHumanRequired))
	opts := runner.Options{
		Config: domain.Config{Severity: map[domain.GateID]domain.Severity{
			domain.GateDiffBudget: domain.SeverityWarn,
		}},
		Caps: runner.NewCapSet(domain.CapDiff),
	}

	results := run(t, []runner.Entry{entry(gate)}, opts)

	if got := results[0].Status; got != domain.StatusFail {
		t.Errorf("status = %q, want fail: warn may not soften a refusal with no legal move", got)
	}
	if got := results[0].Remediation; got != domain.RemediationHumanRequired {
		t.Errorf("remediation = %q, want human-required", got)
	}
	if got := domain.ExitCode(runner.Outcome(results)); got != 5 {
		t.Errorf("exit code = %d, want 5", got)
	}
}
