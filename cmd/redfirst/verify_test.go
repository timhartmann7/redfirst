package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/runner"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

// verifyArgs is the tier 1 invocation every test here starts from.
func verifyArgs(repoDir string, extra ...string) []string {
	args := []string{
		"verify",
		"--repo", repoDir,
		"--base", testkit.FixtureBase,
		"--head", testkit.FixtureHead,
		"--no-hooks",
	}
	return append(args, extra...)
}

// TestVerify_CleanFixWithoutHooksAccusesNobody holds the shape of a tier 1
// run: nine gate lines, none of them a refusal, the hook gates unavailable
// with a reason, exit 0.
func TestVerify_CleanFixWithoutHooksAccusesNobody(t *testing.T) {
	t.Parallel()

	repo := testkit.CleanFix(t)
	var out bytes.Buffer

	if err := run(context.Background(), verifyArgs(repo.Dir, "--report", "json"), &out, io.Discard); err != nil {
		t.Fatalf("verify: %v (exit %d)", err, domain.ExitCode(err))
	}

	var rep domain.Report
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatalf("parse report: %v\n%s", err, out.String())
	}
	if got, want := len(rep.Gates), len(domain.AllGateIDs()); got != want {
		t.Fatalf("report carries %d gates, want %d", got, want)
	}
	for _, g := range rep.Gates {
		if g.Status == domain.StatusFail || g.Status == domain.StatusWarn {
			t.Errorf("%s: status %q on an honest fix: %s", g.ID, g.Status, g.Message)
		}
		if g.Status == domain.StatusUnavailable && g.Reason == "" {
			t.Errorf("%s: unavailable without a reason", g.ID)
		}
	}
	if rep.Verdict != domain.VerdictPass || rep.ExitCode != 0 {
		t.Errorf("verdict = %q exit = %d, want pass and 0", rep.Verdict, rep.ExitCode)
	}
	if rep.Tier != 1 {
		t.Errorf("tier = %d, want 1: --no-hooks was passed", rep.Tier)
	}
	if rep.ConfigSource != "defaults" {
		t.Errorf("config_source = %q, want defaults: the fixture carries no redfirst.toml", rep.ConfigSource)
	}
}

func TestVerify_TextReportNamesEveryGate(t *testing.T) {
	t.Parallel()

	repo := testkit.CleanFix(t)
	var out bytes.Buffer

	if err := run(context.Background(), verifyArgs(repo.Dir), &out, io.Discard); err != nil {
		t.Fatalf("verify: %v", err)
	}

	text := out.String()
	for _, id := range domain.AllGateIDs() {
		if !strings.Contains(text, string(id)) {
			t.Errorf("the text report does not mention %s:\n%s", id, text)
		}
	}
	if !strings.Contains(text, "add .redfirst/ to enable") {
		t.Errorf("an N/A line must advertise the next tier:\n%s", text)
	}
	if !strings.Contains(text, "VERDICT: PASS") {
		t.Errorf("missing verdict line:\n%s", text)
	}
}

