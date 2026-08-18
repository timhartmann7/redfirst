package cliux_test

import (
	"testing"
)

// The tests below are the part of doctor that executes. Every one of them is a
// hook that works in form: it exits 0, it writes a results file, and a project
// running it by hand would see nothing wrong. What each gets wrong is invisible
// until a verdict rests on it, which is why doctor runs rather than reads.

// vitestMarker gives the repository a runner doctor can name a command for. A
// diagnosis that ends in "fix your hook" is the thing section 4 of the spec
// forbids.
var vitestMarker = map[string]string{"package.json": `{"devDependencies":{"vitest":"^2.0.0"}}`}

// twoDirs is the tree shape the filter check needs: two test files a filter can
// tell apart, and in two directories, so a runner that filters by package can
// be told apart from one that ignores the filter altogether.
var twoDirs = []string{"src/total.test.js", "lib/discount.test.js"}

// TestDoctor_IgnoredFilterIsNamedWithTheCommandThatFixesIt is the check S7 owes
// the spec. A hook that runs everything works in form and makes every red-green
// probe run the whole suite.
func TestDoctor_IgnoredFilterIsNamedWithTheCommandThatFixesIt(t *testing.T) {
	t.Parallel()

	got := doctorFixture{tests: twoDirs, hook: "test-ignores-filter.sh", files: vitestMarker}.diagnose(t)

	wants(t, got,
		"ignored · test.sh reported the same 2 cases",
		"run the whole suite 3 times over",
		"vitest run $REDFIRST_FILTER",
	)
}

func TestDoctor_HonouredFilterIsReportedAsSuch(t *testing.T) {
	t.Parallel()

	got := doctorFixture{tests: twoDirs, hook: "test-honest.sh"}.diagnose(t)

	wants(t, got, "honoured · the two runs differ: lib/discount.test.js reported 1, src/total.test.js reported 1")
	rejects(t, got, "ignored")
}

// TestDoctor_TestFilesInOneDirectoryCannotSettleTheFilter keeps doctor from
// naming a cause it has not seen. `go test` takes packages, so two files of one
// directory answer the same whether the filter is honoured or not.
func TestDoctor_TestFilesInOneDirectoryCannotSettleTheFilter(t *testing.T) {
	t.Parallel()

	got := doctorFixture{
		tests: []string{"src/total.test.js", "src/discount.test.js"},
		hook:  "test-ignores-filter.sh",
	}.diagnose(t)

	wants(t, got, "cannot tell · src/ holds every file matching tests.patterns")
	rejects(t, got, "ignored")
}

// TestDoctor_ResultsWithoutPerCaseNamesNameTheExitCodeTheyCost covers the one
// requirement redfirst cannot work around: the names come from somebody else's
// runner, so a diff that modifies an existing test file has no legal move.
func TestDoctor_ResultsWithoutPerCaseNamesNameTheExitCodeTheyCost(t *testing.T) {
	t.Parallel()

	got := doctorFixture{tests: twoDirs, hook: "test-nameless.sh", files: vitestMarker}.diagnose(t)

	wants(t, got,
		"unnamed · test.sh reports cases without names",
		"exit code 5",
		`vitest run --reporter=junit --outputFile="$REDFIRST_RESULTS"`,
	)
}

// TestDoctor_AHookThatWritesNoResultsIsNamedApart separates a runner that names
// nothing from one that reports nothing at all. The second is a different fix.
func TestDoctor_AHookThatWritesNoResultsIsNamedApart(t *testing.T) {
	t.Parallel()

	got := doctorFixture{tests: twoDirs, hook: "test-silent.sh"}.diagnose(t)

	wants(t, got, "none · test.sh wrote no case to $REDFIRST_RESULTS")
	rejects(t, got, "unnamed")
}

// TestDoctor_ARerunThatDidNoWorkIsNamedWithWhatItCosts is the check section 7
// of the spec puts on doctor: a runner handing back its previous verdict turns
// probe_runs into one run, and the K-of-K rule is what stands between a flaky
// test and a green verdict.
func TestDoctor_ARerunThatDidNoWorkIsNamedWithWhatItCosts(t *testing.T) {
	t.Parallel()

	got := doctorFixture{
		tests: twoDirs,
		hook:  "test-replays.sh",
		files: map[string]string{"go.mod": "module example.com/x\n"},
	}.diagnose(t)

	wants(t, got,
		"unexplained gap ·",
		"collapses the 3 red-green probes into one",
		"go test -count=1",
	)
}

