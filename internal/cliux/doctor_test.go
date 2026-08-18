package cliux_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/cliux"
	"github.com/timhartmann7/redfirst/internal/config"
	"github.com/timhartmann7/redfirst/internal/harness"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

// doctorFixture is the repository doctor gets pointed at. Everything it writes
// lands on the base ref unless the field says otherwise: the rules, the hooks
// and the workflow all decide nothing until they are there.
type doctorFixture struct {
	// tests are the test files the tree carries. Two of them, in two
	// directories, is the shape the filter check needs.
	tests []string
	// hook names the script under testdata/hooks that lands as test.sh. Empty
	// leaves the repository without a hook directory at all.
	hook string
	// up names the script that lands as env-up.sh. Empty takes the one that
	// brings nothing up and exits 0.
	up string
	// reset adds env-reset.sh, which is what picks the reused mode.
	reset bool
	// workflow puts the CI workflow on base, which is the whole of tier 1.
	workflow bool
	// config is redfirst.toml as base carries it, empty for none.
	config string
	// files are extra paths written before the commit: stack markers, a
	// migration, whatever the case is about.
	files map[string]string
	// uncommitted is written after the commit, so it reaches the working tree
	// and never the base ref.
	uncommitted map[string]string
}

func (f doctorFixture) build(t *testing.T) *testkit.Repo {
	t.Helper()

	r := testkit.NewRepo(t)
	r.Write("src/total.js", "export const total = () => 0\n")
	for _, path := range f.tests {
		r.Write(path, "test('"+path+"', () => {})\n")
	}
	for path, body := range f.files {
		r.Write(path, body)
	}
	if f.config != "" {
		r.Write(cliux.ConfigPath, f.config)
	}
	if f.workflow {
		r.Write(cliux.WorkflowPath, "name: redfirst\n")
	}
	if f.hook != "" {
		f.writeHooks(t, r, f.hook)
	}
	r.Commit("chore: seed the repository")

	for path, body := range f.uncommitted {
		r.Write(path, body)
	}
	return r
}

func (f doctorFixture) writeHooks(t *testing.T, r *testkit.Repo, test string) {
	t.Helper()

	up := f.up
	if up == "" {
		up = "env-up.sh"
	}
	r.WriteScript(harness.HooksDir+"/env-up.sh", script(t, up))
	r.WriteScript(harness.HooksDir+"/env-down.sh", script(t, "env-down.sh"))
	r.WriteScript(harness.HooksDir+"/test.sh", script(t, test))
	if f.reset {
		r.WriteScript(harness.HooksDir+"/env-reset.sh", script(t, "env-reset.sh"))
	}
}

// script reads one of the hooks under testdata. Each is broken in one way, and
// naming which way is what doctor exists for.
func script(t *testing.T, name string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", "hooks", name))
	if err != nil {
		t.Fatalf("read hook %s: %v", name, err)
	}
	return string(data)
}

// diagnose runs doctor over the fixture the way the CLI does and returns what
// it printed.
func (f doctorFixture) diagnose(t *testing.T, extra ...func(*cliux.Doctor)) string {
	t.Helper()

	repo := f.build(t)
	git := gitRepo(t, repo.Dir)
	base, err := git.Resolve(t.Context(), testkit.FixtureBase)
	if err != nil {
		t.Fatalf("resolve %s: %v", testkit.FixtureBase, err)
	}
	cfg, source, err := config.Load(t.Context(), git, base, cliux.ConfigPath)
	if err != nil {
		t.Fatalf("load the config: %v", err)
	}

	d := cliux.Doctor{
		Repo: git, BaseRef: testkit.FixtureBase, BaseSHA: base,
		Config: cfg, ConfigSource: source, WorkDir: t.TempDir(),
	}
	for _, apply := range extra {
		apply(&d)
	}

	var out strings.Builder
	if err := d.Report(t.Context(), &out); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	return out.String()
}

// noHooks is what --no-hooks does: answer from the repository alone.
func noHooks(d *cliux.Doctor) { d.NoHooks = true }

// wants fails unless every fragment is in the output. A diagnosis is judged by
// what it says, and a substring is what a reader would look for.
func wants(t *testing.T, got string, fragments ...string) {
	t.Helper()

	for _, want := range fragments {
		if !strings.Contains(got, want) {
			t.Errorf("the diagnosis does not carry %q:\n%s", want, got)
		}
	}
}

func rejects(t *testing.T, got string, fragments ...string) {
	t.Helper()

	for _, unwanted := range fragments {
		if strings.Contains(got, unwanted) {
			t.Errorf("the diagnosis carries %q and should not:\n%s", unwanted, got)
		}
	}
}

// TestDoctor_TierZeroNamesTheCommandThatReachesTierOne is the acceptance
// criterion of the whole command: not the fact that something is missing, but
// the line somebody can paste.
func TestDoctor_TierZeroNamesTheCommandThatReachesTierOne(t *testing.T) {
	t.Parallel()

	got := doctorFixture{tests: []string{"src/total.test.js"}}.diagnose(t)

	wants(t, got,
		"tier=0",
		"no "+cliux.WorkflowPath+" → nothing runs redfirst on a pull request",
		"not found → using built-in defaults",
		"not found → red-green, suite-green unavailable",
		"To reach tier 1:",
		"redfirst init --ci",
	)
}

