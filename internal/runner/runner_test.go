package runner_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/runner"
)

// fakeGate stands in for a real gate: these tests check the runner, and the
// stubs would tie them to slices that have not run yet.
type fakeGate struct {
	id       domain.GateID
	requires []domain.Capability
	result   domain.GateResult
	err      error
	runs     int
}

func (g *fakeGate) ID() domain.GateID             { return g.id }
func (g *fakeGate) Requires() []domain.Capability { return g.requires }

func (g *fakeGate) Run(context.Context, domain.Input) (domain.GateResult, error) {
	g.runs++
	return g.result, g.err
}

// entry registers one fake under its own ID. The constructor hands back the
// same instance so the test can read the run counter afterwards.
func entry(g *fakeGate) runner.Entry {
	return runner.Entry{ID: g.id, New: func(domain.Config) runner.Gate { return g }}
}

func staticGate(id domain.GateID, res domain.GateResult) *fakeGate {
	return &fakeGate{id: id, requires: []domain.Capability{domain.CapDiff}, result: res}
}

func hooksGate(id domain.GateID) *fakeGate {
	return &fakeGate{
		id:       id,
		requires: []domain.Capability{domain.CapDiff, domain.CapHooks},
		result:   domain.GateResult{Status: domain.StatusPass},
	}
}

func failure(rem domain.Remediation) domain.GateResult {
	return domain.GateResult{Status: domain.StatusFail, Message: "refused", Remediation: rem}
}

func run(t *testing.T, entries []runner.Entry, opts runner.Options) []domain.GateResult {
	t.Helper()
	results, err := runner.RunEntries(context.Background(), entries, domain.Diff{}, opts)
	if err != nil {
		t.Fatalf("RunEntries: unexpected error: %v", err)
	}
	if len(results) != len(entries) {
		t.Fatalf("got %d results for %d gates", len(results), len(entries))
	}
	return results
}

func TestRunner_GateWithoutCapabilityIsUnavailableWithReason(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		requires   []domain.Capability
		caps       runner.CapSet
		wantReason string
		wantHint   string
	}{
		{
			name:       "MissingHooksNameTheReasonAndTheWayOut",
			requires:   []domain.Capability{domain.CapDiff, domain.CapHooks},
			caps:       runner.NewCapSet(domain.CapDiff),
			wantReason: "no hooks",
			wantHint:   "add .redfirst/ to enable",
		},
		{
			name:       "FirstMissingCapabilityInRequiresOrderWins",
			requires:   []domain.Capability{domain.CapDiff, domain.CapTestFiles, domain.CapCaseNames},
			caps:       runner.NewCapSet(domain.CapDiff),
			wantReason: "no test files in diff",
		},
		{
			name:       "MissingCaseNamesAreReportedWithoutAHint",
			requires:   []domain.Capability{domain.CapDiff, domain.CapCaseNames},
			caps:       runner.NewCapSet(domain.CapDiff, domain.CapTestFiles),
			wantReason: "no per-case test names",
		},
		{
			name:       "MissingStackTraceIsReported",
			requires:   []domain.Capability{domain.CapDiff, domain.CapStackTrace},
			caps:       runner.NewCapSet(domain.CapDiff),
			wantReason: "no stack trace provided",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gate := &fakeGate{id: domain.GateRedGreen, requires: tc.requires}
			got := run(t, []runner.Entry{entry(gate)}, runner.Options{Caps: tc.caps})[0]

			if got.Status != domain.StatusUnavailable {
				t.Errorf("status = %q, want %q", got.Status, domain.StatusUnavailable)
			}
			if got.Reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", got.Reason, tc.wantReason)
			}
			if got.Hint != tc.wantHint {
				t.Errorf("hint = %q, want %q", got.Hint, tc.wantHint)
			}
			if gate.runs != 0 {
				t.Errorf("gate ran %d times without its capability", gate.runs)
			}
		})
	}
}

func TestRunner_GateRunsWhenEveryCapabilityIsPresent(t *testing.T) {
	t.Parallel()

	gate := hooksGate(domain.GateSuiteGreen)
	opts := runner.Options{Caps: runner.NewCapSet(domain.CapDiff, domain.CapHooks)}

	got := run(t, []runner.Entry{entry(gate)}, opts)[0]

	if got.Status != domain.StatusPass {
		t.Errorf("status = %q, want %q", got.Status, domain.StatusPass)
	}
	if gate.runs != 1 {
		t.Errorf("gate ran %d times, want 1", gate.runs)
	}
}

