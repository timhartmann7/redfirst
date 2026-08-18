package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/audit"
	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

func auditArgs(repoDir string, extra ...string) []string {
	return append([]string{"audit", "--repo", repoDir, "--base", testkit.FixtureBase}, extra...)
}

func runAuditJSON(t *testing.T, repoDir string, extra ...string) audit.Summary {
	t.Helper()

	var out bytes.Buffer
	args := auditArgs(repoDir, append([]string{"--report", "json"}, extra...)...)
	if err := run(context.Background(), args, &out, io.Discard); err != nil {
		t.Fatalf("audit: %v", err)
	}
	var s audit.Summary
	if err := json.Unmarshal(out.Bytes(), &s); err != nil {
		t.Fatalf("parse summary: %v\n%s", err, out.String())
	}
	return s
}

// TestAudit_FindingsLeaveTheExitCodeAtZero holds what tier 0 is: a command that
// tells you something about your own repository, not a verdict on it.
func TestAudit_FindingsLeaveTheExitCodeAtZero(t *testing.T) {
	t.Parallel()

	repo := testkit.AuditHistory(t)
	var out bytes.Buffer

	err := run(context.Background(), auditArgs(repo.Dir), &out, io.Discard)
	if err != nil {
		t.Fatalf("audit: %v (exit %d)", err, domain.ExitCode(err))
	}
	if !strings.Contains(out.String(), "protected-paths") {
		t.Errorf("the report found nothing on a history built to trip the gates:\n%s", out.String())
	}
}

// TestAudit_WritesNothingIntoTheRepository holds invariant 6 for the one
// command that walks a repository's whole history.
func TestAudit_WritesNothingIntoTheRepository(t *testing.T) {
	t.Parallel()

	repo := testkit.AuditHistory(t)
	before := repo.Git("rev-parse", testkit.FixtureBase)

	if err := run(context.Background(), auditArgs(repo.Dir), io.Discard, io.Discard); err != nil {
		t.Fatalf("audit: %v", err)
	}

	if got := repo.Git("status", "--porcelain"); got != "" {
		t.Errorf("the working tree changed:\n%s", got)
	}
	if after := repo.Git("rev-parse", testkit.FixtureBase); after != before {
		t.Errorf("base moved from %s to %s", before, after)
	}
}

// TestAudit_ReadsTheConfigFromBase covers invariant 5 on this command: the
// rules come from the ref things were merged into, not from built-in defaults.
func TestAudit_ReadsTheConfigFromBase(t *testing.T) {
	t.Parallel()

	repo := testkit.AuditHistory(t)
	withDefaults := runAuditJSON(t, repo.Dir)

	repo.Write("redfirst.toml", "version = 1\n\n[budget]\nmax_added_lines = 1\n")
	repo.Commit("chore: narrow the diff budget")
	withConfig := runAuditJSON(t, repo.Dir)

	if got := findingUnits(withDefaults, domain.GateDiffBudget); got != 1 {
		t.Fatalf("diff-budget fired on %d units under the defaults, want 1", got)
	}
	if got := findingUnits(withConfig, domain.GateDiffBudget); got <= 1 {
		t.Errorf("diff-budget fired on %d units under max_added_lines = 1, want more than one", got)
	}
}

func TestAudit_RejectsFlagsItCannotHonour(t *testing.T) {
	t.Parallel()

	cases := map[string][]string{
		"unknown unit mode": {"--unit", "pull-request"},
		"unknown format":    {"--report", "yaml"},
		"last below one":    {"--last", "0"},
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			repo := testkit.AuditSquashHistory(t)
			err := run(context.Background(), auditArgs(repo.Dir, extra...), io.Discard, io.Discard)
			if err == nil {
				t.Fatal("the flag was accepted")
			}
			if got := domain.ExitCode(err); got != 4 {
				t.Errorf("exit code = %d, want 4", got)
			}
		})
	}
}

func TestAudit_UnknownBaseIsHarnessFailureNotAgentFault(t *testing.T) {
	t.Parallel()

	repo := testkit.AuditSquashHistory(t)
	args := []string{"audit", "--repo", repo.Dir, "--base", "origin/nowhere"}
	err := run(context.Background(), args, io.Discard, io.Discard)

	if got := domain.ExitCode(err); got != 2 {
		t.Errorf("exit code = %d, want 2: a missing base ref is not the agent's fault", got)
	}
}

func findingUnits(s audit.Summary, id domain.GateID) int {
	for _, f := range s.Findings {
		if f.Category == string(id) {
			return f.Units
		}
	}
	return 0
}