// TestVerify_TwoRunsProduceIdenticalJSON holds invariant 1: the core is a pure
// function of (repo, base, head), so nothing but the timings may move.
func TestVerify_TwoRunsProduceIdenticalJSON(t *testing.T) {
	t.Parallel()

	repo := testkit.CleanFix(t)
	first := runJSON(t, repo.Dir)
	second := runJSON(t, repo.Dir)

	if first != second {
		t.Errorf("two runs on the same input differ\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

func runJSON(t *testing.T, repoDir string) string {
	t.Helper()

	var out bytes.Buffer
	if err := run(context.Background(), verifyArgs(repoDir, "--report", "json"), &out, io.Discard); err != nil {
		t.Fatalf("verify: %v", err)
	}
	var rep domain.Report
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatalf("parse report: %v", err)
	}
	rep.Timings = domain.Timings{}

	stable, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		t.Fatalf("re-encode report: %v", err)
	}
	return string(stable)
}

// TestAssemble_GateWarningsReachTheReportInGateOrder covers the channel a gate
// uses for lines that leave the verdict alone: a guarded path or a suppression
// hit belongs to the gate that found it, and the report gathers them.
func TestAssemble_GateWarningsReachTheReportInGateOrder(t *testing.T) {
	t.Parallel()

	guarded := domain.Warning{Kind: domain.WarnGuardedPaths, File: "pnpm-lock.yaml"}
	suppression := domain.Warning{Kind: domain.WarnSuppression, File: "src/total.ts", Line: 42}
	downgrade := domain.Warning{Kind: domain.WarnConfig, Detail: "cases needs hooks"}
	results := []domain.GateResult{
		{ID: domain.GateProtectedPaths, Status: domain.StatusPass, Warnings: []domain.Warning{guarded}},
		{ID: domain.GateDiffBudget, Status: domain.StatusPass},
		{ID: domain.GateSuppressionScan, Status: domain.StatusPass, Warnings: []domain.Warning{suppression}},
	}

	rep := assemble(
		domain.Diff{}, domain.Config{}, "defaults",
		runner.NewCapSet(domain.CapDiff), results, []domain.Warning{downgrade},
	)

	want := []domain.Warning{downgrade, guarded, suppression}
	if !slices.Equal(rep.Warnings, want) {
		t.Errorf("warnings = %v, want %v", rep.Warnings, want)
	}
}

func TestVerify_WritesTheReportToTheOutFile(t *testing.T) {
	t.Parallel()

	repo := testkit.CleanFix(t)
	out := filepath.Join(t.TempDir(), "report.json")
	var stdout bytes.Buffer

	args := verifyArgs(repo.Dir, "--report", "json", "--out", out)
	if err := run(context.Background(), args, &stdout, io.Discard); err != nil {
		t.Fatalf("verify: %v", err)
	}

	if stdout.Len() != 0 {
		t.Errorf("stdout should stay empty when --out is given, got %q", stdout.String())
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read report file: %v", err)
	}
	if !json.Valid(data) {
		t.Errorf("report file does not hold json:\n%s", data)
	}
}

func TestVerify_GatesFlagRestrictsTheSet(t *testing.T) {
	t.Parallel()

	repo := testkit.CleanFix(t)
	var out bytes.Buffer

	args := verifyArgs(repo.Dir, "--report", "json", "--gates", "protected-paths,diff-budget")
	if err := run(context.Background(), args, &out, io.Discard); err != nil {
		t.Fatalf("verify: %v", err)
	}

	var rep domain.Report
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatalf("parse report: %v", err)
	}
	if len(rep.Gates) != len(domain.AllGateIDs()) {
		t.Fatalf("restricting the set must not drop lines from the report, got %d", len(rep.Gates))
	}
	for _, g := range rep.Gates {
		selected := g.ID == domain.GateProtectedPaths || g.ID == domain.GateDiffBudget
		if !selected && !strings.Contains(g.Reason, "--gates") {
			t.Errorf("%s: reason %q, want it to name --gates", g.ID, g.Reason)
		}
	}
}

func TestVerify_UnknownGateIsRejected(t *testing.T) {
	t.Parallel()

	repo := testkit.CleanFix(t)
	err := run(context.Background(), verifyArgs(repo.Dir, "--gates", "no-such-gate"), io.Discard, io.Discard)
	if err == nil {
		t.Fatal("an unknown gate id was accepted")
	}
	if got := domain.ExitCode(err); got != 4 {
		t.Errorf("exit code = %d, want 4", got)
	}
}

func TestVerify_UnresolvableBaseIsHarnessFailureNotAgentFault(t *testing.T) {
	t.Parallel()

	repo := testkit.CleanFix(t)
	args := []string{"verify", "--repo", repo.Dir, "--base", "origin/nowhere", "--head", testkit.FixtureHead}
	err := run(context.Background(), args, io.Discard, io.Discard)

	if got := domain.ExitCode(err); got != 2 {
		t.Errorf("exit code = %d, want 2: a missing base ref is not the agent's fault", got)
	}
}

// TestVerdictError_OnlyTheReportExplainsAVerdict keeps stderr for the failures
// the report cannot explain. A gate refusal already ends in a VERDICT line, so
// repeating "gate violation" on stderr is noise; a broken harness or a bad
// config never reaches that line and has to say something.
func TestVerdictError_OnlyTheReportExplainsAVerdict(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		err    error
		silent bool
	}{
		{"gate violation", domain.ErrGateViolation, true},
		{"no legal move", domain.ErrHumanRequired, true},
		{"harness failure", domain.ErrHarness, false},
		{"invalid config", domain.ErrConfig, false},
		{"internal error", domain.ErrInternal, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			if got := verdictError(c.err); got != c.silent {
				t.Errorf("verdictError(%v) = %v, want %v", c.err, got, c.silent)
			}
		})
	}
}

func TestRun_VersionPrintsTheVersion(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	if err := run(context.Background(), []string{"version"}, &out, io.Discard); err != nil {
		t.Fatalf("version: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != domain.Version {
		t.Errorf("version printed %q, want %q", got, domain.Version)
	}
}

func TestRun_UnknownCommandAlertsAHuman(t *testing.T) {
	t.Parallel()

	cases := map[string][]string{
		"no command":      {},
		"unknown command": {"audit"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var stderr bytes.Buffer
			err := run(context.Background(), args, io.Discard, &stderr)
			if err == nil {
				t.Fatal("no error")
			}
			if got := domain.ExitCode(err); got != 4 {
				t.Errorf("exit code = %d, want 4", got)
			}
			if !strings.Contains(stderr.String(), "usage:") {
				t.Errorf("no usage printed, got %q", stderr.String())
			}
		})
	}
}
