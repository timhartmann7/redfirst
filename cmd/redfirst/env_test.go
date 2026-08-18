package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/testkit"
)

// TestVerify_ReusedModeBringsTheServicesUpOnceForTheWholeRun is the reused-env
// fixture end to end. The presence of env-reset.sh picks the mode, not a config
// flag, and the whole point of it is that a run pays for the services once
// instead of once per probe.
//
// Reuse breaks toward safety: runs go base then head, so data can only leak
// forward, and the head phase expects green. A leak therefore produces an
// unfair refusal rather than an acquittal, which costs one retry.
func TestVerify_ReusedModeBringsTheServicesUpOnceForTheWholeRun(t *testing.T) {
	t.Parallel()

	// Outside the repository and outside the run directory: teardown removes
	// everything the session created, and teardown is half of what this checks.
	log := filepath.Join(t.TempDir(), "hooks.log")
	repo := onBase(t, testkit.HookedFix(t), "chore: reset the data between runs", func(r *testkit.Repo) {
		testkit.WriteHooks(r, testkit.Hooks{Reset: true, Log: log})
	})

	rep := runReport(t, repo.Dir)

	if rep.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0 on an honest fix", rep.ExitCode)
	}
	if rep.EnvMode != "reused" {
		t.Fatalf("env_mode = %q, want reused: the base ref carries env-reset.sh", rep.EnvMode)
	}

	lines := hookLog(t, log)
	if got := count(lines, "env-up"); got != 1 {
		t.Errorf("env-up.sh ran %d times, want once for the whole run:\n%s", got, strings.Join(lines, "\n"))
	}
	if got := count(lines, "env-down"); got != 1 {
		t.Errorf("env-down.sh ran %d times, want once:\n%s", got, strings.Join(lines, "\n"))
	}
	// One reset before every run but the first: the services carry the state of
	// the run before, and only a reset makes them safe to hand on.
	runs, resets := count(lines, "test "), count(lines, "env-reset")
	if resets != runs-1 {
		t.Errorf("%d runs and %d resets, want a reset between every pair:\n%s",
			runs, resets, strings.Join(lines, "\n"))
	}
	if lines[len(lines)-1] != "env-down" {
		t.Errorf("the run ended on %q, want the teardown last:\n%s", lines[len(lines)-1], strings.Join(lines, "\n"))
	}
}

func hookLog(t *testing.T, path string) []string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the hook log: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func count(lines []string, prefix string) int {
	n := 0
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			n++
		}
	}
	return n
}
