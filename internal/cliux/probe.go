package cliux

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/harness"
)

const (
	// rerunRatio is how much quicker the second run of a phase has to be before
	// the work of the first counts as skipped rather than merely warm.
	rerunRatio = 10
	// rerunFloor is the shortest first run the comparison reads anything into.
	// Starting a runner costs a fixed amount that the first execution of a
	// script pays in full, and below a second that cost is most of the
	// measurement: an accusation resting on it would name a cause nobody saw.
	rerunFloor = time.Second
)

// migrationDirs are the directory names a schema usually lives under. See
// reuseRows: whether they sit in paths.protected decides whether reusing the
// services stays correct.
var migrationDirs = []string{"migrations", "migrate"}

// harnessRows runs the project's own hooks and reports what only a run can
// settle.
func (d Doctor) harnessRows(ctx context.Context, markers Markers) []row {
	if d.NoHooks {
		return []row{{key: "harness", values: []string{"not probed · --no-hooks"}}}
	}
	return d.probe(ctx, helpFor(markers.Runner))
}

func (d Doctor) probe(ctx context.Context, help runnerHelp) (rows []row) {
	session, err := harness.Open(ctx, harness.Options{Source: d.Repo, Base: d.BaseSHA, WorkDir: d.WorkDir})
	if err != nil {
		return []row{problem("harness", err)}
	}
	defer func() {
		// A teardown that fails is a finding rather than a crash. Naming it is
		// the whole job of this command.
		if closeErr := session.Close(context.WithoutCancel(ctx)); closeErr != nil {
			rows = append(rows, problem(hookDown, closeErr))
		}
	}()

	rows = append(rows, row{key: "env mode", values: []string{envMode(session.Mode())}})
	return append(rows, d.checks(ctx, session, help)...)
}

// checks runs test.sh against the base tree and turns the runs into findings.
// Twice over one file, which is what a cached rerun shows up in, and once over
// a second file, which is what an ignored filter shows up in.
func (d Doctor) checks(ctx context.Context, session *harness.Session, help runnerHelp) []row {
	w, err := session.Worktree(ctx, domain.PhaseBase, d.BaseSHA)
	if err != nil {
		return []row{problem("working copy", err)}
	}
	tree, err := walk(w.Dir(), d.Config)
	if err != nil {
		return []row{problem("working copy", err)}
	}
	if len(tree.tests) == 0 {
		return []row{{key: "test files", values: []string{fmt.Sprintf(
			"none on %s match tests.patterns, so the hooks have nothing to answer for here", d.BaseRef)}}}
	}

	first, err := w.Run(ctx, tree.tests[:1])
	if err != nil {
		return []row{problem(hookTest, err)}
	}
	second, err := w.Run(ctx, tree.tests[:1])
	if err != nil {
		return []row{problem(hookTest, err)}
	}

	rows := []row{
		d.caseNamesRow(first, help),
		d.filterRow(ctx, w, tree.tests, first, help),
		d.rerunRow(ctx, first, second, help),
	}
	if session.Mode() == harness.ModeReused {
		rows = append(rows, d.reuseRows(tree.migrations)...)
	}
	return rows
}

// caseNamesRow covers the one requirement redfirst cannot work around on its
// own. Without per-case names red-green cannot tell a case the diff added from
// one base already had, so a diff that modifies an existing test file gets exit
// code 5, and no diff the agent writes changes that. The spec puts this check
// on doctor for exactly that reason.
func (d Doctor) caseNamesRow(run domain.Run, help runnerHelp) row {
	const key = "case names"
	switch {
	case len(run.Cases) == 0 && !run.Green:
		// The hook exited non-zero and named nothing, which is what a suite
		// that never built looks like. Every gate that reads the results file
		// is blind to why.
		return row{key: key, values: []string{
			fmt.Sprintf("none · %s exited non-zero and wrote no case to $REDFIRST_RESULTS", hookTest),
			fmt.Sprintf("run it by hand in a working copy to see what it said: %s", hookTest),
			help.results,
		}}
	case len(run.Cases) == 0:
		return row{key: key, values: []string{
			fmt.Sprintf("none · %s wrote no case to $REDFIRST_RESULTS, so every gate that reads it is blind", hookTest),
			help.results,
		}}
	case !run.Named():
		return row{key: key, values: []string{
			fmt.Sprintf("unnamed · %s reports cases without names", hookTest),
			"a diff that modifies an existing test file gets exit code 5 until that changes",
			help.results,
		}}
	}
	return row{key: key, values: []string{fmt.Sprintf("%s named by %s", plural(len(run.Cases)), hookTest)}}
}

