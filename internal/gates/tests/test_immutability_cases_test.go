package tests_test

import (
	"context"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/gates/tests"
)

// casesProbe hands the gate the runs a harness would have produced. Mode cases
// judges what the runner returned, so a fixture repository would only put a
// shell script between the test and the thing under test.
type casesProbe struct {
	inventory domain.Runs
	head      domain.Runs
}

func (p casesProbe) Inventory(context.Context) (domain.Runs, error) { return p.inventory, nil }
func (p casesProbe) Head(context.Context) (domain.Runs, error)      { return p.head, nil }

func (p casesProbe) Overlaid(context.Context) (domain.Runs, error)  { return nil, nil }
func (p casesProbe) Suite(context.Context) (domain.Runs, error)     { return nil, nil }
func (p casesProbe) BaseSuite(context.Context) (domain.Runs, error) { return nil, nil }

func (p casesProbe) Retry(context.Context, []string) (domain.Runs, error) { return nil, nil }

const casesFile = "src/total.test.js"

// probeRun is one execution of the hook, named case by case.
func probeRun(index int, outcomes map[string]domain.CaseOutcome, order ...string) domain.Run {
	run := domain.Run{Index: index, Green: true}
	for _, name := range order {
		outcome := outcomes[name]
		if outcome == domain.CaseFail {
			run.Green = false
		}
		run.Cases = append(run.Cases, domain.Case{File: casesFile, Name: name, Outcome: outcome})
	}
	return run
}

// passing is one green run over the named cases.
func passing(names ...string) domain.Runs {
	outcomes := make(map[string]domain.CaseOutcome, len(names))
	for _, name := range names {
		outcomes[name] = domain.CasePass
	}
	return domain.Runs{probeRun(1, outcomes, names...)}
}

// TestTestImmutability_CasesJudgesOutcomesRatherThanLines walks the mode's whole
// rule: the names base reported have to sit inside the names on head, and every
// one that carried over has to be green there.
func TestTestImmutability_CasesJudgesOutcomesRatherThanLines(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		probe   casesProbe
		want    domain.GateStatus
		message string
		// evidence is the case the report has to name, empty where it names
		// none.
		evidence string
	}{
		{
			// Renaming a symbol that tests cover needs edits to the test file,
			// and strict and append-only both refuse those. The case names do
			// not move with the symbol, so cases lets the refactor through.
			name: "ARenamedSymbolKeepsEveryCase",
			probe: casesProbe{
				inventory: passing("adds prices"),
				head:      passing("adds prices"),
			},
			want:    domain.StatusPass,
			message: "cases · 0 removed, 0 regressed",
		},
		{
			name: "ACaseAddedAlongsideTheOldOnesIsFine",
			probe: casesProbe{
				inventory: passing("adds prices"),
				head:      passing("adds prices", "applies a discount"),
			},
			want:    domain.StatusPass,
			message: "cases · 0 removed, 0 regressed",
		},
		{
			// Deleting a test is the violation the whole tool exists for, and
			// under cases it shows up as a name that stopped being reported.
			name: "ACaseThatDisappearedOnHeadIsRemoved",
			probe: casesProbe{
				inventory: passing("adds prices", "rounds the total"),
				head:      passing("adds prices"),
			},
			want:     domain.StatusFail,
			message:  "cases · 1 removed",
			evidence: "rounds the total",
		},
		{
			// Hollowing a case out into a stub keeps the name and loses the
			// insurance, so the mode looks at the outcome as well.
			name: "ACaseThatWentRedOnHeadIsRegressed",
			probe: casesProbe{
				inventory: passing("adds prices"),
				head: domain.Runs{probeRun(1,
					map[string]domain.CaseOutcome{"adds prices": domain.CaseFail}, "adds prices")},
			},
			want:     domain.StatusFail,
			message:  "cases · 1 regressed",
			evidence: "adds prices",
		},
		{
			// Green in one run and gone from the next is not green: K-of-K
			// applies to a case that carried over exactly as it does to a new
			// one.
			name: "ACaseGreenInOnlySomeHeadRunsIsRegressed",
			probe: casesProbe{
				inventory: passing("adds prices"),
				head: domain.Runs{
					probeRun(1, map[string]domain.CaseOutcome{"adds prices": domain.CasePass}, "adds prices"),
					probeRun(2, nil),
				},
			},
			want:     domain.StatusFail,
			message:  "cases · 1 regressed",
			evidence: "adds prices",
		},
		{
			// A diff that removed every test file leaves no head run at all,
			// and every case base carried is gone with them.
			name: "NoHeadRunAtAllLosesEveryCase",
			probe: casesProbe{
				inventory: passing("adds prices"),
			},
			want:     domain.StatusFail,
			message:  "cases · 1 removed",
			evidence: "adds prices",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// A fixtures list that claims nothing in the diff, so the file
			// answers to tests.immutability and the mode under test applies.
			cfg := casesConfig(t, []string{"**/*.snap"}, domain.ImmutabilityCases, domain.ImmutabilityWarn)
			res := runCasesGate(t, cfg, tc.probe)

			assertStatus(t, res, tc.want)
			if res.Message != tc.message {
				t.Errorf("message = %q, want %q", res.Message, tc.message)
			}
			if tc.want == domain.StatusFail && res.Remediation != domain.RemediationFixCode {
				t.Errorf("remediation = %q, want %q: the sources answer for an outcome",
					res.Remediation, domain.RemediationFixCode)
			}
			assertCaseEvidence(t, res, tc.evidence)
		})
	}
}

