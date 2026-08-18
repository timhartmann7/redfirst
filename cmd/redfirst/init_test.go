package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/cliux"
	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

// TestInit_WritesAtTheTopLevelFromASubdirectory covers where the files land.
// A workflow generated into a subdirectory is a workflow no runner ever reads,
// and the command would look like it worked.
func TestInit_WritesAtTheTopLevelFromASubdirectory(t *testing.T) {
	t.Parallel()

	r := testkit.CleanFix(t)
	sub := filepath.Join(r.Dir, "src")

	var out bytes.Buffer
	if err := run(context.Background(), []string{"init", "--config", "--repo", sub}, &out, io.Discard); err != nil {
		t.Fatalf("init --config: %v", err)
	}
	if got := out.String(); got != "wrote "+cliux.ConfigPath+"\n" {
		t.Errorf("output = %q, want the path it wrote", got)
	}
	if _, err := os.Stat(filepath.Join(r.Dir, cliux.ConfigPath)); err != nil {
		t.Errorf("the config did not land at the top level: %v", err)
	}
}

// TestInit_TouchesNothingButWhatItGenerates holds invariant 6 for the one
// command that writes at all: it adds the file asked for and changes no other
// byte of the repository.
func TestInit_TouchesNothingButWhatItGenerates(t *testing.T) {
	t.Parallel()

	r := testkit.CleanFix(t)
	args := []string{"init", "--config", "--repo", r.Dir}
	if err := run(context.Background(), args, io.Discard, io.Discard); err != nil {
		t.Fatalf("init --config: %v", err)
	}

	got := r.Git("status", "--porcelain=v1", "--untracked-files=all")
	if got != "?? "+cliux.ConfigPath {
		t.Errorf("the repository changed as:\n%s\nwant one new %s", got, cliux.ConfigPath)
	}
}

// TestInit_RefusesAWorkflowThisBuildCannotPin is the shipped behaviour until
// the first release: the pin is what the workflow exists for, so a build with
// nothing to pin says so instead of generating a file that proves less.
func TestInit_RefusesAWorkflowThisBuildCannotPin(t *testing.T) {
	t.Parallel()

	if pin := cliux.PinnedRelease(); pin.Version != "" && pin.SHA256 != "" {
		t.Skip("this build carries a pinned release: " + pin.Version)
	}

	r := testkit.CleanFix(t)
	err := run(context.Background(), []string{"init", "--ci", "--repo", r.Dir}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("an unpinned build generated a workflow")
	}
	if !strings.Contains(err.Error(), "released binary") {
		t.Errorf("error %q does not say how to get a pinned workflow", err)
	}
	if got := r.Git("status", "--porcelain=v1", "--untracked-files=all"); got != "" {
		t.Errorf("the refusal left %q behind", got)
	}
}

// TestInit_TheGeneratedConfigJudgesTheNextDiff closes the loop the command
// exists for: the file it writes, committed to base, is the rule set the next
// verify run applies. The same diff that the built-in defaults let through
// stops against the strict set, which is the whole difference between judging a
// human pull request and judging an agent.
func TestInit_TheGeneratedConfigJudgesTheNextDiff(t *testing.T) {
	t.Parallel()

	r := testkit.NewRepo(t)
	r.Write("src/total.js", "export const total = 0\n")
	r.Write("src/total.test.js", "test('totals', () => {})\n")
	r.Commit("feat: seed the tree")

	r.Branch(testkit.FixtureHead)
	r.Write("src/total.js", "export const total = 1\n")
	r.Commit("fix: change the total without a test")

	underDefaults := runReport(t, r.Dir)
	if underDefaults.ConfigSource != "defaults" {
		t.Fatalf("config_source = %q before the config was generated", underDefaults.ConfigSource)
	}
	if got := gateByID(t, underDefaults, domain.GateNewTestPresent); got.Status != domain.StatusSkip {
		t.Fatalf("new-test-present is %q under the defaults, want skip", got.Status)
	}

	r.Checkout(testkit.FixtureBase)
	args := []string{"init", "--config", "--repo", r.Dir}
	if err := run(context.Background(), args, io.Discard, io.Discard); err != nil {
		t.Fatalf("init --config: %v", err)
	}
	r.Commit("chore: judge this repository by the generated rules")
	r.Checkout(testkit.FixtureHead)

	rep, err := reportOfRefusal(t, r.Dir)
	if domain.ExitCode(err) != 1 {
		t.Fatalf("exit code %d for %v, want 1: the diff carries no new test", domain.ExitCode(err), err)
	}
	if rep.ConfigSource != "base:"+cliux.ConfigPath {
		t.Errorf("config_source = %q, want the generated config", rep.ConfigSource)
	}
	if got := gateByID(t, rep, domain.GateNewTestPresent); got.Status != domain.StatusFail {
		t.Errorf("new-test-present is %q under the generated config, want fail", got.Status)
	}
}

// reportOfRefusal runs verify and keeps both halves of the answer: a refusal is
// an error and a report at once, and this test reads the report.
func reportOfRefusal(t *testing.T, repoDir string) (domain.Report, error) {
	t.Helper()

	args := []string{
		"verify", "--repo", repoDir,
		"--base", testkit.FixtureBase, "--head", testkit.FixtureHead,
		"--report", "json",
	}
	var out bytes.Buffer
	err := run(context.Background(), args, &out, io.Discard)

	var rep domain.Report
	if jsonErr := json.Unmarshal(out.Bytes(), &rep); jsonErr != nil {
		t.Fatalf("parse report: %v\n%s", jsonErr, out.String())
	}
	return rep, err
}

// TestInit_NeedsSomethingToGenerate keeps a bare `init` from looking like it
// did something.
func TestInit_NeedsSomethingToGenerate(t *testing.T) {
	t.Parallel()

	r := testkit.CleanFix(t)
	var stdout bytes.Buffer
	err := run(context.Background(), []string{"init", "--repo", r.Dir}, &stdout, io.Discard)
	if err == nil {
		t.Fatal("init with no flag reported success")
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want nothing written", stdout.String())
	}
}
