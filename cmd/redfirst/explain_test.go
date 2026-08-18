package main

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
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

// TestExplain_AnswersForThePathWhateverTheShellPassed covers the spellings a
// terminal produces. A rooted glob misses "./.claude/x" and misses an absolute
// path, and the answer that came back would be wrong rather than missing: the
// command exists to settle arguments, so a wrong "not protected" is the worst
// output it has.
func TestExplain_AnswersForThePathWhateverTheShellPassed(t *testing.T) {
	t.Parallel()

	r := testkit.CleanFix(t)
	root := r.Git("rev-parse", "--show-toplevel")

	tests := []struct {
		name string
		path string
	}{
		{name: "as the rules spell it", path: ".claude/settings.json"},
		{name: "as a shell completes a hidden directory", path: "./.claude/settings.json"},
		{name: "as an absolute path pastes", path: filepath.Join(root, ".claude/settings.json")},
		{name: "with a detour through a sibling directory", path: "src/../.claude/settings.json"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := explainOutput(t, r.Dir, "--path", tc.path)
			want := "paths.protected    yes · .claude/** · an edit here blocks the diff"
			if !strings.Contains(got, want) {
				t.Errorf("%q was not judged as %s:\n%s", tc.path, ".claude/**", got)
			}
		})
	}
}

// TestExplain_RefusesARelativePathTypedFromASubdirectory covers the spelling
// with two readings. From inside packages/api, "tests/x.ts" names one file to
// the shell and another to the rules, a rooted glob matches one and misses the
// other, and answering either would settle an argument with a coin toss.
//
// No t.Parallel: t.Chdir moves the whole process, which is the point.
func TestExplain_RefusesARelativePathTypedFromASubdirectory(t *testing.T) {
	r := testkit.CleanFix(t)
	t.Chdir(filepath.Join(r.Dir, "src"))

	err := run(context.Background(),
		[]string{"explain", "--repo", ".", "--base", testkit.FixtureBase, "--path", "total.test.js"},
		io.Discard, io.Discard)
	if err == nil {
		t.Fatal("a path with two readings got one answer")
	}
	for _, want := range []string{"src/total.test.js", "total.test.js", "absolute"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not carry %q", err, want)
		}
	}

	// An absolute path has one reading, so it still gets answered from here.
	got := explainOutput(t, ".", "--path", filepath.Join(r.Dir, "src/total.test.js"))
	if !strings.Contains(got, "tests.patterns     yes") {
		t.Errorf("an absolute path was not judged from the subdirectory:\n%s", got)
	}
}

// TestExplain_RefusesAPathOutsideTheRepository keeps the answer honest where it
// cannot be given: the rules describe one repository, and every list in them
// would report "no" for a file that is not in it.
func TestExplain_RefusesAPathOutsideTheRepository(t *testing.T) {
	t.Parallel()

	r := testkit.CleanFix(t)
	err := run(context.Background(),
		[]string{"explain", "--repo", r.Dir, "--base", testkit.FixtureBase, "--path", "../elsewhere/x.ts"},
		io.Discard, io.Discard)
	if err == nil {
		t.Fatal("a path outside the repository got an answer")
	}
	if !strings.Contains(err.Error(), "outside the repository") {
		t.Errorf("error %q does not say the path is not in the repository", err)
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
