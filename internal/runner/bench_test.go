package runner_test

import (
	"context"
	"testing"
	"time"

	"github.com/timhartmann7/redfirst/internal/config"
	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/gitx"
	"github.com/timhartmann7/redfirst/internal/harness"
	"github.com/timhartmann7/redfirst/internal/runner"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

// hookCost is what one run of the fake harness is made to cost. It stands in
// for the dependency install, the build and the suite of a real project, which
// is where the minutes of a tier 2 run actually go.
//
// Large enough that the core's share is measurable against it, small enough
// that the whole benchmark stays inside a few seconds.
const hookCost = 200 * time.Millisecond

const benchProbeRuns = 3

// BenchmarkTierTwo_CoreOverheadOnTopOfTheHooks is the third performance budget
// from section 10 of the spec, and the one that matters most: tier 1 fits
// inside 300 ms without effort, while tier 2 costs whatever the hooks cost, and
// the core's job there is to add nothing on top.
//
// The hook time is measured rather than assumed. Counting probe_runs against a
// sleep would leave out env-up.sh, env-down.sh and whatever the runner itself
// does before it reports, and every second of that would land on the core's
// bill and make the number meaningless. The harness times each hook process, so
// what is left after subtracting them from the wall clock is the core: the git
// exports, the overlay, the diff arithmetic and the gates.
//
// Two metrics come out, and core-ms/op is the one to watch. The 5% of the
// budget is a ratio against a real project's hooks, which cost minutes; the
// fake harness here costs under two seconds, so the same absolute core time
// prints as a far larger percentage than a real repository would ever see.
// Read %overhead as an upper bound rather than as the budget itself, and track
// core-ms/op for regressions.
//
// Both are reported rather than asserted: a benchmark that failed on a
// threshold would go red on a loaded machine and teach everyone to ignore it,
// and the comparison belongs in CI, which is what section 9 of CLAUDE.md asks
// for.
func BenchmarkTierTwo_CoreOverheadOnTopOfTheHooks(b *testing.B) {
	repo := benchRepo(b)

	var hooks time.Duration
	for b.Loop() {
		hooks += benchVerify(b, repo)
	}
	b.StopTimer()

	core := b.Elapsed() - hooks
	b.ReportMetric(float64(core)/float64(hooks)*100, "%overhead")
	b.ReportMetric(float64(core.Microseconds())/1000/float64(b.N), "core-ms/op")
}

// benchVerify runs every gate over the fixture the way verify does, environment
// included, and fails on anything but a clean acquittal: a run cut off by a
// refusal would measure a path nobody pays for.
func benchVerify(b *testing.B, repo *testkit.Repo) time.Duration {
	b.Helper()

	ctx := context.Background()
	git, err := gitx.Open(ctx, repo.Dir)
	if err != nil {
		b.Fatalf("open %s: %v", repo.Dir, err)
	}
	diff, err := git.Diff(ctx, testkit.FixtureBase, testkit.FixtureHead)
	if err != nil {
		b.Fatalf("diff: %v", err)
	}

	cfg := benchConfig()
	session, err := harness.Open(ctx, harness.Options{
		Source: git, Base: diff.Base, WorkDir: b.TempDir(),
	})
	if err != nil {
		b.Fatalf("open the session: %v", err)
	}
	defer func() {
		if err := session.Close(ctx); err != nil {
			b.Fatalf("close the session: %v", err)
		}
	}()

	probe := runner.NewProbe(session, git, diff, cfg)
	results, err := runner.Run(ctx, diff, runner.Options{
		Config: cfg,
		Caps:   runner.Capabilities(runner.Env{HooksPresent: true}, diff, runner.HarnessProbe{}, cfg),
		Open:   func(context.Context) (domain.Probe, error) { return probe, nil },
	})
	if err != nil {
		b.Fatalf("run the gates: %v", err)
	}
	if err := runner.Outcome(results); err != nil {
		b.Fatalf("the benchmark fixture is not an honest fix: %v", err)
	}
	return hookTime(probe.Timings())
}

// hookTime is everything the report bills to the project's own scripts. The
// deferred teardown lands after this reads, so env-down.sh of the last run
// counts toward the core, which errs against us and is the right direction for
// a budget.
func hookTime(t domain.Timings) time.Duration {
	seconds := t.EnvUpS + t.BaseProbeS + t.HeadProbeS + t.SuiteS + t.BaseSuiteS
	return time.Duration(seconds * float64(time.Second))
}

// benchConfig is the rule set compiled into the binary, which is what a repo
// on tier 2 without a redfirst.toml is judged by.
func benchConfig() domain.Config {
	cfg := config.Defaults()
	cfg.RedGreen.ProbeRuns = benchProbeRuns
	return cfg
}

// benchRepo is the honest fix, over a harness whose runs cost a known amount.
func benchRepo(b *testing.B) *testkit.Repo {
	b.Helper()

	r := testkit.NewRepo(b)
	r.Write("src/total.js", probeSource)
	r.Write("src/total.test.js", probeTest)
	testkit.WriteHooks(r, testkit.Hooks{Cost: hookCost})
	r.Commit("feat: add the order total and the hooks")

	r.Branch(testkit.FixtureHead)
	honestFix(r)
	r.Commit("fix: apply the item discount to the total")
	return r
}
