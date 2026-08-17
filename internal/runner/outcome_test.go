package runner_test

import (
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/runner"
)

func TestExitCode_HumanRequiredIsFiveNotOne(t *testing.T) {
	t.Parallel()

	legalMove := domain.GateResult{
		ID: domain.GateProtectedPaths, Status: domain.StatusFail, Remediation: domain.RemediationRevertPaths,
	}
	noLegalMove := domain.GateResult{
		ID: domain.GateRedGreen, Status: domain.StatusFail, Remediation: domain.RemediationHumanRequired,
	}

	cases := []struct {
		name    string
		results []domain.GateResult
		want    int
	}{
		{"HumanRequiredOutranksAnotherRefusal", []domain.GateResult{legalMove, noLegalMove}, 5},
		{"PositionInTheReportDoesNotMatter", []domain.GateResult{noLegalMove, legalMove}, 5},
		{"WithoutItARefusalIsOne", []domain.GateResult{legalMove}, 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := domain.ExitCode(runner.Outcome(tc.results)); got != tc.want {
				t.Errorf("exit code = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestOutcome_OnlyAFailChangesTheVerdict(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		results []domain.GateResult
		want    int
	}{
		{name: "NoGates", want: 0},
		{
			name: "PassSkipAndUnavailableAcquit",
			results: []domain.GateResult{
				{ID: domain.GateProtectedPaths, Status: domain.StatusPass},
				{ID: domain.GateDiffBudget, Status: domain.StatusSkip, Reason: "disabled in config"},
				{ID: domain.GateSuiteGreen, Status: domain.StatusUnavailable, Reason: "no hooks"},
			},
			want: 0,
		},
		{
			name:    "WarnLeavesTheVerdictAlone",
			results: []domain.GateResult{{ID: domain.GateDiffBudget, Status: domain.StatusWarn}},
			want:    0,
		},
		{
			name: "OneFailAmongPassesRefuses",
			results: []domain.GateResult{
				{ID: domain.GateProtectedPaths, Status: domain.StatusPass},
				{
					ID:          domain.GateNewTestPresent,
					Status:      domain.StatusFail,
					Remediation: domain.RemediationAddTest,
				},
			},
			want: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := domain.ExitCode(runner.Outcome(tc.results)); got != tc.want {
				t.Errorf("exit code = %d, want %d", got, tc.want)
			}
		})
	}
}