// TestDoctor_AHookThatStagesItsFirstRunIsNotAccusedOfAnything is the other
// side of the same measurement. The spec's own example installs dependencies on
// $REDFIRST_PROBE_INDEX 1, so the gap between the two runs has an innocent
// reading and doctor may not pick the guilty one.
func TestDoctor_AHookThatStagesItsFirstRunIsNotAccusedOfAnything(t *testing.T) {
	t.Parallel()

	got := doctorFixture{tests: twoDirs, hook: "test-staged.sh"}.diagnose(t)

	wants(t, got, "cannot tell ·", "branches on $REDFIRST_PROBE_INDEX")
	rejects(t, got, "unexplained gap")
}

// TestDoctor_TwoRunsThatBothCostTimeAreReportedFresh is the healthy case of the
// same check.
func TestDoctor_TwoRunsThatBothCostTimeAreReportedFresh(t *testing.T) {
	t.Parallel()

	got := doctorFixture{tests: twoDirs, hook: "test-steady.sh"}.diagnose(t)

	wants(t, got, "fresh · run 1 took")
	rejects(t, got, "unexplained gap")
}

// TestDoctor_ReusedServicesFlagMigrationsOutsideProtectedPaths covers section 13
// of the spec. Reuse is correct because base and head share a schema, and they
// share it only while the migrations sit where the agent cannot reach them.
func TestDoctor_ReusedServicesFlagMigrationsOutsideProtectedPaths(t *testing.T) {
	t.Parallel()

	got := doctorFixture{
		tests: twoDirs,
		hook:  "test-honest.sh",
		reset: true,
		files: map[string]string{"db/migrations/001_init.sql": "create table orders ();\n"},
	}.diagnose(t)

	wants(t, got,
		"reused · env-reset.sh resets the data",
		"db/migrations/001_init.sql is outside paths.protected",
		"add the path to paths.protected",
	)
}

// TestDoctor_ProtectedMigrationsLeaveReuseAlone is the same repository with the
// path where it belongs.
func TestDoctor_ProtectedMigrationsLeaveReuseAlone(t *testing.T) {
	t.Parallel()

	got := doctorFixture{
		tests:  twoDirs,
		hook:   "test-honest.sh",
		reset:  true,
		files:  map[string]string{"db/migrations/001_init.sql": "create table orders ();\n"},
		config: "version = 1\n\n[paths]\nprotected = [\"db/migrations/**\"]\n",
	}.diagnose(t)

	wants(t, got, "reused ·")
	rejects(t, got, "outside paths.protected")
}

// TestDoctor_FreshModeIsNamedWithTheFileThatWouldChangeIt keeps the mode out of
// the config: the presence of one hook picks it, and a reader has to see which.
func TestDoctor_FreshModeIsNamedWithTheFileThatWouldChangeIt(t *testing.T) {
	t.Parallel()

	got := doctorFixture{tests: twoDirs, hook: "test-honest.sh"}.diagnose(t)

	wants(t, got, "fresh · no env-reset.sh")
}

// TestDoctor_ABrokenEnvUpIsAFindingRatherThanACrash holds what separates doctor
// from verify. A harness that cannot start is the answer somebody came for, so
// it belongs in the diagnosis with what the hook printed.
func TestDoctor_ABrokenEnvUpIsAFindingRatherThanACrash(t *testing.T) {
	t.Parallel()

	got := doctorFixture{
		tests: twoDirs, workflow: true, hook: "test-honest.sh", up: "env-up-broken.sh",
	}.diagnose(t)

	wants(t, got, "tier=2", "env-up.sh", "the database refused the connection")
}

// TestDoctor_NoTestFileLeavesTheHarnessNothingToAnswerFor is the boundary the
// probe would otherwise index its way off: a repository with hooks and no test.
func TestDoctor_NoTestFileLeavesTheHarnessNothingToAnswerFor(t *testing.T) {
	t.Parallel()

	got := doctorFixture{hook: "test-honest.sh"}.diagnose(t)

	wants(t, got, "none on main match tests.patterns")
	rejects(t, got, "case names")
}