// filterRow answers what only a second run settles: does test.sh restrict
// itself to $REDFIRST_FILTER?
//
// Two filtered runs over two different files, rather than one filtered run
// against the whole suite. The comparison against the suite calls a filter
// ignored wherever the other test files happen to hold no case at all, and
// doctor may not name a cause it has not seen.
func (d Doctor) filterRow(
	ctx context.Context, w *harness.Worktree, tests []string, first domain.Run, help runnerHelp,
) row {
	const key = "filter"
	a, b, ok := pair(tests)
	if !ok {
		return row{key: key, values: []string{
			fmt.Sprintf("cannot tell · %s holds every file matching tests.patterns", dir(tests[0])),
			"a runner that filters by directory answers the same whether it reads the filter or not",
		}}
	}
	other, err := w.Run(ctx, []string{b})
	if err != nil {
		return problem(key, err)
	}

	ran, alone := caseIDs(first), caseIDs(other)
	switch {
	case len(ran) == 0 && len(alone) == 0:
		return row{key: key, values: []string{fmt.Sprintf(
			"cannot tell · neither %s nor %s reported a case", a, b)}}
	case slices.Equal(ran, alone):
		return row{key: key, values: []string{
			fmt.Sprintf("ignored · %s reported the same %s for %s and for %s",
				hookTest, plural(len(ran)), a, b),
			fmt.Sprintf("the red-green probes will run the whole suite %d times over instead of the files of the diff",
				d.Config.RedGreen.ProbeRuns),
			help.filter,
		}}
	}
	return row{key: key, values: []string{fmt.Sprintf(
		"honoured · the two runs differ: %s reported %d, %s reported %d", a, len(ran), b, len(alone))}}
}

// pair picks the two files the filter check compares, in different directories
// wherever the tree offers them.
//
// A runner that takes packages rather than files, `go test` above all, answers
// the same for two files of one directory. Comparing those two would call an
// honest filter ignored, and doctor may not name a cause it has not seen.
func pair(tests []string) (a, b string, ok bool) {
	for _, t := range tests[1:] {
		if dir(t) != dir(tests[0]) {
			return tests[0], t, true
		}
	}
	return "", "", false
}

func dir(name string) string {
	if cut := strings.LastIndex(name, "/"); cut >= 0 {
		return name[:cut] + "/"
	}
	return "the repository root"
}

// rerunRow catches a runner that replays its own previous verdict. Probes that
// collapse into one leave probe_runs meaning nothing, and the identical result
// it demands is what stands between a fake test and a green verdict.
//
// Two runs of a phase do the same work by construction: the tree is identical
// byte for byte between them. The one legitimate reason for the second to be
// quicker is a hook that installs and builds on $REDFIRST_PROBE_INDEX 1, the
// way the spec's own example does, so a test.sh that never mentions the
// variable leaves the runner's own cache as the only explanation left.
func (d Doctor) rerunRow(ctx context.Context, first, second domain.Run, help runnerHelp) row {
	const key = "reruns"
	staged, err := d.testStages(ctx)
	if err != nil {
		return problem(key, err)
	}

	took := fmt.Sprintf("run 1 took %s, run 2 took %s", round(first.Took), round(second.Took))
	switch {
	case staged:
		return row{key: key, values: []string{fmt.Sprintf(
			"cannot tell · %s, and %s branches on $REDFIRST_PROBE_INDEX, so the first run does work the second skips",
			took, hookTest)}}
	case first.Took < rerunFloor:
		return row{key: key, values: []string{
			"cannot tell · " + took + ", too short to tell a replayed result from the cost of starting a runner",
		}}
	case second.Took*rerunRatio < first.Took:
		return row{key: key, values: []string{
			fmt.Sprintf("replayed · %s, and %s does the same work in both", took, hookTest),
			fmt.Sprintf("the %d red-green probes collapse into one, and a flaky test walks through",
				d.Config.RedGreen.ProbeRuns),
			help.rerun,
		}}
	}
	return row{key: key, values: []string{"fresh · " + took}}
}

// reuseRows covers what service reuse rests on, from section 13 of the spec:
// base and head share a schema because the migrations sit in paths.protected,
// so the agent cannot have changed them. Move them out and env-reset.sh resets
// the data of a schema the head phase already changed.
func (d Doctor) reuseRows(migrations []string) []row {
	if len(migrations) == 0 {
		return nil
	}
	return []row{{key: "reuse", values: []string{
		fmt.Sprintf("%s is outside paths.protected, and %s resets data without touching the schema",
			migrations[0], hookReset),
		"a diff may then change the schema between the base phase and the head one",
		fmt.Sprintf("add the path to paths.protected, or delete %s to cycle the services instead", hookReset),
	}}}
}

