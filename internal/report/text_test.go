package report_test

import (
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

func TestReport_TextMatchesGoldenPerTier(t *testing.T) {
	t.Parallel()
	for _, tc := range goldenCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			testkit.Golden(t, tc.name+".txt", []byte(renderText(t, tc.report)))
		})
	}
}

func TestReport_TextUnavailableLineCarriesReasonAndHint(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		gate   domain.GateResult
		want   string
		absent string
	}{
		{
			name: "ReasonAndHint",
			gate: domain.GateResult{
				ID:     domain.GateRedGreen,
				Status: domain.StatusUnavailable,
				Reason: "no hooks",
				Hint:   "add .redfirst/ to enable",
			},
			want: "N/A   red-green              no hooks, add .redfirst/ to enable",
		},
		{
			name: "ReasonOnlyWhenNoHint",
			gate: domain.GateResult{
				ID:     domain.GateBlastRadius,
				Status: domain.StatusUnavailable,
				Reason: "no stack trace provided",
			},
			want:   "N/A   blast-radius           no stack trace provided",
			absent: ",",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := gateLine(t, tc.gate)
			if got != tc.want {
				t.Fatalf("unavailable line\n got: %q\nwant: %q", got, tc.want)
			}
			if tc.absent != "" && strings.Contains(got, tc.absent) {
				t.Errorf("line %q must not contain %q", got, tc.absent)
			}
		})
	}
}

