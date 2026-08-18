package harness_test

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/harness"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

// closeSession is what a caller writes: teardown reaches the environment
// through a defer, whatever ends the run.
func closeSession(t *testing.T, ctx context.Context, s *harness.Session) {
	t.Helper()

	if err := s.Close(ctx); err != nil {
		t.Errorf("close the session: %v", err)
	}
}

// TestSession_ReusedModeBringsTheServicesUpOnceAndResetsBetweenRuns covers the
// mode the presence of env-reset.sh picks: the expensive layer is paid for
// once, and the data goes back to a clean state between runs.
func TestSession_ReusedModeBringsTheServicesUpOnceAndResetsBetweenRuns(t *testing.T) {
	t.Parallel()

	f := newFixture(t, recording())
	s := f.open(t, false)

	if s.Mode() != harness.ModeReused {
		t.Fatalf("mode = %q, want %q: the hook set carries env-reset.sh", s.Mode(), harness.ModeReused)
	}
	w, err := s.Worktree(t.Context(), harness.PhaseBase, testkit.FixtureBase)
	if err != nil {
		t.Fatalf("base worktree: %v", err)
	}
	for range 3 {
		if _, err := w.Run(t.Context(), nil); err != nil {
			t.Fatalf("run: %v", err)
		}
	}
	closeSession(t, t.Context(), s)

	if got := f.count("env-up"); got != 1 {
		t.Errorf("env-up.sh ran %d times, want once", got)
	}
	if got := f.count("env-reset"); got != 2 {
		t.Errorf("env-reset.sh ran %d times, want twice: it runs between runs, not before the first", got)
	}
	if got := f.count("env-down"); got != 1 {
		t.Errorf("env-down.sh ran %d times, want once", got)
	}
}

// TestSession_FreshModeCyclesTheServicesAroundEveryRun covers the other row of
// the table: no reset hook, so nothing may be carried between runs.
func TestSession_FreshModeCyclesTheServicesAroundEveryRun(t *testing.T) {
	t.Parallel()

	set := recording()
	set.reset = ""
	f := newFixture(t, set)
	s := f.open(t, false)

	if s.Mode() != harness.ModeFresh {
		t.Fatalf("mode = %q, want %q without env-reset.sh", s.Mode(), harness.ModeFresh)
	}
	w, err := s.Worktree(t.Context(), harness.PhaseBase, testkit.FixtureBase)
	if err != nil {
		t.Fatalf("base worktree: %v", err)
	}
	for range 2 {
		if _, err := w.Run(t.Context(), nil); err != nil {
			t.Fatalf("run: %v", err)
		}
	}
	closeSession(t, t.Context(), s)

	if got := f.count("env-up"); got != 2 {
		t.Errorf("env-up.sh ran %d times, want one per run", got)
	}
	if got := f.count("env-down"); got != 2 {
		t.Errorf("env-down.sh ran %d times, want one per run", got)
	}
	if got := f.count("env-reset"); got != 0 {
		t.Errorf("env-reset.sh ran %d times without being part of the hook set", got)
	}
}

// TestSession_FreshFlagRefusesToReuseServicesThatOfferAReset covers --fresh-env
// on a hook set that would otherwise be reused.
func TestSession_FreshFlagRefusesToReuseServicesThatOfferAReset(t *testing.T) {
	t.Parallel()

	f := newFixture(t, recording())
	s := f.open(t, true)
	defer closeSession(t, t.Context(), s)

	if s.Mode() != harness.ModeFresh {
		t.Errorf("mode = %q under --fresh-env, want %q", s.Mode(), harness.ModeFresh)
	}
	if got := f.count("env-up"); got != 0 {
		t.Errorf("env-up.sh ran %d times at open; fresh mode brings the services up per run", got)
	}
}

