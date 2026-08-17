package runner_test

import (
	"context"
	"slices"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/runner"
)

func gateIndex(entries []runner.Entry, id domain.GateID) int {
	return slices.IndexFunc(entries, func(e runner.Entry) bool { return e.ID == id })
}

func TestRegistry_AllDeclaredGatesRegistered(t *testing.T) {
	t.Parallel()

	// The expected set comes from the domain, so declaring a gate without
	// registering it is a failure rather than a number nobody updated.
	want := domain.AllGateIDs()
	got := runner.Registry()

	t.Run("EveryDeclaredIDOccupiesItsOwnSlotInOrder", func(t *testing.T) {
		if len(got) != len(want) {
			t.Fatalf("registry holds %d gates, the domain declares %d", len(got), len(want))
		}
		for i, id := range want {
			if got[i].ID != id {
				t.Errorf("slot %d holds %q, want %q", i, got[i].ID, id)
			}
		}
	})

	t.Run("ConstructorBuildsTheGateItIsRegisteredUnder", func(t *testing.T) {
		for _, e := range got {
			if e.New == nil {
				t.Fatalf("gate %q has no constructor", e.ID)
			}
			if id := e.New(domain.Config{}).ID(); id != e.ID {
				t.Errorf("gate registered as %q reports itself as %q", e.ID, id)
			}
		}
	})

	t.Run("SuiteGreenRunsBeforeRedGreen", func(t *testing.T) {
		// Probes buy nothing against a suite already red on head.
		if gateIndex(got, domain.GateSuiteGreen) > gateIndex(got, domain.GateRedGreen) {
			t.Error("red-green is registered before suite-green")
		}
	})

	t.Run("DiffOnlyGatesRunBeforeTheFirstGateThatNeedsHooks", func(t *testing.T) {
		// The short-circuit lives in this order: a gate that reads nothing but
		// the diff costs milliseconds and has to get its refusal in before
		// anyone brings the environment up.
		lastStatic, firstHooks := -1, -1
		for i, e := range got {
			requires := e.New(domain.Config{}).Requires()
			if slices.Equal(requires, []domain.Capability{domain.CapDiff}) {
				lastStatic = i
			}
			if firstHooks < 0 && slices.Contains(requires, domain.CapHooks) {
				firstHooks = i
			}
		}
		if firstHooks < 0 {
			t.Fatal("no gate needs hooks; the short-circuit has nothing to cut off")
		}
		if lastStatic > firstHooks {
			t.Errorf("diff-only gate %q sits behind %q, which needs hooks", got[lastStatic].ID, got[firstHooks].ID)
		}
	})

	t.Run("ReturnsAFreshSliceEachCall", func(t *testing.T) {
		// Nothing about a registered gate may survive between runs.
		first := runner.Registry()
		first[0] = runner.Entry{}
		if runner.Registry()[0].ID != want[0] {
			t.Errorf("a mutated result changed the next call, want %q back", want[0])
		}
	})
}

func TestRunner_SkeletonRunSkipsEveryStubAndRefusesNothing(t *testing.T) {
	t.Parallel()

	// Every capability is present, so nothing hides behind unavailable: what
	// the run reports is what the stubs themselves return.
	caps := runner.NewCapSet(
		domain.CapDiff, domain.CapHooks, domain.CapTestFiles, domain.CapCaseNames, domain.CapStackTrace,
	)

	results, err := runner.Run(context.Background(), domain.Diff{}, runner.Options{Caps: caps})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != len(domain.AllGateIDs()) {
		t.Fatalf("got %d report lines, want one per declared gate", len(results))
	}
	for _, r := range results {
		if r.Status != domain.StatusSkip {
			t.Errorf("gate %q: status %q, want %q", r.ID, r.Status, domain.StatusSkip)
		}
		if r.Reason != "gate not implemented yet" {
			t.Errorf("gate %q: reason %q", r.ID, r.Reason)
		}
	}
	if code := domain.ExitCode(runner.Outcome(results)); code != 0 {
		t.Errorf("exit code %d, want 0: a skeleton run accuses nobody", code)
	}
}
