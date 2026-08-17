package paths_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/config"
	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/gates/paths"
	"github.com/timhartmann7/redfirst/internal/gitx"
	"github.com/timhartmann7/redfirst/internal/runner"
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

// oversizedFiles is what the branch touches by hand: four files past the
// default limit of twenty, so the refusal cannot come from anything else.
const oversizedFiles = 24

// bulk repeats a line n times.
func bulk(line string, n int) string {
	return strings.Repeat(line+"\n", n)
}

// bulkLines makes one file longer than the whole added-line budget, so the
// generated file and the test file each break that limit on their own.
// Counting either would add an overrun beside the file count, and the fixture
// test allows exactly one.
func bulkLines() int { return config.Defaults().Budget.MaxAddedLines + 100 }

// oversizedDiff is the fixture of the same name: a branch well past the file
// budget, carrying a regenerated file and a new test file besides. Base marks
// the generated tree in .gitattributes, and invariant 5 keeps that mark out of
// the agent's reach.
func oversizedDiff(t *testing.T) *testkit.Repo {
	t.Helper()

	r := testkit.NewRepo(t)
	r.Write(".gitattributes", "generated/** linguist-generated\n")
	r.Write("generated/schema.js", bulk("export const field = 0", 10))
	for i := range oversizedFiles {
		r.Write(fmt.Sprintf("src/mod%02d.js", i), fmt.Sprintf("export const mod = %d\n", i))
	}
	r.Commit("feat: seed the module tree")

	r.Branch(testkit.FixtureHead)
	for i := range oversizedFiles {
		r.Write(fmt.Sprintf("src/mod%02d.js", i), fmt.Sprintf("export const mod = %d\nexport const patched = true\n", i))
	}
	r.Write("generated/schema.js", bulk("export const field = 1", bulkLines()))
	r.Write("src/mod00.test.js", bulk("test('mod', () => {})", bulkLines()))
	r.Commit("feat: patch every module")

	return r
}

// TestDiffBudget_OversizedDiffFixtureCountsNeitherGeneratedNorTestFiles drives
// the gate from a real repository: the generated mark comes from
// .gitattributes at the merge base rather than from a field a test filled in.
// Each excluded file is longer than the whole added-line budget, so counting
// either shows up twice over: as a file the count should not hold and as an
// overrun on a limit the assertion below allows nobody to break.
func TestDiffBudget_OversizedDiffFixtureCountsNeitherGeneratedNorTestFiles(t *testing.T) {
	t.Parallel()

	cfg := config.Defaults()
	diff := fixtureDiff(t, oversizedDiff(t))
	assertGenerated(t, diff, "generated/schema.js")

	res := runGateOnDiff(t, paths.NewDiffBudget(cfg), cfg, diff)

	if res.Status != domain.StatusFail {
		t.Fatalf("status = %q, want a refusal on %d files: %s", res.Status, oversizedFiles, res.Message)
	}
	wantMessage := fmt.Sprintf("%d/%d files", oversizedFiles, cfg.Budget.MaxFiles)
	if !strings.HasPrefix(res.Message, wantMessage) {
		t.Errorf("message = %q, want it to open with %q", res.Message, wantMessage)
	}
	want := []string{fmt.Sprintf("%d files, the budget allows %d", oversizedFiles, cfg.Budget.MaxFiles)}
	assertOverrun(t, res, want)
}

// assertGenerated keeps the fixture honest: without the mark from base the
// case would be testing an ordinary file with a long diff.
func assertGenerated(t *testing.T, d domain.Diff, path string) {
	t.Helper()

	for _, f := range d.Files {
		if f.Path == path {
			if !f.Generated {
				t.Fatalf("%s is not marked linguist-generated", path)
			}
			return
		}
	}
	t.Fatalf("the fixture diff holds no %s", path)
}

// guardedOnly is the fixture of the same name: the branch touches nothing but
// guarded paths. A human who bumped the dependencies gets a line and a merge,
// not a red cross.
func guardedOnly(t *testing.T) *testkit.Repo {
	t.Helper()

	r := testkit.NewRepo(t)
	r.Write("src/total.js", fixtureSource)
	r.Write("pnpm-lock.yaml", "lockfileVersion: 9\n")
	r.Write(".github/dependabot.yml", "version: 2\n")
	r.Commit("chore: pin the dependencies")

	r.Branch(testkit.FixtureHead)
	r.Write("pnpm-lock.yaml", "lockfileVersion: 9\nupdated: true\n")
	r.Write(".github/dependabot.yml", "version: 2\nschedule: weekly\n")
	r.Commit("chore: bump the dependencies")

	return r
}

// TestPathGates_GuardedOnlyFixtureWarnsAndStillExitsZero carries the fixture's
// whole claim: the run reports both guarded files and the verdict stays at
// zero. It reads the exit code through the runner rather than the gate result,
// because "verdict 0" is a property of the run and not of one line in it.
func TestPathGates_GuardedOnlyFixtureWarnsAndStillExitsZero(t *testing.T) {
	t.Parallel()

	cfg := config.Defaults()
	diff := fixtureDiff(t, guardedOnly(t))

	protected := runGateOnDiff(t, paths.NewProtectedPaths(cfg), cfg, diff)
	budget := runGateOnDiff(t, paths.NewDiffBudget(cfg), cfg, diff)

	results := []domain.GateResult{protected, budget}
	if code := domain.ExitCode(runner.Outcome(results)); code != 0 {
		t.Errorf("exit code = %d, want 0: a guarded path is a line for a reviewer, not a refusal", code)
	}

	got := make([]string, 0, len(protected.Warnings))
	for _, w := range protected.Warnings {
		if w.Kind != domain.WarnGuardedPaths {
			t.Errorf("warning kind = %q, want %q", w.Kind, domain.WarnGuardedPaths)
		}
		got = append(got, w.File)
	}
	slices.Sort(got)
	want := []string{".github/dependabot.yml", "pnpm-lock.yaml"}
	if !slices.Equal(got, want) {
		t.Errorf("guarded files = %v, want %v", got, want)
	}
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
