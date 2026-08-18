package audit_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/timhartmann7/redfirst/internal/audit"
	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

// TestAudit_SummaryOverAKnownHistoryMatchesExactly is the acceptance criterion
// of the slice. Every unit of the fixture carries a known finding, so the
// counts are checkable by hand and an off-by-one in the walk shows up here.
func TestAudit_SummaryOverAKnownHistoryMatchesExactly(t *testing.T) {
	t.Parallel()

	got := run(t, testkit.AuditHistory(t), options())
	want := []audit.Finding{
		{Category: "protected-paths", Units: 1},
		{Category: "diff-budget", Units: 1},
		{Category: "test-immutability", Units: 2},
		{Category: "no-disabled-tests", Units: 2},
		{Category: "suppression-scan", Units: 1},
		{Category: "guarded-paths", Units: 1},
	}

	if !slices.Equal(got.Findings, want) {
		t.Errorf("findings = %v\nwant %v", got.Findings, want)
	}
}

// TestAudit_AnHonestUnitCarriesNoFinding keeps the summary honest in the other
// direction: a count of one has to mean the other seven units were clean.
func TestAudit_AnHonestUnitCarriesNoFinding(t *testing.T) {
	t.Parallel()

	got := run(t, testkit.AuditHistory(t), options())

	oldest := got.PerUnit[len(got.PerUnit)-1]
	if oldest.Subject != "Merge pull request #1 from shop/discount" {
		t.Fatalf("the oldest unit is %q, want the honest fix", oldest.Subject)
	}
	if len(oldest.Findings) != 0 {
		t.Errorf("the honest fix tripped %v", oldest.Findings)
	}
	if oldest.Tests.Files != 1 {
		t.Errorf("the honest fix counts %d test files, want the one it touched", oldest.Tests.Files)
	}
}

// TestAudit_OneUnitCountsOnceForOneGate covers the rule the counts rest on: a
// gate that spoke names itself and its own report lines do not count again, so
// one empty catch block is one finding rather than two.
func TestAudit_OneUnitCountsOnceForOneGate(t *testing.T) {
	t.Parallel()

	got := run(t, testkit.AuditHistory(t), options())

	for _, u := range got.PerUnit {
		if slices.Contains(u.Findings, string(domain.GateSuppressionScan)) &&
			slices.Contains(u.Findings, string(domain.WarnSuppression)) {
			t.Errorf("unit %s counts one suppressed symptom twice: %v", u.Commit, u.Findings)
		}
	}
}

// TestAudit_NeverBringsTheEnvironmentUp holds the promise of tier 0: the
// command reads. Hooks on the base ref change nothing, and the gates that need
// them contribute no finding.
func TestAudit_NeverBringsTheEnvironmentUp(t *testing.T) {
	t.Parallel()

	marker := filepath.Join(t.TempDir(), "env-up-ran")
	r := testkit.AuditSquashHistory(t)
	r.Write(".redfirst/env-up.sh", "#!/bin/sh\ntouch "+marker+"\n")
	r.Write(".redfirst/test.sh", "#!/bin/sh\ntouch "+marker+"\n")
	r.Write(".redfirst/env-down.sh", "#!/bin/sh\n")
	r.Commit("chore: add the redfirst hooks (#15)")

	got := run(t, r, options())

	if _, err := os.Stat(marker); err == nil {
		t.Error("audit ran a hook; tier 0 executes nothing")
	}
	if got.Units != 5 {
		t.Errorf("audited %d units, want 5 with the hook commit", got.Units)
	}
	for _, u := range got.PerUnit {
		for _, gate := range []domain.GateID{domain.GateSuiteGreen, domain.GateRedGreen} {
			if slices.Contains(u.Findings, string(gate)) {
				t.Errorf("unit %s carries %s, a gate that needs an environment", u.Commit, gate)
			}
		}
	}
}

// TestAudit_CasesImmutabilityIsAuditedAsAppendOnlyWithANote covers the config
// row: the mode judges outcomes, audit produces none, and the downgrade toward
// the stricter rule may not happen quietly.
func TestAudit_CasesImmutabilityIsAuditedAsAppendOnlyWithANote(t *testing.T) {
	t.Parallel()

	opts := options()
	opts.Config.Tests.Immutability = domain.ImmutabilityCases
	got := run(t, testkit.AuditHistory(t), opts)

	if len(got.Notes) != 1 {
		t.Fatalf("notes = %v, want the one downgrade", got.Notes)
	}
	if !slices.ContainsFunc(got.Findings, func(f audit.Finding) bool {
		return f.Category == string(domain.GateTestImmutability)
	}) {
		t.Error("the downgraded gate judged nothing; append-only forbids more than cases does")
	}
}
