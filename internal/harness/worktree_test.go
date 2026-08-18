package harness_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/harness"
	"github.com/timhartmann7/redfirst/internal/runner"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

func worktree(t *testing.T, s *harness.Session, phase harness.Phase, tree string) *harness.Worktree {
	t.Helper()

	w, err := s.Worktree(t.Context(), phase, tree)
	if err != nil {
		t.Fatalf("%s worktree: %v", phase, err)
	}
	return w
}

func mustRun(t *testing.T, w *harness.Worktree, filter ...string) harness.Result {
	t.Helper()

	res, err := w.Run(t.Context(), filter)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return res
}

// TestWorktree_InstallsDependenciesOncePerPhaseNotPerRun is the acceptance
// criterion of the slice, counted where it costs money: the working copy is
// built per phase, and the runs inside a phase share it. Otherwise probe_runs
// would mean three dependency installs and three builds for one pull request.
func TestWorktree_InstallsDependenciesOncePerPhaseNotPerRun(t *testing.T) {
	t.Parallel()

	f := newFixture(t, recording())
	s := f.open(t, false)
	defer closeSession(t, t.Context(), s)

	base := worktree(t, s, harness.PhaseBase, testkit.FixtureBase)
	for range 3 {
		mustRun(t, base)
	}
	head := worktree(t, s, harness.PhaseHead, testkit.FixtureHead)
	mustRun(t, head)

	if got := f.count("install base"); got != 1 {
		t.Errorf("the base phase installed dependencies %d times, want once", got)
	}
	if got := f.count("test base"); got != 3 {
		t.Errorf("the base phase ran test.sh %d times, want three", got)
	}
	if got := f.count("install head"); got != 1 {
		t.Errorf("the head phase installed dependencies %d times, want once", got)
	}
}

// TestWorktree_ProbeIndexCountsInsideThePhase covers the variable a hook keys
// its install off: it starts at one in every phase.
func TestWorktree_ProbeIndexCountsInsideThePhase(t *testing.T) {
	t.Parallel()

	f := newFixture(t, recording())
	s := f.open(t, false)
	defer closeSession(t, t.Context(), s)

	base := worktree(t, s, harness.PhaseBase, testkit.FixtureBase)
	first, second := mustRun(t, base), mustRun(t, base)
	head := mustRun(t, worktree(t, s, harness.PhaseHead, testkit.FixtureHead))

	if first.Index != 1 || second.Index != 2 || head.Index != 1 {
		t.Errorf("probe indexes are %d, %d and %d, want 1, 2 and 1", first.Index, second.Index, head.Index)
	}
	for _, want := range []string{"test base 1", "test base 2", "test head 1"} {
		if !slices.ContainsFunc(f.lines(), func(l string) bool { return strings.HasPrefix(l, want) }) {
			t.Errorf("the hook log carries no %q line:\n%s", want, strings.Join(f.lines(), "\n"))
		}
	}
}

// TestWorktree_EachPhaseGetsAWorkingCopyOfItsOwn is the other half of the same
// rule, and the one that decides a verdict: a dist directory compiled on base
// that survived into the head phase would turn a fix that does not exist green.
func TestWorktree_EachPhaseGetsAWorkingCopyOfItsOwn(t *testing.T) {
	t.Parallel()

	f := newFixture(t, recording())
	s := f.open(t, false)
	defer closeSession(t, t.Context(), s)

	base := worktree(t, s, harness.PhaseBase, testkit.FixtureBase)
	mustRun(t, base)
	mustRun(t, base)
	head := worktree(t, s, harness.PhaseHead, testkit.FixtureHead)
	mustRun(t, head)

	if base.Dir() == head.Dir() {
		t.Fatalf("both phases ran in %s", base.Dir())
	}
	// The second run of the base phase sees the artifact of the first, and the
	// first run of the head phase sees nothing.
	if !slices.Contains(f.lines(), "dist base 2 [built on base]") {
		t.Errorf("the runs of one phase did not share a working copy:\n%s", strings.Join(f.lines(), "\n"))
	}
	if !slices.Contains(f.lines(), "dist head 1 []") {
		t.Errorf("the head phase inherited the build of the base phase:\n%s", strings.Join(f.lines(), "\n"))
	}
}