func TestRunner_StopsBeforeEnvWhenStaticGateFails(t *testing.T) {
	t.Parallel()

	allCaps := runner.NewCapSet(domain.CapDiff, domain.CapHooks)

	t.Run("HooksGateIsCutOffByAnEarlierRefusal", func(t *testing.T) {
		t.Parallel()

		static := staticGate(domain.GateProtectedPaths, failure(domain.RemediationRevertPaths))
		hooked := hooksGate(domain.GateSuiteGreen)

		got := run(t, []runner.Entry{entry(static), entry(hooked)}, runner.Options{Caps: allCaps})

		if got[1].Status != domain.StatusSkip {
			t.Errorf("status = %q, want %q", got[1].Status, domain.StatusSkip)
		}
		if !strings.Contains(got[1].Reason, string(domain.GateProtectedPaths)) {
			t.Errorf("reason = %q, want it to name %q", got[1].Reason, domain.GateProtectedPaths)
		}
		if hooked.runs != 0 {
			t.Errorf("hooks gate ran %d times after a static refusal", hooked.runs)
		}
	})

	t.Run("StaticGatesKeepRunningAfterARefusal", func(t *testing.T) {
		t.Parallel()

		first := staticGate(domain.GateProtectedPaths, failure(domain.RemediationRevertPaths))
		second := staticGate(domain.GateDiffBudget, domain.GateResult{Status: domain.StatusPass})

		got := run(t, []runner.Entry{entry(first), entry(second)}, runner.Options{Caps: allCaps})

		if got[1].Status != domain.StatusPass || second.runs != 1 {
			t.Errorf("static gate got %q after %d runs, want pass after 1", got[1].Status, second.runs)
		}
	})

	t.Run("HooksGateRunsWhenTheStaticGatesPass", func(t *testing.T) {
		t.Parallel()

		static := staticGate(domain.GateProtectedPaths, domain.GateResult{Status: domain.StatusPass})
		hooked := hooksGate(domain.GateSuiteGreen)

		run(t, []runner.Entry{entry(static), entry(hooked)}, runner.Options{Caps: allCaps})

		if hooked.runs != 1 {
			t.Errorf("hooks gate ran %d times, want 1", hooked.runs)
		}
	})

	t.Run("RefusalDowngradedToWarnDoesNotCutOffTheEnvironment", func(t *testing.T) {
		t.Parallel()

		static := staticGate(domain.GateDiffBudget, failure(domain.RemediationReduceScope))
		hooked := hooksGate(domain.GateSuiteGreen)
		opts := runner.Options{
			Config: domain.Config{Severity: map[domain.GateID]domain.Severity{
				domain.GateDiffBudget: domain.SeverityWarn,
			}},
			Caps: allCaps,
		}

		run(t, []runner.Entry{entry(static), entry(hooked)}, opts)

		if hooked.runs != 1 {
			t.Errorf("hooks gate ran %d times behind a warn, want 1", hooked.runs)
		}
	})
}

func TestRunner_DisabledGateIsSkipNotUnavailable(t *testing.T) {
	t.Parallel()

	// The gate also lacks its capability: a user choice has to outrank the
	// missing capability, otherwise the report blames the environment.
	gate := hooksGate(domain.GateSuiteGreen)
	opts := runner.Options{
		Config: domain.Config{Gates: domain.Gates{Disabled: []domain.GateID{domain.GateSuiteGreen}}},
		Caps:   runner.NewCapSet(domain.CapDiff),
	}

	got := run(t, []runner.Entry{entry(gate)}, opts)[0]

	if got.Status != domain.StatusSkip {
		t.Errorf("status = %q, want %q", got.Status, domain.StatusSkip)
	}
	if got.Reason != "disabled in config" {
		t.Errorf("reason = %q, want %q", got.Reason, "disabled in config")
	}
	if gate.runs != 0 {
		t.Errorf("disabled gate ran %d times", gate.runs)
	}
}

