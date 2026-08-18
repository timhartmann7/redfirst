package audit_test

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/audit"
	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

// sample is a summary built by hand, so a renderer test says nothing about git.
func sample() audit.Summary {
	return audit.Summary{
		Base:   "origin/main",
		Unit:   audit.ModeSquash,
		Caveat: "a PR merged as several commits counts several times",
		Units:  2,
		Days:   34,
		Notes:  []string{`tests.immutability "cases" needs hooks, audited as "append-only"`},
		Findings: []audit.Finding{
			{Category: "diff-budget", Units: 2},
			{Category: "guarded-paths", Units: 1},
		},
		PerUnit: []audit.Unit{
			{
				Commit: "8b2e740c1f4ade9c1f4ade9c1f4ade9c1f4ade9c", PR: "#412",
				Subject: "fix: apply the item discount to the total (#412)", Date: "2026-01-14",
				Diff:     domain.Stats{Files: 4, Added: 87, Deleted: 12},
				Tests:    domain.Stats{Files: 1, Added: 31},
				Findings: []string{"diff-budget", "guarded-paths"},
			},
			{
				Commit: "f3c9a11ade9c1f4ade9c1f4ade9c1f4ade9c1f4a", PR: "#409",
				Subject: "chore: bump the lockfile (#409)", Date: "2026-01-11",
				Diff:     domain.Stats{Files: 1, Added: 902, Deleted: 40},
				Findings: []string{"diff-budget"},
			},
		},
	}
}

func render(t *testing.T, s audit.Summary, f audit.Format, perUnit bool) string {
	t.Helper()

	var b bytes.Buffer
	if err := audit.Render(&b, s, f, perUnit); err != nil {
		t.Fatalf("render %s: %v", f, err)
	}
	return b.String()
}

// TestRender_TextOverAKnownHistoryMatchesTheGolden pins the whole summary, the
// header caveat included: the mode is what a reader has to see first.
func TestRender_TextOverAKnownHistoryMatchesTheGolden(t *testing.T) {
	t.Parallel()

	got := render(t, run(t, testkit.AuditHistory(t), options()), audit.FormatText, false)
	testkit.Golden(t, "audit-history.txt", []byte(got))
}

func TestRender_TextCarriesTheModeTheCaveatAndTheNotes(t *testing.T) {
	t.Parallel()

	got := render(t, sample(), audit.FormatText, false)

	for _, want := range []string{
		"Audited 2 squashed commits on origin/main (last 34 days)",
		"unit=squash · a PR merged as several commits counts several times",
		"note: tests.immutability",
		"  2  diff-budget",
		"  1  guarded-paths",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the report does not carry %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "PER UNIT") {
		t.Errorf("the breakdown appeared without --per-pr:\n%s", got)
	}
}

func TestRender_TextPerUnitNamesEveryUnitAndItsFindings(t *testing.T) {
	t.Parallel()

	got := render(t, sample(), audit.FormatText, true)

	for _, want := range []string{
		"PER UNIT",
		"8b2e740  2026-01-14",
		"4 files (+87 -12)",
		"diff-budget, guarded-paths",
		"fix: apply the item discount to the total (#412)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the breakdown does not carry %q:\n%s", want, got)
		}
	}
	for _, line := range strings.Split(got, "\n") {
		if strings.HasSuffix(line, " ") {
			t.Errorf("a report line ends in whitespace: %q", line)
		}
	}
}

func TestRender_TextSaysSoWhenNothingWasFound(t *testing.T) {
	t.Parallel()

	s := sample()
	s.Findings = nil
	if got := render(t, s, audit.FormatText, false); !strings.Contains(got, "no findings") {
		t.Errorf("an empty summary prints nothing at all:\n%s", got)
	}
}

func TestRender_JSONOmitsTheBreakdownUnlessAsked(t *testing.T) {
	t.Parallel()

	var without audit.Summary
	if err := json.Unmarshal([]byte(render(t, sample(), audit.FormatJSON, false)), &without); err != nil {
		t.Fatalf("parse json: %v", err)
	}
	if len(without.PerUnit) != 0 {
		t.Errorf("json carries %d units without --per-pr", len(without.PerUnit))
	}
	if without.Units != 2 || without.Unit != audit.ModeSquash {
		t.Errorf("json lost the count or the mode: %+v", without)
	}

	var with audit.Summary
	if err := json.Unmarshal([]byte(render(t, sample(), audit.FormatJSON, true)), &with); err != nil {
		t.Fatalf("parse json: %v", err)
	}
	if len(with.PerUnit) != 2 {
		t.Errorf("json carries %d units under --per-pr, want 2", len(with.PerUnit))
	}
}

func TestRender_CSVCountsCategoriesWithoutTheBreakdown(t *testing.T) {
	t.Parallel()

	rows := readCSV(t, render(t, sample(), audit.FormatCSV, false))

	want := [][]string{{"category", "units"}, {"diff-budget", "2"}, {"guarded-paths", "1"}}
	if len(rows) != len(want) {
		t.Fatalf("csv holds %d rows, want %d: %v", len(rows), len(want), rows)
	}
	for i := range want {
		if strings.Join(rows[i], ",") != strings.Join(want[i], ",") {
			t.Errorf("row %d = %v, want %v", i, rows[i], want[i])
		}
	}
}

// TestRender_CSVPerUnitCarriesTheDistribution covers what the flag is for:
// setting a budget from the upper quartile of your own history needs the
// numbers of every unit, not their sum.
func TestRender_CSVPerUnitCarriesTheDistribution(t *testing.T) {
	t.Parallel()

	rows := readCSV(t, render(t, sample(), audit.FormatCSV, true))

	if len(rows) != 3 {
		t.Fatalf("csv holds %d rows, want a header and two units: %v", len(rows), rows)
	}
	header := strings.Join(rows[0], ",")
	if header != "commit,pr,date,files,added,deleted,test_files,test_added,findings,subject" {
		t.Fatalf("csv header = %q", header)
	}
	if got := rows[1][3:6]; strings.Join(got, ",") != "4,87,12" {
		t.Errorf("the first unit counts %v, want 4 files +87 -12", got)
	}
	if got := rows[1][8]; got != "diff-budget guarded-paths" {
		t.Errorf("findings column = %q", got)
	}
}

func TestParseFormat_RejectsAnUnknownFormat(t *testing.T) {
	t.Parallel()

	if _, err := audit.ParseFormat("yaml"); err == nil {
		t.Error("an unknown report format was accepted")
	}
	for _, want := range []audit.Format{audit.FormatText, audit.FormatJSON, audit.FormatCSV} {
		got, err := audit.ParseFormat(string(want))
		if err != nil || got != want {
			t.Errorf("ParseFormat(%q) = %q, %v", want, got, err)
		}
	}
}

func readCSV(t *testing.T, s string) [][]string {
	t.Helper()

	rows, err := csv.NewReader(strings.NewReader(s)).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v\n%s", err, s)
	}
	return rows
}
