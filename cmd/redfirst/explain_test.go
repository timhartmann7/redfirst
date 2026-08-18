package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

const baseConfig = `version = 1

[budget]
max_files = 3
`

const headConfig = `version = 1

[budget]
max_files = 999
`

func explainOutput(t *testing.T, repoDir string, extra ...string) string {
	t.Helper()

	args := append([]string{"explain", "--repo", repoDir, "--base", testkit.FixtureBase}, extra...)

	var out bytes.Buffer
	if err := run(context.Background(), args, &out, io.Discard); err != nil {
		t.Fatalf("explain: %v", err)
	}
	return out.String()
}

// TestExplain_ReadsTheConfigOfTheBaseRef is invariant 5 seen from the command
// that exists to settle arguments: printing the config the agent wrote on head
// would describe rules nothing applies.
func TestExplain_ReadsTheConfigOfTheBaseRef(t *testing.T) {
	t.Parallel()

	r := testkit.NewRepo(t)
	r.Write("redfirst.toml", baseConfig)
	r.Write("src/total.js", "export const total = 0\n")
	r.Commit("feat: seed the tree")

	r.Branch(testkit.FixtureHead)
	r.Write("redfirst.toml", headConfig)
	r.Commit("chore: widen the budget")

	got := explainOutput(t, r.Dir)
	if !strings.Contains(got, "budget.max_files                   3") {
		t.Errorf("the budget of the base ref is missing:\n%s", got)
	}
	if strings.Contains(got, "999") {
		t.Errorf("explain printed the config head carries:\n%s", got)
	}
	if !strings.Contains(got, "config=base:redfirst.toml") {
		t.Errorf("the header does not name the config it read:\n%s", got)
	}
}

// TestExplain_RunsNothingAndWritesNothing holds the two promises of the command
// at once: no environment comes up and the repository keeps every byte it had.
func TestExplain_RunsNothingAndWritesNothing(t *testing.T) {
	t.Parallel()

	r := testkit.HookedFix(t)
	before := r.Git("status", "--porcelain=v1", "--untracked-files=all")

	if got := explainOutput(t, r.Dir, "--path", "src/total.test.js"); !strings.Contains(got, "tests.patterns") {
		t.Errorf("the path view is missing its rules:\n%s", got)
	}
	if after := r.Git("status", "--porcelain=v1", "--untracked-files=all"); after != before {
		t.Errorf("the working tree changed: %q, was %q", after, before)
	}
}

// TestExplain_RejectsAConfigTheVerdictWouldRejectToo keeps the two commands on
// the same rules: a broken config is exit code 3 wherever it is read.
func TestExplain_RejectsAConfigTheVerdictWouldRejectToo(t *testing.T) {
	t.Parallel()

	r := testkit.NewRepo(t)
	r.Write("redfirst.toml", "version = 1\n\n[paths]\nprotected_paths = []\n")
	r.Commit("chore: misspell a key")

	err := run(context.Background(), []string{"explain", "--repo", r.Dir, "--base", testkit.FixtureBase},
		io.Discard, io.Discard)
	if err == nil {
		t.Fatal("a config with an unknown key explained itself")
	}
	if got := domain.ExitCode(err); got != 3 {
		t.Errorf("exit code %d for %v, want 3", got, err)
	}
}