// TestSession_AServiceLeftRunningDoesNotHoldTheRunOpen covers what env-up.sh
// is for: it starts services and returns, and the services outlive it. A hook
// whose output went down a pipe would keep the run waiting for the last holder
// of that pipe, which is the service itself.
func TestSession_AServiceLeftRunningDoesNotHoldTheRunOpen(t *testing.T) {
	t.Parallel()

	set := recording()
	set.up = "env-up-background.sh"
	f := newFixture(t, set)
	s := f.open(t, false)
	defer closeSession(t, t.Context(), s)

	if _, err := worktree(t, s, harness.PhaseBase, testkit.FixtureBase).Run(t.Context(), nil); err != nil {
		t.Fatalf("run: %v", err)
	}
}

// TestSession_TearsTheEnvironmentDownAfterAPanic holds invariant 8 on the path
// that loses the stack: a deferred Close is what makes teardown survive it.
func TestSession_TearsTheEnvironmentDownAfterAPanic(t *testing.T) {
	t.Parallel()

	f := newFixture(t, recording())

	func() {
		defer func() {
			if recover() == nil {
				t.Error("the panic did not reach the deferred close")
			}
		}()

		s := f.open(t, false)
		defer func() { _ = s.Close(t.Context()) }()
		panic("a gate blew up")
	}()

	if got := f.count("env-down"); got != 1 {
		t.Errorf("env-down.sh ran %d times after a panic, want once", got)
	}
}

// TestSession_TearsTheEnvironmentDownAfterTheDeadline holds the same invariant
// where the run's own context is already dead: teardown gets a context of its
// own, or the services stay up.
func TestSession_TearsTheEnvironmentDownAfterTheDeadline(t *testing.T) {
	t.Parallel()

	f := newFixture(t, recording())
	s := f.open(t, false)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := s.Close(ctx); err != nil {
		t.Fatalf("close after the deadline: %v", err)
	}
	if got := f.count("env-down"); got != 1 {
		t.Errorf("env-down.sh ran %d times on a dead context, want once", got)
	}
}

// TestSession_CloseIsSafeToCallTwice keeps a deferred close next to an explicit
// one from tearing the environment down twice.
func TestSession_CloseIsSafeToCallTwice(t *testing.T) {
	t.Parallel()

	f := newFixture(t, recording())
	s := f.open(t, false)

	closeSession(t, t.Context(), s)
	closeSession(t, t.Context(), s)

	if got := f.count("env-down"); got != 1 {
		t.Errorf("env-down.sh ran %d times, want once", got)
	}
}

