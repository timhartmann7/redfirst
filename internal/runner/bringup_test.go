package runner_test

import (
	"context"
	"errors"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/runner"
)

// fakeProbe counts what the runner asked of the environment. The executing
// gates get their own fixtures; what this file checks is when the runner brings
// the environment up at all.
type fakeProbe struct {
	inventory domain.Runs
	err       error
	opens     int
	runs      int
}

func (p *fakeProbe) open(context.Context) (domain.Probe, error) {
	p.opens++
	return p, p.err
}

func (p *fakeProbe) Inventory(context.Context) (domain.Runs, error) {
	p.runs++
	return p.inventory, nil
}

func (p *fakeProbe) Overlaid(context.Context) (domain.Runs, error)  { return nil, nil }
func (p *fakeProbe) Head(context.Context) (domain.Runs, error)      { return nil, nil }
func (p *fakeProbe) Suite(context.Context) (domain.Runs, error)     { return nil, nil }
func (p *fakeProbe) BaseSuite(context.Context) (domain.Runs, error) { return nil, nil }

func (p *fakeProbe) Retry(context.Context, []string) (domain.Runs, error) { return nil, nil }

// namedInventory is what a harness that names its cases reports for the test
// files of the diff.
func namedInventory() domain.Runs {
	return domain.Runs{{
		Index: 1, Green: true,
		Cases: []domain.Case{{File: "src/total.test.js", Name: "adds prices", Outcome: domain.CasePass}},
	}}
}

// namelessInventory is what a runner without per-case results reports: it ran,
// it was green, and it named nothing.
func namelessInventory() domain.Runs {
	return domain.Runs{{Index: 1, Green: true}}
}

// caseNamesGate stands in for red-green and for test-immutability in cases
// mode: it needs the environment and the names the inventory produces.
func caseNamesGate(id domain.GateID) *fakeGate {
	return &fakeGate{
		id:       id,
		requires: []domain.Capability{domain.CapDiff, domain.CapHooks, domain.CapCaseNames},
		result:   domain.GateResult{Status: domain.StatusPass},
	}
}

func TestRunner_BringsTheEnvironmentUpOnceAheadOfTheFirstGateThatNeedsIt(t *testing.T) {
	t.Parallel()

	probe := &fakeProbe{inventory: namedInventory()}
	first, second := hooksGate(domain.GateSuiteGreen), caseNamesGate(domain.GateRedGreen)
	static := staticGate(domain.GateProtectedPaths, domain.GateResult{Status: domain.StatusPass})

	got := run(t, []runner.Entry{entry(static), entry(first), entry(second)}, runner.Options{
		Caps: runner.NewCapSet(domain.CapDiff, domain.CapHooks),
		Open: probe.open,
	})

	if probe.opens != 1 || probe.runs != 1 {
		t.Errorf("the environment came up %d times and inventoried %d times, want once each",
			probe.opens, probe.runs)
	}
	for i, res := range got {
		if res.Status != domain.StatusPass {
			t.Errorf("gate %d is %q: %s %s", i, res.Status, res.Reason, res.Message)
		}
	}
}

// TestRunner_StaticRefusalLeavesTheEnvironmentUntouched is the short-circuit
// measured where it pays: a refused diff must not cost a container.
func TestRunner_StaticRefusalLeavesTheEnvironmentUntouched(t *testing.T) {
	t.Parallel()

	probe := &fakeProbe{inventory: namedInventory()}
	static := staticGate(domain.GateProtectedPaths, failure(domain.RemediationRevertPaths))
	hooked := hooksGate(domain.GateSuiteGreen)

	run(t, []runner.Entry{entry(static), entry(hooked)}, runner.Options{
		Caps: runner.NewCapSet(domain.CapDiff, domain.CapHooks),
		Open: probe.open,
	})

	if probe.opens != 0 {
		t.Errorf("the environment came up %d times after a static refusal, want none", probe.opens)
	}
}

// TestRunner_MissingHooksBringNoEnvironmentUp keeps the unavailable line free
// of side effects: without the capability there is nothing to open.
func TestRunner_MissingHooksBringNoEnvironmentUp(t *testing.T) {
	t.Parallel()

	probe := &fakeProbe{inventory: namedInventory()}
	hooked := hooksGate(domain.GateSuiteGreen)

	got := run(t, []runner.Entry{entry(hooked)}, runner.Options{
		Caps: runner.NewCapSet(domain.CapDiff),
		Open: probe.open,
	})

	if probe.opens != 0 {
		t.Errorf("the environment came up %d times without CapHooks", probe.opens)
	}
	if got[0].Status != domain.StatusUnavailable || got[0].Reason != "no hooks" {
		t.Errorf("got %q / %q, want unavailable / no hooks", got[0].Status, got[0].Reason)
	}
}

func TestRunner_CaseNamesArriveFromTheInventoryRun(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		inventory domain.Runs
		want      domain.GateStatus
	}{
		{"NamedResultsGrantTheCapability", namedInventory(), domain.StatusPass},
		{
			// The gate has to say what is missing rather than run blind: no
			// diff the agent writes can add names to somebody else's runner.
			name: "ARunnerWithoutNamesWithholdsIt", inventory: namelessInventory(),
			want: domain.StatusUnavailable,
		},
		{
			// A diff that modifies no existing test file has no inventory to
			// run and no existing case to lose, so the capability holds with
			// nothing to show for it.
			name: "NoInventoryToRunGrantsItAnyway", inventory: nil,
			want: domain.StatusPass,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			probe := &fakeProbe{inventory: tc.inventory}
			gate := caseNamesGate(domain.GateRedGreen)

			got := run(t, []runner.Entry{entry(gate)}, runner.Options{
				Caps: runner.NewCapSet(domain.CapDiff, domain.CapHooks),
				Open: probe.open,
			})

			if got[0].Status != tc.want {
				t.Errorf("status = %q, want %q (reason %q)", got[0].Status, tc.want, got[0].Reason)
			}
			if tc.want == domain.StatusUnavailable && got[0].Reason != "no per-case test names" {
				t.Errorf("reason = %q, want the missing capability named", got[0].Reason)
			}
		})
	}
}

// TestRunner_EnvironmentThatCannotComeUpLeavesNoVerdict holds the boundary a
// broken harness sits on: exit code 2, never an acquittal.
func TestRunner_EnvironmentThatCannotComeUpLeavesNoVerdict(t *testing.T) {
	t.Parallel()

	probe := &fakeProbe{err: domain.ErrHarness}
	hooked := hooksGate(domain.GateSuiteGreen)

	_, err := runner.RunEntries(context.Background(), []runner.Entry{entry(hooked)},
		domain.Diff{}, runner.Options{
			Caps: runner.NewCapSet(domain.CapDiff, domain.CapHooks),
			Open: probe.open,
		})

	if !errors.Is(err, domain.ErrHarness) {
		t.Fatalf("err = %v, want a harness failure", err)
	}
	if code := domain.ExitCode(err); code != 2 {
		t.Errorf("exit code %d, want 2", code)
	}
}