// TestTestImmutability_CasesGovernsTheTestSurfaceToo covers the second list.
// tests.fixtures takes the same modes, and a snapshot judged by outcome answers
// the same question a test file does.
func TestTestImmutability_CasesGovernsTheTestSurfaceToo(t *testing.T) {
	t.Parallel()

	// Both lists claim the file here, and tests.fixtures speaks first for a
	// path they share, so the mode under test is the fixtures one.
	cfg := casesConfig(t, []string{"**/*.test.*"}, domain.ImmutabilityAppendOnly, domain.ImmutabilityCases)
	res := runCasesGate(t, cfg, casesProbe{inventory: passing("adds prices")})

	assertStatus(t, res, domain.StatusFail)
	assertCaseEvidence(t, res, "adds prices")
}

// casesConfig puts the modes on the two lists. The fixtures patterns are the
// argument because they are what decides which list claims the file the diff
// modifies, and so which of the two modes the run applies.
func casesConfig(t *testing.T, fixtures []string, mode, fixturesMode domain.Immutability) domain.Config {
	t.Helper()

	return testsConfig(t, rules{
		patterns:     []string{"**/*.test.*"},
		fixtures:     fixtures,
		immutability: mode,
		fixturesMode: fixturesMode,
	})
}

// runCasesGate drives the gate over a diff that modifies one test file, which
// is what makes the mode apply at all.
func runCasesGate(t *testing.T, cfg domain.Config, probe casesProbe) domain.GateResult {
	t.Helper()

	res, err := tests.NewTestImmutability(cfg).Run(context.Background(), domain.Input{
		Diff:   domain.Diff{Files: []domain.FileChange{modified(casesFile, 3)}},
		Config: cfg,
		Probe:  probe,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res
}

func assertCaseEvidence(t *testing.T, res domain.GateResult, want string) {
	t.Helper()

	if want == "" {
		if len(res.Evidence) != 0 {
			t.Errorf("evidence = %v, want none", res.Evidence)
		}
		return
	}
	if len(res.Evidence) != 1 {
		t.Fatalf("evidence = %v, want the one case that broke the rule", res.Evidence)
	}
	if res.Evidence[0].Case != want {
		t.Errorf("evidence names case %q, want %q", res.Evidence[0].Case, want)
	}
}
