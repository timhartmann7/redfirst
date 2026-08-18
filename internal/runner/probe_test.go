package runner_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/gitx"
	"github.com/timhartmann7/redfirst/internal/harness"
	"github.com/timhartmann7/redfirst/internal/runner"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

const probeSource = `export function total(items) {
  return items.reduce((sum, item) => sum + item.price, 0)
}
`

const probeFixed = `export function total(items) {
  return items.reduce((sum, item) => sum + item.price * (1 - (item.discount ?? 0)), 0)
}
`

const probeTest = `// case: adds prices
`

const probeAddedCase = `// case: applies a discount | needs: discount
`

// probeFixture is an honest fix: one new case in an existing test file, and a
// source change that is what makes the case pass.
type probeFixture struct {
	repo  *testkit.Repo
	log   string
	diff  domain.Diff
	probe *runner.Probe
}

func newProbeFixture(t *testing.T, build func(r *testkit.Repo)) *probeFixture {
	t.Helper()

	f := &probeFixture{log: filepath.Join(t.TempDir(), "hooks.log")}
	f.repo = testkit.NewRepo(t)
	f.repo.Write("src/total.js", probeSource)
	f.repo.Write("src/total.test.js", probeTest)
	testkit.WriteHooks(f.repo, testkit.Hooks{Log: f.log})
	f.repo.Commit("feat: add the order total and the hooks")

	f.repo.Branch(testkit.FixtureHead)
	build(f.repo)
	f.repo.Commit("fix: apply the item discount to the total")

	git, err := gitx.Open(t.Context(), f.repo.Dir)
	if err != nil {
		t.Fatalf("open %s: %v", f.repo.Dir, err)
	}
	if f.diff, err = git.Diff(t.Context(), testkit.FixtureBase, testkit.FixtureHead); err != nil {
		t.Fatalf("diff: %v", err)
	}

	session, err := harness.Open(t.Context(), harness.Options{
		Source: git, Base: f.diff.Base, WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("open the session: %v", err)
	}
	t.Cleanup(func() {
		if err := session.Close(context.WithoutCancel(t.Context())); err != nil {
			t.Errorf("close the session: %v", err)
		}
	})

	cfg := probeConfig(t)
	f.probe = runner.NewProbe(session, git, f.diff, cfg)
	return f
}

// probeConfig is the built-in shape of the test surface, spelled out here so
// the probe test does not depend on internal/config.
func probeConfig(t *testing.T) domain.Config {
	t.Helper()

	patterns, err := domain.NewPatternSet([]string{"**/*.test.*"})
	if err != nil {
		t.Fatalf("tests.patterns: %v", err)
	}
	return domain.Config{
		Tests:    domain.Tests{Patterns: patterns},
		RedGreen: domain.RedGreen{ProbeRuns: 2},
		Suite:    domain.Suite{RetryFailed: 1},
	}
}

// phases is the order the hooks ran in, one line per run.
func (f *probeFixture) phases(t *testing.T) []string {
	t.Helper()

	data, err := os.ReadFile(f.log)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("read the hook log: %v", err)
	}
	var runs []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if rest, ok := strings.CutPrefix(line, "test "); ok {
			runs = append(runs, rest)
		}
	}
	return runs
}

// honestFix appends a case whose marker only the fixed source carries.
func honestFix(r *testkit.Repo) {
	r.Write("src/total.js", probeFixed)
	r.Write("src/total.test.js", probeTest+probeAddedCase)
}

// TestProbe_OverlayCarriesTheTestFilesAndNotTheFix is the base probe's whole
// claim: the added case runs against the sources as base has them, so it goes
// red there and green once the fix arrives.
func TestProbe_OverlayCarriesTheTestFilesAndNotTheFix(t *testing.T) {
	t.Parallel()

	f := newProbeFixture(t, honestFix)

	overlaid, err := f.probe.Overlaid(t.Context())
	if err != nil {
		t.Fatalf("base probe: %v", err)
	}
	if got := overlaid.Outcomes("applies a discount"); !got.All(domain.CaseFail) {
		t.Errorf("the new case on base: %v, want a failure in every run", got)
	}
	if got := overlaid.Outcomes("adds prices"); !got.All(domain.CasePass) {
		t.Errorf("the case base already had: %v, want a pass in every run", got)
	}

	head, err := f.probe.Head(t.Context())
	if err != nil {
		t.Fatalf("head probe: %v", err)
	}
	if got := head.Outcomes("applies a discount"); !got.All(domain.CasePass) {
		t.Errorf("the new case on head: %v, want a pass in every run", got)
	}
}