// testStages reports whether test.sh distinguishes the first run of a phase.
// It reads the hook from base, which is the copy that runs.
func (d Doctor) testStages(ctx context.Context) (bool, error) {
	r, err := d.Repo.ShowFile(ctx, d.BaseSHA, harness.HooksDir+"/"+hookTest)
	if err != nil {
		return false, err
	}
	defer func() { _ = r.Close() }()

	s := bufio.NewScanner(r)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		// A comment naming the variable explains the rule rather than acting on
		// it, and the generated blank template names it in so many words. Taking
		// that for a staged first run would leave the rerun timings unread on
		// every hook somebody filled in from the template.
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, "REDFIRST_PROBE_INDEX") {
			return true, nil
		}
	}
	return false, s.Err()
}

// baseTree is what one walk over the exported base working copy found. Both
// answers come out of the same walk: the copy lives for the length of the
// probe, and a second traversal would read whatever test.sh installed into it.
type baseTree struct {
	tests []string
	// migrations are the files under a migrations directory that
	// paths.protected leaves out. See reuseRows.
	migrations []string
}

func walk(dir string, cfg domain.Config) (baseTree, error) {
	var t baseTree
	err := filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if cfg.Tests.Patterns.Match(name) {
			t.tests = append(t.tests, name)
		}
		if underMigrations(name) && !cfg.Paths.Protected.Match(name) {
			t.migrations = append(t.migrations, name)
		}
		return nil
	})
	if err != nil {
		return baseTree{}, fmt.Errorf("read the working copy of %s: %w", dir, err)
	}
	// Sorted rather than left in walk order, so the file the probe filters to
	// is the same one on every machine.
	slices.Sort(t.tests)
	slices.Sort(t.migrations)
	return t, nil
}

func underMigrations(name string) bool {
	for _, part := range strings.Split(name, "/") {
		if slices.Contains(migrationDirs, part) {
			return true
		}
	}
	return false
}

// caseIDs is what a run reported, as a sorted set. The file travels with the
// name because a runner that reports per file and not per case still says
// which file it ran, and that is enough to tell two filters apart.
func caseIDs(run domain.Run) []string {
	ids := make([]string, 0, len(run.Cases))
	for _, c := range run.Cases {
		ids = append(ids, c.File+"\x00"+c.Name)
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

func envMode(m harness.Mode) string {
	if m == harness.ModeReused {
		return fmt.Sprintf("reused · %s resets the data and the services stay up between runs", hookReset)
	}
	return fmt.Sprintf("fresh · no %s, so the services cycle around every run", hookReset)
}

// problem is a finding the probe could not get past. A harness that cannot run
// is the specific thing to print rather than the thing to crash on, and a hook
// failure carries the tail of what the hook printed, line by line.
func problem(key string, err error) row {
	return row{key: key, values: strings.Split(strings.TrimSpace(err.Error()), "\n")}
}

func round(d time.Duration) time.Duration { return d.Round(time.Millisecond) }

// plural keeps a diagnosis from reading "1 cases".
func plural(n int) string {
	if n == 1 {
		return "1 case"
	}
	return fmt.Sprintf("%d cases", n)
}

// runnerHelp is the command a stack needs to satisfy the hook contract. Naming
// the fact of a problem is what section 4 of the spec forbids, so every finding
// ends in something somebody can paste.
type runnerHelp struct {
	results string
	filter  string
	rerun   string
}

func helpFor(runner string) runnerHelp {
	const (
		anyResults = "have test.sh write JUnit XML or TAP to $REDFIRST_RESULTS, one entry per case"
		anyFilter  = "pass $REDFIRST_FILTER to the runner, and run everything when it is empty"
		anyRerun   = "the runner has to execute the tests again rather than replay the run before it"
	)
	help := map[string]runnerHelp{
		"vitest": {
			results: `vitest run --reporter=junit --outputFile="$REDFIRST_RESULTS"`,
			filter:  "vitest run $REDFIRST_FILTER",
		},
		"jest": {
			results: `jest --reporters=jest-junit, with JEST_JUNIT_OUTPUT_FILE="$REDFIRST_RESULTS"`,
			filter:  "jest $REDFIRST_FILTER",
		},
		"pytest": {
			results: `pytest --junitxml="$REDFIRST_RESULTS"`,
			filter:  "pytest $REDFIRST_FILTER",
		},
		"go test": {
			results: `gotestsum --junitfile "$REDFIRST_RESULTS" -- ./...`,
			filter:  "gotestsum -- $(for f in $REDFIRST_FILTER; do dirname \"$f\"; done | sort -u)",
			rerun:   "go test -count=1: without it the test cache replays the verdict of the run before",
		},
	}
	h := help[runner]
	if h.results == "" {
		h.results = anyResults
	}
	if h.filter == "" {
		h.filter = anyFilter
	}
	if h.rerun == "" {
		h.rerun = anyRerun
	}
	return h
}
