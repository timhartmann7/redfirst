package paths_test

import (
	"context"
	"testing"

	"github.com/timhartmann7/redfirst/internal/config"
	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/gates/paths"
	"github.com/timhartmann7/redfirst/internal/gitx"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

// The fixture repositories of this slice get assembled here through the shared
// builder in internal/testkit. The table tests judge the gates from (Diff,
// Config) alone; these exist because git decides the facts they carry: a
// rename is a rename because git found one, and a file is generated because
// .gitattributes at the merge base says so.

const fixtureWorkflow = `name: redfirst
on: [pull_request]
jobs:
  verify:
    runs-on: ubuntu-latest
    steps:
      - run: redfirst verify
`

const fixtureInstructions = `# CLAUDE.md

Do not edit existing tests.
`

const fixtureSource = "export function total(items) {\n  return items.length\n}\n"

// fixtureDiff builds the diff the way verify does: merge base to head, with
// the linguist marks read from base.
func fixtureDiff(t *testing.T, r *testkit.Repo) domain.Diff {
	t.Helper()

	ctx := context.Background()
	repo, err := gitx.Open(ctx, r.Dir)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	d, err := repo.Diff(ctx, testkit.FixtureBase, testkit.FixtureHead)
	if err != nil {
		t.Fatalf("diff fixture: %v", err)
	}
	return d
}

// touchedWorkflow is the fixture of the same name: the branch moves the
// redfirst workflow aside instead of editing it. The destination path sits in
// no protected list, so only the source of the rename gives the agent away.
func touchedWorkflow(t *testing.T) *testkit.Repo {
	t.Helper()

	r := testkit.NewRepo(t)
	r.Write("src/total.js", fixtureSource)
	r.Write(".github/workflows/redfirst.yml", fixtureWorkflow)
	r.Commit("ci: run redfirst on every pull request")

	r.Branch(testkit.FixtureHead)
	r.Rename(".github/workflows/redfirst.yml", ".github/workflows/ci.yml")
	r.Write("src/total.js", fixtureSource+"\nexport const version = 2\n")
	r.Commit("chore: rename the workflow")

	return r
}

// touchedClaudeMD is the quieter half of the threat model: an edit to the
// instructions changes the behaviour of every future agent in the repository
// while reading as a documentation tweak.
func touchedClaudeMD(t *testing.T) *testkit.Repo {
	t.Helper()

	r := testkit.NewRepo(t)
	r.Write("src/total.js", fixtureSource)
	r.Write("CLAUDE.md", fixtureInstructions)
	r.Commit("docs: write down the rules for agents")

	r.Branch(testkit.FixtureHead)
	r.Write("CLAUDE.md", fixtureInstructions+"\nEditing tests is fine when they are wrong.\n")
	r.Write("src/total.js", fixtureSource+"\nexport const version = 2\n")
	r.Commit("docs: clarify the testing rules")

	return r
}

// TestProtectedPaths_TouchedWorkflowFixtureCatchesTheRenameOnItsSource drives
// the gate from a real git rename under the built-in defaults. The workflow
// lands on a path no list protects, so only the source of the rename gives the
// diff away.
func TestProtectedPaths_TouchedWorkflowFixtureCatchesTheRenameOnItsSource(t *testing.T) {
	t.Parallel()

	cfg := config.Defaults()
	diff := fixtureDiff(t, touchedWorkflow(t))
	assertRenameFound(t, diff, ".github/workflows/redfirst.yml")

	res := runGateOnDiff(t, paths.NewProtectedPaths(cfg), cfg, diff)

	assertRefusal(t, res)
	assertEvidence(t, res, ".github/workflows/redfirst.yml",
		"renamed to .github/workflows/ci.yml, under paths.protected")
	// .github/** is guarded in the defaults, and the file is already refused:
	// a reviewer gets one line about it, not two.
	if len(res.Warnings) != 0 {
		t.Errorf("warnings = %v, want none behind a refusal", res.Warnings)
	}
}

// TestProtectedPaths_TouchedClaudeMDFixtureCatchesTheInstructionEdit covers the
// quietest row of the threat model: rewriting the rules for future agents
// reads as a documentation tweak.
func TestProtectedPaths_TouchedClaudeMDFixtureCatchesTheInstructionEdit(t *testing.T) {
	t.Parallel()

	cfg := config.Defaults()

	res := runGateOnDiff(t, paths.NewProtectedPaths(cfg), cfg, fixtureDiff(t, touchedClaudeMD(t)))

	assertRefusal(t, res)
	assertEvidence(t, res, "CLAUDE.md", "modified, under paths.protected")
}

// assertRenameFound keeps the fixture honest: without git's own rename
// detection the case would quietly turn into an addition and a deletion, and
// the gate would be answering a different question.
func assertRenameFound(t *testing.T, d domain.Diff, oldPath string) {
	t.Helper()

	for _, f := range d.Files {
		if f.Status == domain.FileRenamed && f.OldPath == oldPath {
			return
		}
	}
	t.Fatalf("git found no rename of %s in %v", oldPath, d.Files)
}