// TestDoctor_TierOneNamesThePresetTheMarkersPicked covers the sample output in
// section 5 of the spec, down to the preset in the command.
func TestDoctor_TierOneNamesThePresetTheMarkersPicked(t *testing.T) {
	t.Parallel()

	got := doctorFixture{
		tests:    []string{"src/total.test.js"},
		workflow: true,
		files:    map[string]string{"package.json": `{"devDependencies":{"vitest":"^2.0.0"}}`},
	}.diagnose(t)

	wants(t, got,
		"tier=1",
		"base:"+cliux.WorkflowPath,
		"node + vitest, no docker-compose",
		"To reach tier 2:",
		"redfirst init --hooks node-unit",
	)
}

// TestDoctor_ComposeBesideNodePicksTheDockerPreset keeps the two node presets
// apart on the one marker that separates them.
func TestDoctor_ComposeBesideNodePicksTheDockerPreset(t *testing.T) {
	t.Parallel()

	got := doctorFixture{
		tests:    []string{"src/total.test.js"},
		workflow: true,
		files: map[string]string{
			"package.json":       `{"devDependencies":{"vitest":"^2.0.0"}}`,
			"docker-compose.yml": "services:\n",
		},
	}.diagnose(t)

	wants(t, got, "node + vitest, docker-compose", "redfirst init --hooks node-postgres-docker")
}

// TestDoctor_TwoStacksAskForAPresetRatherThanGuessing keeps a monorepo from
// being handed a test.sh for the wrong half of itself.
func TestDoctor_TwoStacksAskForAPresetRatherThanGuessing(t *testing.T) {
	t.Parallel()

	got := doctorFixture{
		tests:    []string{"src/total.test.js"},
		workflow: true,
		files: map[string]string{
			"package.json": `{"devDependencies":{"jest":"^29.0.0"}}`,
			"go.mod":       "module example.com/x\n",
		},
	}.diagnose(t)

	wants(t, got,
		"node and go + jest",
		"redfirst init --hooks <preset>",
		"markers for node and go",
	)
}

// TestDoctor_FilesInTheWorkingTreeOnlyDecideNothing is the trap right after
// `redfirst init`: everything that judges is read from base, and a hook set
// nobody merged looks identical on the filesystem to one that runs.
func TestDoctor_FilesInTheWorkingTreeOnlyDecideNothing(t *testing.T) {
	t.Parallel()

	got := doctorFixture{
		tests: []string{"src/total.test.js"},
		uncommitted: map[string]string{
			harness.HooksDir + "/test.sh": "#!/bin/sh\n",
			cliux.ConfigPath:              "version = 1\n",
			cliux.WorkflowPath:            "name: redfirst\n",
		},
	}.diagnose(t)

	wants(t, got,
		"tier=0",
		harness.HooksDir+"/ in the working tree, not on "+testkit.FixtureBase,
		cliux.ConfigPath+" in the working tree, not on "+testkit.FixtureBase,
		cliux.WorkflowPath+" in the working tree, not on "+testkit.FixtureBase,
	)
}

// TestDoctor_HooksOnBaseAreTierTwo is the other end of the same rule.
func TestDoctor_HooksOnBaseAreTierTwo(t *testing.T) {
	t.Parallel()

	got := doctorFixture{
		tests:    []string{"src/total.test.js", "lib/discount.test.js"},
		workflow: true,
		hook:     "test-honest.sh",
	}.diagnose(t)

	wants(t, got, "tier=2", "base:"+harness.HooksDir+"/ → red-green and suite-green run")
	// Tier 2 is the top of the ladder, and advice for a tier already reached
	// would read as though the diagnosis had found nothing.
	rejects(t, got, "To reach tier")
}

// TestDoctor_ConfigRowNamesWhereTheRulesCameFrom keeps the answer to "which
// rules judged me" in one place: config.Load decides it, doctor prints it.
func TestDoctor_ConfigRowNamesWhereTheRulesCameFrom(t *testing.T) {
	t.Parallel()

	got := doctorFixture{
		tests:  []string{"src/total.test.js"},
		config: "version = 1\n\n[budget]\nmax_files = 4\n",
	}.diagnose(t)

	wants(t, got, "base:"+cliux.ConfigPath)
}

// TestDoctor_NoHooksAnswersFromTheRepositoryAlone covers the flag somebody with
// an expensive environment reaches for. Nothing may run.
func TestDoctor_NoHooksAnswersFromTheRepositoryAlone(t *testing.T) {
	t.Parallel()

	got := doctorFixture{
		tests: []string{"src/total.test.js", "lib/discount.test.js"},
		hook:  "test-honest.sh",
	}.diagnose(t, noHooks)

	wants(t, got, "tier=2", "not probed · --no-hooks")
	rejects(t, got, "case names", "filter", "reruns")
}

// TestDoctor_TierOneOutputMatchesTheGolden pins the layout. Nothing here runs,
// so every byte of it is decided by the repository.
func TestDoctor_TierOneOutputMatchesTheGolden(t *testing.T) {
	t.Parallel()

	got := doctorFixture{
		tests:    []string{"src/total.test.js"},
		workflow: true,
		config:   "version = 1\n",
		files:    map[string]string{"package.json": `{"devDependencies":{"vitest":"^2.0.0"}}`},
	}.diagnose(t)

	testkit.Golden(t, "doctor-tier1.txt", []byte(got))
}