// TestWorktree_LaysOutTheTreeOfItsPhase covers what a phase is: the source it
// runs against comes from the ref the caller named.
func TestWorktree_LaysOutTheTreeOfItsPhase(t *testing.T) {
	t.Parallel()

	f := newFixture(t, recording())
	s := f.open(t, false)
	defer closeSession(t, t.Context(), s)

	base := worktree(t, s, harness.PhaseBase, testkit.FixtureBase)
	head := worktree(t, s, harness.PhaseHead, testkit.FixtureHead)

	if got := readFile(t, base.Dir(), "src/total.js"); !strings.Contains(got, "=> 0") {
		t.Errorf("the base working copy holds %q", got)
	}
	if got := readFile(t, head.Dir(), "src/total.js"); !strings.Contains(got, "i.length") {
		t.Errorf("the head working copy holds %q", got)
	}
}

// TestWorktree_PassesTheFilterToTestSh covers the variable that keeps a probe
// to the files it is about: without it the probes run the whole suite.
func TestWorktree_PassesTheFilterToTestSh(t *testing.T) {
	t.Parallel()

	f := newFixture(t, recording())
	s := f.open(t, false)
	defer closeSession(t, t.Context(), s)

	w := worktree(t, s, harness.PhaseBase, testkit.FixtureBase)
	mustRun(t, w, "src/total.test.js", "src/order.test.js")
	mustRun(t, w)

	lines := f.lines()
	if !slices.ContainsFunc(lines, func(l string) bool {
		return strings.Contains(l, "filter=[src/total.test.js src/order.test.js]")
	}) {
		t.Errorf("the filter did not reach test.sh:\n%s", strings.Join(lines, "\n"))
	}
	if !slices.ContainsFunc(lines, func(l string) bool { return strings.Contains(l, "filter=[]") }) {
		t.Errorf("an empty filter has to run the whole suite:\n%s", strings.Join(lines, "\n"))
	}
	if !slices.ContainsFunc(lines, func(l string) bool { return strings.Contains(l, "workdir=["+w.Dir()+"]") }) {
		t.Errorf("test.sh did not run in the working copy:\n%s", strings.Join(lines, "\n"))
	}
}

func TestWorktree_ReportsTheCasesTheHarnessNamed(t *testing.T) {
	t.Parallel()

	f := newFixture(t, recording())
	s := f.open(t, false)
	defer closeSession(t, t.Context(), s)

	res := mustRun(t, worktree(t, s, harness.PhaseBase, testkit.FixtureBase))

	if !res.Green {
		t.Error("a run that exited zero is green")
	}
	want := []harness.Case{
		{File: "src/total.test.js", Name: "adds prices", Outcome: harness.OutcomePass},
		{File: "src/total.test.js", Name: "rounds the total", Outcome: harness.OutcomeSkip},
	}
	if !slices.Equal(res.Cases, want) {
		t.Errorf("cases = %v\nwant %v", res.Cases, want)
	}
}

// TestWorktree_ARedRunIsAnAnswerNotAHarnessFailure keeps the split the exit
// codes rest on: tests that failed are what the gates are here to read.
func TestWorktree_ARedRunIsAnAnswerNotAHarnessFailure(t *testing.T) {
	t.Parallel()

	set := recording()
	set.test = "test-red.sh"
	f := newFixture(t, set)
	s := f.open(t, false)
	defer closeSession(t, t.Context(), s)

	res, err := worktree(t, s, harness.PhaseBase, testkit.FixtureBase).Run(t.Context(), nil)
	if err != nil {
		t.Fatalf("a red run reported an error: %v", err)
	}
	if res.Green {
		t.Error("a run that exited non-zero is not green")
	}
	if len(res.Cases) != 1 || res.Cases[0].Outcome != harness.OutcomeFail {
		t.Errorf("cases = %v, want the one failure", res.Cases)
	}
}