func TestRunner_GatesFlagSelectsTheGatesThatRun(t *testing.T) {
	t.Parallel()

	build := func() (*fakeGate, *fakeGate, []runner.Entry) {
		a := staticGate(domain.GateProtectedPaths, domain.GateResult{Status: domain.StatusPass})
		b := staticGate(domain.GateDiffBudget, domain.GateResult{Status: domain.StatusPass})
		return a, b, []runner.Entry{entry(a), entry(b)}
	}

	t.Run("UnselectedGateIsSkippedWithItsOwnReason", func(t *testing.T) {
		t.Parallel()

		a, b, entries := build()
		opts := runner.Options{Caps: runner.NewCapSet(domain.CapDiff), Only: []domain.GateID{domain.GateDiffBudget}}

		got := run(t, entries, opts)

		if got[0].Status != domain.StatusSkip || got[0].Reason != "not selected by --gates" {
			t.Errorf("got %q / %q, want skip / not selected by --gates", got[0].Status, got[0].Reason)
		}
		if a.runs != 0 || b.runs != 1 {
			t.Errorf("runs: unselected %d, selected %d; want 0 and 1", a.runs, b.runs)
		}
	})

	t.Run("EmptySelectionRunsEveryGate", func(t *testing.T) {
		t.Parallel()

		a, b, entries := build()

		run(t, entries, runner.Options{Caps: runner.NewCapSet(domain.CapDiff)})

		if a.runs != 1 || b.runs != 1 {
			t.Errorf("runs: %d and %d, want 1 and 1", a.runs, b.runs)
		}
	})
}

func TestRunner_GateErrorAbortsTheRun(t *testing.T) {
	t.Parallel()

	broken := &fakeGate{
		id:       domain.GateSuiteGreen,
		requires: []domain.Capability{domain.CapDiff},
		err:      fmt.Errorf("%w: test.sh exited 127", domain.ErrHarness),
	}
	later := staticGate(domain.GateRedGreen, domain.GateResult{Status: domain.StatusPass})
	entries := []runner.Entry{entry(broken), entry(later)}

	results, err := runner.RunEntries(
		context.Background(), entries, domain.Diff{}, runner.Options{Caps: runner.NewCapSet(domain.CapDiff)},
	)

	if !errors.Is(err, domain.ErrHarness) {
		t.Fatalf("error = %v, want it to wrap domain.ErrHarness", err)
	}
	if !strings.Contains(err.Error(), string(domain.GateSuiteGreen)) {
		t.Errorf("error %q does not name the failing gate", err)
	}
	if results != nil {
		t.Errorf("results = %v, want none: a broken harness may not yield a verdict", results)
	}
	if later.runs != 0 {
		t.Errorf("gate after the broken one ran %d times", later.runs)
	}
}

func TestRunner_FillsInTheIDAGateForgot(t *testing.T) {
	t.Parallel()

	// The registry owns the identity of a report line, so a forgetful gate
	// cannot produce an anonymous one.
	gate := &fakeGate{
		id:       domain.GateNewTestPresent,
		requires: []domain.Capability{domain.CapDiff},
		result:   domain.GateResult{Status: domain.StatusPass},
	}

	got := run(t, []runner.Entry{entry(gate)}, runner.Options{Caps: runner.NewCapSet(domain.CapDiff)})[0]

	if got.ID != domain.GateNewTestPresent {
		t.Errorf("id = %q, want %q", got.ID, domain.GateNewTestPresent)
	}
}

func TestRunner_ReturnsOneResultPerGateInOrder(t *testing.T) {
	t.Parallel()

	order := []domain.GateID{domain.GateProtectedPaths, domain.GateSuiteGreen, domain.GateBlastRadius}
	entries := []runner.Entry{
		entry(staticGate(order[0], failure(domain.RemediationRevertPaths))),
		entry(hooksGate(order[1])),
		entry(&fakeGate{id: order[2], requires: []domain.Capability{domain.CapDiff, domain.CapStackTrace}}),
	}

	got := run(t, entries, runner.Options{Caps: runner.NewCapSet(domain.CapDiff, domain.CapHooks)})

	for i, want := range order {
		if got[i].ID != want {
			t.Errorf("result %d is %q, want %q", i, got[i].ID, want)
		}
	}
	if got[1].Status != domain.StatusSkip || got[2].Status != domain.StatusUnavailable {
		t.Errorf("statuses = %q, %q; want skip and unavailable", got[1].Status, got[2].Status)
	}
}