// TestSession_CloseRemovesEverythingItCreated holds the requirement that every
// temporary directory goes, the panic path included.
func TestSession_CloseRemovesEverythingItCreated(t *testing.T) {
	t.Parallel()

	f := newFixture(t, recording())
	work := t.TempDir()

	s, err := harness.Open(t.Context(), harness.Options{
		Source: gitRepo(t, f), Base: testkit.FixtureBase, WorkDir: work,
	})
	if err != nil {
		t.Fatalf("open the session: %v", err)
	}
	if _, err := s.Worktree(t.Context(), harness.PhaseBase, testkit.FixtureBase); err != nil {
		t.Fatalf("base worktree: %v", err)
	}
	if entries, _ := os.ReadDir(work); len(entries) != 1 {
		t.Fatalf("the session created %d directories under the work dir, want one", len(entries))
	}

	closeSession(t, t.Context(), s)

	entries, err := os.ReadDir(work)
	if err != nil {
		t.Fatalf("read the work dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("close left %d entries behind: %v", len(entries), entries)
	}
}

// TestSession_RunIDDiffersBetweenSessions covers the isolation prefix hooks put
// into container and network names: two branches checked at once must not fight
// over the same resources.
func TestSession_RunIDDiffersBetweenSessions(t *testing.T) {
	t.Parallel()

	f := newFixture(t, recording())
	first := f.open(t, false)
	closeSession(t, t.Context(), first)
	second := f.open(t, false)
	closeSession(t, t.Context(), second)

	ids := make([]string, 0, 2)
	for _, l := range f.lines() {
		if id, ok := strings.CutPrefix(l, "env-up "); ok {
			ids = append(ids, id)
		}
	}
	if len(ids) != 2 {
		t.Fatalf("the log holds %d run ids, want two: %v", len(ids), f.lines())
	}
	if ids[0] == ids[1] {
		t.Errorf("both sessions ran as %q", ids[0])
	}
	if !strings.HasPrefix(ids[0], "redfirst-") {
		t.Errorf("run id = %q, want a prefix a human recognises in a container name", ids[0])
	}
}

// TestSession_BrokenEnvUpIsHarnessFailureNotAgentFault holds the split the
// orchestrator's retry budget rests on, and tears down what came up halfway.
func TestSession_BrokenEnvUpIsHarnessFailureNotAgentFault(t *testing.T) {
	t.Parallel()

	set := recording()
	set.up = "env-up-broken.sh"
	f := newFixture(t, set)

	_, err := f.tryOpen(t, false)
	if !errors.Is(err, domain.ErrHarness) {
		t.Fatalf("open with a broken env-up.sh: %v, want a harness error", err)
	}
	if got := domain.ExitCode(err); got != 2 {
		t.Errorf("exit code = %d, want 2", got)
	}
	if !strings.Contains(err.Error(), "the database refused the connection") {
		t.Errorf("the error hides what the hook said:\n%v", err)
	}
	if got := f.count("env-down"); got != 1 {
		t.Errorf("env-down.sh ran %d times after env-up.sh failed, want once", got)
	}
}

func TestSession_MissingRequiredHookIsHarnessFailure(t *testing.T) {
	t.Parallel()

	set := recording()
	set.test = ""
	f := newFixture(t, set)

	_, err := f.tryOpen(t, false)
	if !errors.Is(err, domain.ErrHarness) {
		t.Fatalf("open without test.sh: %v, want a harness error", err)
	}
	if !strings.Contains(err.Error(), "test.sh") {
		t.Errorf("the error does not name the missing hook:\n%v", err)
	}
}

// TestSession_NonExecutableHookNamesTheFile keeps the commonest setup mistake
// from arriving as a permission error about a temporary path.
func TestSession_NonExecutableHookNamesTheFile(t *testing.T) {
	t.Parallel()

	f := newFixture(t, recording())
	f.repo.Write(harness.HooksDir+"/test.sh", "#!/bin/sh\n")
	f.repo.Commit("chore: commit test.sh without its bit")

	_, err := f.tryOpen(t, false)
	if !errors.Is(err, domain.ErrHarness) {
		t.Fatalf("open with a non-executable test.sh: %v, want a harness error", err)
	}
	if !strings.Contains(err.Error(), "not executable") || !strings.Contains(err.Error(), "test.sh") {
		t.Errorf("the error does not name the problem:\n%v", err)
	}
}

// TestSession_EnvScriptExportsAheadOfTheOtherHooks covers the optional env.sh:
// it is sourced, so what it exports reaches the hook that runs after it.
func TestSession_EnvScriptExportsAheadOfTheOtherHooks(t *testing.T) {
	t.Parallel()

	set := recording()
	set.env = "env.sh"
	f := newFixture(t, set)
	s := f.open(t, false)
	defer closeSession(t, t.Context(), s)

	w, err := s.Worktree(t.Context(), harness.PhaseBase, testkit.FixtureBase)
	if err != nil {
		t.Fatalf("base worktree: %v", err)
	}
	if _, err := w.Run(t.Context(), nil); err != nil {
		t.Fatalf("run: %v", err)
	}

	if !slices.ContainsFunc(f.lines(), func(l string) bool { return strings.Contains(l, "greeting=[hello]") }) {
		t.Errorf("test.sh did not see what env.sh exported:\n%s", strings.Join(f.lines(), "\n"))
	}
}