// TestWorktree_ResultsWithoutNamesWithholdCapCaseNames is where the harness
// meets the capability model: red-green judges cases, and a harness that names
// none leaves the runner nothing to grant.
func TestWorktree_ResultsWithoutNamesWithholdCapCaseNames(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{"test-record.sh": true, "test-unnamed.sh": false}
	for script, want := range cases {
		t.Run(script, func(t *testing.T) {
			t.Parallel()

			set := recording()
			set.test = script
			f := newFixture(t, set)
			s := f.open(t, false)
			defer closeSession(t, t.Context(), s)

			res := mustRun(t, worktree(t, s, harness.PhaseBase, testkit.FixtureBase))
			if res.Named() != want {
				t.Errorf("Named() = %v, want %v for cases %v", res.Named(), want, res.Cases)
			}

			caps := runner.Capabilities(
				runner.Env{HooksPresent: true}, domain.Diff{},
				runner.HarnessProbe{CaseNames: res.Named()}, domain.Config{},
			)
			if got := caps.Has(domain.CapCaseNames); got != want {
				t.Errorf("CapCaseNames = %v, want %v", got, want)
			}
		})
	}
}

// TestWorktree_TheDeadlineKillsTheProcessTreeAndTearsDown holds the two things
// a timeout owes: killing the script alone would leave the containers and the
// test runner it started behind, and the environment still has to come down.
func TestWorktree_TheDeadlineKillsTheProcessTreeAndTearsDown(t *testing.T) {
	t.Parallel()

	set := recording()
	set.test = "test-hang.sh"
	set.reset = ""
	f := newFixture(t, set)
	s := f.open(t, false)
	defer closeSession(t, t.Context(), s)

	w := worktree(t, s, harness.PhaseBase, testkit.FixtureBase)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	// The cancellation stands in for the deadline and for a SIGINT alike: both
	// reach a run as a dead context. It waits for the grandchild rather than
	// for a clock, so the test never races the shell that starts it.
	child := make(chan int, 1)
	go func() {
		child <- f.waitForChild()
		cancel()
	}()

	started := time.Now()
	_, err := w.Run(ctx, nil)
	pid := <-child
	if pid == 0 {
		t.Fatalf("the hook never recorded the process it started: run said %v after %s, and the log holds\n%s",
			err, time.Since(started), strings.Join(f.lines(), "\n"))
	}
	if !errors.Is(err, domain.ErrHarness) {
		t.Fatalf("a run the cancellation killed: %v, want a harness error", err)
	}
	waitGone(t, pid)
	if got := f.count("env-down"); got != 1 {
		t.Errorf("env-down.sh ran %d times after the run was killed, want once", got)
	}
}

// waitForChild polls for the pid the hanging hook records. It runs off the test
// goroutine, so it reports a failure as zero rather than through t.
//
// The budget is generous on purpose. It bounds a failure rather than the
// behaviour: a loaded machine can take seconds to start a shell, and a test
// that calls that a regression is a test nobody trusts.
func (f *fixture) waitForChild() int {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		// The newline is the hook saying it finished writing.
		data, err := os.ReadFile(f.log + ".pid")
		if err == nil && strings.HasSuffix(string(data), "\n") {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
				return pid
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	return 0
}

// waitGone polls until the process is gone. The kill is asynchronous, and a
// grandchild is reaped by init rather than by us.
func waitGone(t *testing.T, pid int) {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("process %d outlived the run; the deadline killed the script and not its tree", pid)
}

func readFile(t *testing.T, dir, path string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path)))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
