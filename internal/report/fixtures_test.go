package report_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/report"
)

// tier1Report is the tier 1 sample from section 9 of the spec: static gates
// only, the two hook gates unavailable with their hint.
func tier1Report() domain.Report {
	return domain.Report{
		Schema:          domain.SchemaVersion,
		RedfirstVersion: "0.4.1",
		Tier:            1,
		ConfigSource:    "defaults",
		Base:            "f3c9a11",
		Head:            "8b2e740",
		Verdict:         domain.VerdictFail,
		ExitCode:        1,
		Diff:            domain.Stats{Files: 4, Added: 87, Deleted: 12},
		Gates: []domain.GateResult{
			{ID: domain.GateProtectedPaths, Status: domain.StatusPass},
			{
				ID:      domain.GateDiffBudget,
				Status:  domain.StatusWarn,
				Message: "24/20 files, +910/800, -12/400",
			},
			{
				ID:          domain.GateTestImmutability,
				Status:      domain.StatusFail,
				Message:     "append-only · src/lib/order/total.test.ts was deleted",
				Remediation: domain.RemediationRevertPaths,
				Evidence: []domain.Evidence{{
					File:   "src/lib/order/total.test.ts",
					Detail: "31 lines removed, append-only allows none",
				}},
			},
			{ID: domain.GateNewTestPresent, Status: domain.StatusSkip, Reason: "disabled by default"},
			{ID: domain.GateNoDisabledTests, Status: domain.StatusPass},
			{
				ID:     domain.GateSuiteGreen,
				Status: domain.StatusUnavailable,
				Reason: "no hooks",
				Hint:   "add .redfirst/ to enable",
			},
			{
				ID:     domain.GateRedGreen,
				Status: domain.StatusUnavailable,
				Reason: "no hooks",
				Hint:   "add .redfirst/ to enable",
			},
		},
		Timings: domain.Timings{TotalS: 0.42},
	}
}

// tier2Report is the tier 2 sample from section 9: hooks present, red-green
// refusing on a base probe that disagrees with itself.
func tier2Report() domain.Report {
	return domain.Report{
		Schema:          domain.SchemaVersion,
		RedfirstVersion: "0.4.1",
		Tier:            2,
		ConfigSource:    "base:redfirst.toml",
		Base:            "f3c9a11",
		Head:            "8b2e740",
		EnvMode:         "reused",
		ProbeRuns:       3,
		Verdict:         domain.VerdictFail,
		ExitCode:        1,
		Diff:            domain.Stats{Files: 4, Added: 87, Deleted: 12},
		Tests:           domain.Stats{Files: 1, Added: 31},
		Gates: []domain.GateResult{
			{ID: domain.GateProtectedPaths, Status: domain.StatusPass},
			{ID: domain.GateDiffBudget, Status: domain.StatusPass, Message: "4/20 files, +87/800, -12/400"},
			{
				ID:      domain.GateTestImmutability,
				Status:  domain.StatusPass,
				Message: "cases · 0 removed, 0 regressed",
			},
			{
				ID:      domain.GateNewTestPresent,
				Status:  domain.StatusPass,
				Message: "src/lib/order/total.test.ts",
			},
			{ID: domain.GateNoDisabledTests, Status: domain.StatusPass},
			{
				ID:      domain.GateSuiteGreen,
				Status:  domain.StatusPass,
				Message: "412 passed, 1 flaky (untouched)",
			},
			{
				ID:          domain.GateRedGreen,
				Status:      domain.StatusFail,
				Message:     "flaky test, base probe is not deterministic",
				Remediation: domain.RemediationFixTest,
				Evidence: []domain.Evidence{{
					File:     "src/lib/order/total.test.ts",
					Case:     "promo item does not break the total",
					BaseRuns: []string{"fail", "pass", "fail"},
				}},
			},
		},
		Warnings: []domain.Warning{
			{
				Kind:   domain.WarnFlaky,
				File:   "tests/integration/webhook.spec.ts",
				Detail: "passed on retry, not touched by diff",
			},
			{Kind: domain.WarnGuardedPaths, File: "pnpm-lock.yaml"},
			{
				Kind:    domain.WarnSuppression,
				File:    "src/lib/order/total.ts",
				Line:    42,
				Pattern: "empty catch",
				Detail:  "empty catch block added",
			},
		},
		Timings: domain.Timings{TotalS: 214},
	}
}

// passReport is the tier 1 result the spec calls valid: unavailable gates in
// the report and exit code 0.
func passReport() domain.Report {
	return domain.Report{
		Schema:          domain.SchemaVersion,
		RedfirstVersion: "0.4.1",
		Tier:            1,
		ConfigSource:    "base:redfirst.toml",
		Base:            "f3c9a11",
		Head:            "8b2e740",
		Verdict:         domain.VerdictPass,
		ExitCode:        0,
		Diff:            domain.Stats{Files: 2, Added: 14, Deleted: 3},
		Tests:           domain.Stats{Files: 1, Added: 9},
		Gates: []domain.GateResult{
			{ID: domain.GateProtectedPaths, Status: domain.StatusPass},
			{ID: domain.GateDiffBudget, Status: domain.StatusPass, Message: "2/20 files, +14/800, -3/400"},
			{ID: domain.GateTestImmutability, Status: domain.StatusPass, Message: "append-only · 0 removed"},
			{ID: domain.GateNewTestPresent, Status: domain.StatusPass, Message: "src/lib/order/total.test.ts"},
			{ID: domain.GateNoDisabledTests, Status: domain.StatusPass},
			{ID: domain.GateSuppressionScan, Status: domain.StatusPass},
			{
				ID:     domain.GateSuiteGreen,
				Status: domain.StatusUnavailable,
				Reason: "no hooks",
				Hint:   "add .redfirst/ to enable",
			},
			{
				ID:     domain.GateRedGreen,
				Status: domain.StatusUnavailable,
				Reason: "no hooks",
				Hint:   "add .redfirst/ to enable",
			},
			{
				ID:     domain.GateBlastRadius,
				Status: domain.StatusUnavailable,
				Reason: "no stack trace provided",
			},
		},
		Timings: domain.Timings{TotalS: 0.19},
	}
}

// goldenCase pairs a golden file name with the report it renders from.
type goldenCase struct {
	name   string
	report domain.Report
}

func goldenCases() []goldenCase {
	return []goldenCase{
		{"tier1", tier1Report()},
		{"tier2", tier2Report()},
		{"pass", passReport()},
	}
}

func renderText(t *testing.T, r domain.Report) string {
	t.Helper()
	var buf bytes.Buffer
	if err := report.Text(&buf, r); err != nil {
		t.Fatalf("Text: %v", err)
	}
	return buf.String()
}

// gateLine renders one gate on its own and returns its line, skipping the two
// header lines and the blank line the unavailable block puts in front.
func gateLine(t *testing.T, g domain.GateResult) string {
	t.Helper()
	out := renderText(t, domain.Report{Gates: []domain.GateResult{g}})
	for _, l := range strings.Split(out, "\n")[2:] {
		if l != "" {
			return l
		}
	}
	t.Fatalf("no gate line in\n%s", out)
	return ""
}

func firstLine(s string) string { return nthLine(s, 0) }

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}

func nthLine(s string, n int) string {
	lines := strings.Split(s, "\n")
	if n >= len(lines) {
		return ""
	}
	return lines[n]
}