func TestReport_TextVerdictCountsOnlyFailedGates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		statuses []domain.GateStatus
		verdict  domain.Verdict
		exit     int
		want     string
	}{
		{
			name:     "OneFailReadsSingular",
			statuses: []domain.GateStatus{domain.StatusPass, domain.StatusFail, domain.StatusWarn},
			verdict:  domain.VerdictFail,
			exit:     1,
			want:     "VERDICT: FAIL (1 gate)  exit=1",
		},
		{
			name: "WarnSkipAndUnavailableDoNotCount",
			statuses: []domain.GateStatus{
				domain.StatusFail, domain.StatusFail, domain.StatusWarn,
				domain.StatusSkip, domain.StatusUnavailable,
			},
			verdict: domain.VerdictFail,
			exit:    1,
			want:    "VERDICT: FAIL (2 gates)  exit=1",
		},
		{
			name:     "HumanRequiredKeepsExitFive",
			statuses: []domain.GateStatus{domain.StatusFail},
			verdict:  domain.VerdictFail,
			exit:     5,
			want:     "VERDICT: FAIL (1 gate)  exit=5",
		},
		{
			name:     "PassOmitsTheCount",
			statuses: []domain.GateStatus{domain.StatusPass, domain.StatusUnavailable},
			verdict:  domain.VerdictPass,
			exit:     0,
			want:     "VERDICT: PASS  exit=0",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := domain.Report{Verdict: tc.verdict, ExitCode: tc.exit}
			for _, s := range tc.statuses {
				r.Gates = append(r.Gates, domain.GateResult{ID: domain.GateDiffBudget, Status: s})
			}
			if got := lastLine(renderText(t, r)); got != tc.want {
				t.Fatalf("verdict line\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

func TestReport_TextFailureLineSaysWhatIsWrong(t *testing.T) {
	t.Parallel()
	got := renderText(t, tier2Report())
	want := []string{
		"FAIL  red-green              flaky test, base probe is not deterministic",
		`      case "promo item does not break the total"  base runs: fail, pass, fail`,
		"      remediation: fix-test",
	}
	for _, line := range want {
		if !strings.Contains(got, line+"\n") {
			t.Errorf("report is missing line %q\n---\n%s", line, got)
		}
	}
	// The evidence names the case, not the file: a file with twenty cases tells
	// a reviewer nothing.
	for _, l := range strings.Split(got, "\n") {
		if strings.HasPrefix(l, "      src/lib/order/total.test.ts") {
			t.Errorf("red-green evidence names the file instead of the case: %q", l)
		}
	}
}

func TestReport_TextAlignsMessageAtColumn29(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		gate domain.GateResult
	}{
		{"ShortStatusLabel", domain.GateResult{
			ID: domain.GateRedGreen, Status: domain.StatusUnavailable, Reason: "no hooks",
		}},
		{"LongestGateID", domain.GateResult{
			ID: domain.GateTestImmutability, Status: domain.StatusPass, Message: "cases · 0 removed",
		}},
		{"ShortestGateID", domain.GateResult{
			ID: domain.GateRedGreen, Status: domain.StatusFail, Message: "test is green on base",
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			line := gateLine(t, tc.gate)
			msg := tc.gate.Message
			if msg == "" {
				msg = tc.gate.Reason
			}
			if idx := strings.Index(line, msg); idx != 29 {
				t.Fatalf("message starts at column %d, want 29: %q", idx, line)
			}
		})
	}
}

func TestReport_TextOmitsEmptyBlocks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		report domain.Report
		want   string
	}{
		{
			name: "NoGatesNoWarnings",
			report: domain.Report{
				RedfirstVersion: "0.1.0", Tier: 1, ConfigSource: "defaults",
				Base: "f3c9a11", Head: "8b2e740", Verdict: domain.VerdictPass,
			},
			want: "redfirst v0.1.0 · verify · tier=1 · config=defaults\n" +
				"base f3c9a11  head 8b2e740  diff 0 files (+0 -0)\n" +
				"\n" +
				"VERDICT: PASS  exit=0\n",
		},
		{
			name: "PassingGateHasNoTrailingPadding",
			report: domain.Report{
				RedfirstVersion: "0.1.0", Tier: 1, ConfigSource: "defaults",
				Base: "f3c9a11", Head: "8b2e740", Verdict: domain.VerdictPass,
				Gates: []domain.GateResult{{ID: domain.GateProtectedPaths, Status: domain.StatusPass}},
			},
			want: "redfirst v0.1.0 · verify · tier=1 · config=defaults\n" +
				"base f3c9a11  head 8b2e740  diff 0 files (+0 -0)\n" +
				"\n" +
				"PASS  protected-paths\n" +
				"\n" +
				"VERDICT: PASS  exit=0\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := renderText(t, tc.report); got != tc.want {
				t.Fatalf("text report\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

func TestReport_TextSuppressionsGoToReviewNeededBlock(t *testing.T) {
	t.Parallel()
	got := renderText(t, tier2Report())
	if !strings.Contains(got, "\nREVIEW NEEDED\n  src/lib/order/total.ts:42   empty catch block added\n") {
		t.Errorf("suppression block missing or malformed\n---\n%s", got)
	}
	if strings.Contains(got, "WARN  suppression") {
		t.Error("a suppression warning must not also appear as a WARN line")
	}
	if !strings.Contains(got, "WARN  guarded-paths          pnpm-lock.yaml\n") {
		t.Errorf("guarded-paths warning missing\n---\n%s", got)
	}
}

func TestReport_TextHeaderCarriesEnvModeOnlyWithOne(t *testing.T) {
	t.Parallel()
	tier1 := firstLine(renderText(t, tier1Report()))
	if tier1 != "redfirst v0.4.1 · verify · tier=1 · config=defaults" {
		t.Errorf("tier 1 header: %q", tier1)
	}
	tier2 := firstLine(renderText(t, tier2Report()))
	want := "redfirst v0.4.1 · verify · tier=2 · config=base:redfirst.toml · env_mode=reused · probe_runs=3"
	if tier2 != want {
		t.Errorf("tier 2 header\n got: %q\nwant: %q", tier2, want)
	}
}

func TestReport_TextStatsLineCountsTestFilesOnlyWhenPresent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		tests domain.Stats
		want  string
	}{
		{"NoTestFilesInDiff", domain.Stats{}, "base f3c9a11  head 8b2e740  diff 4 files (+87 -12)"},
		{
			"OneTestFileReadsSingular",
			domain.Stats{Files: 1, Added: 31},
			"base f3c9a11  head 8b2e740  diff 4 files (+87 -12), tests 1 file (+31)",
		},
		{
			"TwoTestFilesReadPlural",
			domain.Stats{Files: 2, Added: 40},
			"base f3c9a11  head 8b2e740  diff 4 files (+87 -12), tests 2 files (+40)",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := domain.Report{
				Base:    "f3c9a11ffffffffffffffffffffffffffffffff",
				Head:    "8b2e740aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Verdict: domain.VerdictPass,
				Diff:    domain.Stats{Files: 4, Added: 87, Deleted: 12},
			}
			r.Tests = tc.tests
			if got := nthLine(renderText(t, r), 1); got != tc.want {
				t.Fatalf("stats line\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}
