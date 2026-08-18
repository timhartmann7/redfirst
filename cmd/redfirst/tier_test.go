package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

// runReport runs verify over a fixture and returns the parsed report.
//
// A refusal is an answer rather than a failure here: the report is written
// before verify returns the verdict, and what these tests read is the report.
// Anything else is a run that produced none.
func runReport(t *testing.T, repoDir string, extra ...string) domain.Report {
	t.Helper()

	args := append([]string{
		"verify",
		"--repo", repoDir,
		"--base", testkit.FixtureBase,
		"--head", testkit.FixtureHead,
		"--report", "json",
	}, extra...)

	var out bytes.Buffer
	err := run(context.Background(), args, &out, io.Discard)
	if err != nil && !errors.Is(err, domain.ErrGateViolation) && !errors.Is(err, domain.ErrHumanRequired) {
		t.Fatalf("verify: %v", err)
	}
	var rep domain.Report
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatalf("parse report: %v\n%s", err, out.String())
	}
	return rep
}

// TestVerify_HooksOnBaseGiveTierTwo proves the tier is discovered rather than
// declared: the same head commit reaches tier 2 purely because base carries
// .redfirst/, which is what "nothing gets rewritten between the tiers" means.
func TestVerify_HooksOnBaseGiveTierTwo(t *testing.T) {
	t.Parallel()

	repo := testkit.HookedFix(t)

	withHooks := runReport(t, repo.Dir)
	if withHooks.Tier != 2 {
		t.Errorf("tier = %d, want 2: base carries .redfirst/", withHooks.Tier)
	}
	if withHooks.ConfigSource != "base:redfirst.toml" {
		t.Errorf("config_source = %q, want base:redfirst.toml", withHooks.ConfigSource)
	}
	if got := gateByID(t, withHooks, domain.GateSuiteGreen); got.Status == domain.StatusUnavailable {
		t.Errorf("suite-green is %q with hooks present: %s", got.Status, got.Reason)
	}

	noHooks := runReport(t, repo.Dir, "--no-hooks")
	if noHooks.Tier != 1 {
		t.Errorf("tier = %d with --no-hooks, want 1", noHooks.Tier)
	}
	if got := gateByID(t, noHooks, domain.GateSuiteGreen); got.Status != domain.StatusUnavailable {
		t.Errorf("suite-green is %q under --no-hooks, want unavailable", got.Status)
	}
}

// TestVerify_CasesImmutabilityWithoutHooksIsDowngradedWithAReportLine covers
// the spec's validation row: the mode needs hooks, the downgrade moves toward
// strictness, and it may not happen quietly.
func TestVerify_CasesImmutabilityWithoutHooksIsDowngradedWithAReportLine(t *testing.T) {
	t.Parallel()

	repo := testkit.HookedFix(t)

	if warned := configWarning(runReport(t, repo.Dir)); warned != "" {
		t.Errorf("downgraded with hooks present: %q", warned)
	}

	warned := configWarning(runReport(t, repo.Dir, "--no-hooks"))
	if warned == "" {
		t.Fatal("cases without hooks was downgraded in silence")
	}
	if !bytes.Contains([]byte(warned), []byte("append-only")) {
		t.Errorf("warning = %q, want it to name the mode that now applies", warned)
	}
}

func gateByID(t *testing.T, rep domain.Report, id domain.GateID) domain.GateResult {
	t.Helper()

	for _, g := range rep.Gates {
		if g.ID == id {
			return g
		}
	}
	t.Fatalf("the report carries no %s line", id)
	return domain.GateResult{}
}

func configWarning(rep domain.Report) string {
	for _, w := range rep.Warnings {
		if w.Kind == domain.WarnConfig {
			return w.Detail
		}
	}
	return ""
}
