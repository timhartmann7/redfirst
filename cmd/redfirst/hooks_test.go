package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

// TestVerify_MissingHookDirectoryIsUnavailableNotARefusal is the no-hooks
// fixture end to end, and the property tier 1 rests on: a repository that never
// wrote a hook gets a complete verdict on everything else. An incomplete set of
// gates is a valid set.
func TestVerify_MissingHookDirectoryIsUnavailableNotARefusal(t *testing.T) {
	t.Parallel()

	// No --no-hooks: the run has to discover the absence for itself.
	rep := runReport(t, testkit.CleanFix(t).Dir)

	if rep.Tier != 1 {
		t.Errorf("tier = %d, want 1: the fixture carries no %s", rep.Tier, ".redfirst")
	}
	if rep.ExitCode != 0 || rep.Verdict != domain.VerdictPass {
		t.Errorf("verdict = %q exit = %d, want a pass: no hook is nobody's violation",
			rep.Verdict, rep.ExitCode)
	}
	for _, id := range []domain.GateID{domain.GateSuiteGreen, domain.GateRedGreen} {
		got := gateByID(t, rep, id)
		if got.Status != domain.StatusUnavailable {
			t.Errorf("%s is %q, want unavailable", id, got.Status)
		}
		if got.Reason != "no hooks" {
			t.Errorf("%s: reason = %q, want it to name the missing capability", id, got.Reason)
		}
		if got.Hint == "" {
			t.Errorf("%s: an unavailable line has to say how to remove it", id)
		}
	}
	if got := gateByID(t, rep, domain.GateProtectedPaths); got.Status != domain.StatusPass {
		t.Errorf("protected-paths is %q; the static gates judge the diff either way", got.Status)
	}
}

// TestVerify_HookDirectoryOnHeadDoesNotReachTheRun holds invariant 5 for the
// hooks: they come from base, so a branch cannot switch its own checks on. The
// two defences stack, and the second one is why the run also refuses the diff:
// .redfirst/** sits in paths.protected.
func TestVerify_HookDirectoryOnHeadDoesNotReachTheRun(t *testing.T) {
	t.Parallel()

	repo := testkit.CleanFix(t)
	repo.WriteScript(".redfirst/env-up.sh", "#!/bin/sh\n")
	repo.WriteScript(".redfirst/test.sh", "#!/bin/sh\n")
	repo.WriteScript(".redfirst/env-down.sh", "#!/bin/sh\n")
	repo.Commit("chore: add hooks on the branch under judgement")

	var out bytes.Buffer
	args := []string{
		"verify", "--repo", repo.Dir,
		"--base", testkit.FixtureBase, "--head", testkit.FixtureHead, "--report", "json",
	}
	err := run(context.Background(), args, &out, io.Discard)
	if got := domain.ExitCode(err); got != 1 {
		t.Fatalf("exit code = %d, want 1: the diff writes into paths.protected", got)
	}

	var rep domain.Report
	if jsonErr := json.Unmarshal(out.Bytes(), &rep); jsonErr != nil {
		t.Fatalf("parse report: %v\n%s", jsonErr, out.String())
	}
	if rep.Tier != 1 {
		t.Errorf("tier = %d, want 1: the hooks are on head, and head does not judge", rep.Tier)
	}
	if got := gateByID(t, rep, domain.GateRedGreen); got.Status != domain.StatusUnavailable {
		t.Errorf("red-green is %q; a hook set the branch wrote may not switch a gate on", got.Status)
	}
}