// TestProbe_HeadPhaseRunsBehindTheWholeBasePhase covers the one ordering the
// spec fixes: state leaks forward only, and the base probe is the single run
// in the system that expects red, so a leak into it produces an acquittal.
func TestProbe_HeadPhaseRunsBehindTheWholeBasePhase(t *testing.T) {
	t.Parallel()

	f := newProbeFixture(t, honestFix)

	// Head asked for first, the way test-immutability in cases mode asks for
	// it: the base phase still has to be behind us by then.
	if _, err := f.probe.Head(t.Context()); err != nil {
		t.Fatalf("head probe: %v", err)
	}

	want := []string{"base 1", "base 2", "base 3", "head 1", "head 2"}
	got := f.phases(t)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("runs = %v, want %v: inventory, then two base probes, then head", got, want)
	}
}

// TestProbe_EachPhaseRunsOnceHoweverManyGatesAsk holds the sharing the whole
// design rests on: two gates read the same runs, and neither pays for them
// twice.
func TestProbe_EachPhaseRunsOnceHoweverManyGatesAsk(t *testing.T) {
	t.Parallel()

	f := newProbeFixture(t, honestFix)

	for range 2 {
		if _, err := f.probe.Inventory(t.Context()); err != nil {
			t.Fatalf("inventory: %v", err)
		}
		if _, err := f.probe.Head(t.Context()); err != nil {
			t.Fatalf("head probe: %v", err)
		}
	}

	if got := len(f.phases(t)); got != 5 {
		t.Errorf("%d runs for two rounds of asking, want 5: 1 inventory + 2 base + 2 head", got)
	}
}

// TestProbe_NoInventoryWhereTheDiffModifiesNoTestFile keeps the run a diff of
// new test files always cost off the bill.
func TestProbe_NoInventoryWhereTheDiffModifiesNoTestFile(t *testing.T) {
	t.Parallel()

	f := newProbeFixture(t, func(r *testkit.Repo) {
		r.Write("src/total.js", probeFixed)
		r.Write("src/discount.test.js", probeAddedCase)
	})

	runs, err := f.probe.Inventory(t.Context())
	if err != nil {
		t.Fatalf("inventory: %v", err)
	}
	if len(runs) != 0 {
		t.Errorf("the inventory ran %d times over a diff that modifies no test file", len(runs))
	}
	if got := f.phases(t); len(got) != 0 {
		t.Errorf("the hook ran %v, want nothing", got)
	}
}

// TestProbe_OverlayRemovesATestTheDiffDeleted covers the direction a file-level
// overlay would miss: the base copy of a deleted test may not stay behind to
// answer for a file the agent removed.
func TestProbe_OverlayRemovesATestTheDiffDeleted(t *testing.T) {
	t.Parallel()

	f := newProbeFixture(t, func(r *testkit.Repo) {
		r.Remove("src/total.test.js")
	})

	inventory, err := f.probe.Inventory(t.Context())
	if err != nil {
		t.Fatalf("inventory: %v", err)
	}
	if got := inventory.Names(); len(got) != 1 || got[0] != "adds prices" {
		t.Fatalf("inventory named %v, want the case base carried", got)
	}

	overlaid, err := f.probe.Overlaid(t.Context())
	if err != nil {
		t.Fatalf("base probe: %v", err)
	}
	if got := overlaid.Names(); len(got) != 0 {
		t.Errorf("the base probe still reports %v after the overlay dropped the file", got)
	}
}

// TestProbe_SuitePhaseRunsUnfilteredAndTheBaseSuiteReplacesIt covers the last
// two runs suite-green can ask for, and the working copy they share.
func TestProbe_SuitePhaseRunsUnfilteredAndTheBaseSuiteReplacesIt(t *testing.T) {
	t.Parallel()

	f := newProbeFixture(t, honestFix)

	suite, err := f.probe.Suite(t.Context())
	if err != nil {
		t.Fatalf("suite: %v", err)
	}
	if !suite.AllGreen() {
		t.Errorf("the suite on head is red: %v", suite)
	}

	base, err := f.probe.BaseSuite(t.Context())
	if err != nil {
		t.Fatalf("base suite: %v", err)
	}
	if got := base.Names(); len(got) != 1 || got[0] != "adds prices" {
		t.Errorf("the base suite reported %v, want the case base carries alone", got)
	}
	// Both runs are the first of their working copy, so a hook installs
	// dependencies for each of them exactly once.
	want := []string{"suite 1", "suite 1"}
	if got := f.phases(t); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("runs = %v, want %v", got, want)
	}
}
