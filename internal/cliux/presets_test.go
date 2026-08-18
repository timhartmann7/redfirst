package cliux_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/cliux"
	"github.com/timhartmann7/redfirst/internal/gitx"
	"github.com/timhartmann7/redfirst/internal/harness"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

// written presets a repository and returns what landed, keyed by hook name.
func written(t *testing.T, root string, p cliux.Preset) map[string]string {
	t.Helper()

	got, paths, err := cliux.InitHooks(root, p)
	if err != nil {
		t.Fatalf("init --hooks %s: %v", p, err)
	}
	if p != cliux.PresetAuto && got != p {
		t.Fatalf("wrote the %s set for a request for %s", got, p)
	}

	files := make(map[string]string, len(paths))
	for _, rel := range paths {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		files[filepath.Base(rel)] = string(data)
	}
	return files
}

// gitRepo is the git adapter over a fixture, which is what doctor reads the
// base ref through.
func gitRepo(t *testing.T, dir string) *gitx.Repo {
	t.Helper()

	repo, err := gitx.Open(t.Context(), dir)
	if err != nil {
		t.Fatalf("open %s: %v", dir, err)
	}
	return repo
}

// templates is every preset that names a template. PresetAuto names none.
func templates() []cliux.Preset {
	return slices.DeleteFunc(cliux.Presets(), func(p cliux.Preset) bool { return p == cliux.PresetAuto })
}

// TestInitHooks_EveryPresetWritesARunnableHookSet is the acceptance criterion
// of the templates: a hook that will not parse, or that arrives without its
// executable bit, is a broken harness rather than a red test, and a project
// finds that out at the moment a verdict depends on it.
func TestInitHooks_EveryPresetWritesARunnableHookSet(t *testing.T) {
	t.Parallel()

	for _, p := range templates() {
		t.Run(string(p), func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			files := written(t, root, p)

			for _, required := range []string{"env-up.sh", "test.sh", "env-down.sh"} {
				if _, ok := files[required]; !ok {
					t.Errorf("the %s set carries no %s, and tier 2 runs nothing without it", p, required)
				}
			}
			for name := range files {
				full := filepath.Join(root, harness.HooksDir, name)
				info, err := os.Stat(full)
				if err != nil {
					t.Fatalf("stat %s: %v", name, err)
				}
				if !strings.HasSuffix(name, ".sh") {
					// Data a hook reads rather than a hook. Nothing executes it.
					if info.Mode().Perm()&0o111 != 0 {
						t.Errorf("%s is executable and is not a script", name)
					}
					continue
				}
				if info.Mode().Perm()&0o111 == 0 {
					t.Errorf("%s is not executable: %v", name, info.Mode().Perm())
				}
				if out, err := exec.CommandContext(t.Context(), "sh", "-n", full).CombinedOutput(); err != nil {
					t.Errorf("%s does not parse as a shell script: %v\n%s", name, err, out)
				}
			}
		})
	}
}

// TestInitHooks_EveryTestHookObeysTheHookContract holds the requirements of
// section 7 that decide whether a verdict is worth anything. A generated hook
// that broke one of them would ship the exact defect doctor exists to find.
func TestInitHooks_EveryTestHookObeysTheHookContract(t *testing.T) {
	t.Parallel()

	tests := []struct {
		preset cliux.Preset
		// staged says whether the preset has work to do once per phase. go test
		// builds as it runs and caches modules outside the working copy, so it
		// has none; leaving the branch out is what lets doctor read the rerun
		// timings for the one stack where a replayed verdict is the default.
		staged bool
	}{
		{cliux.PresetNodeUnit, true},
		{cliux.PresetNodePostgres, true},
		{cliux.PresetPythonPytest, true},
		{cliux.PresetGo, false},
		{cliux.PresetBlank, false},
	}

	for _, tc := range tests {
		t.Run(string(tc.preset), func(t *testing.T) {
			t.Parallel()

			test := written(t, t.TempDir(), tc.preset)["test.sh"]
			for _, want := range []string{"REDFIRST_FILTER", "REDFIRST_RESULTS"} {
				if !strings.Contains(test, want) {
					t.Errorf("the %s test.sh never mentions $%s:\n%s", tc.preset, want, test)
				}
			}
			staged := strings.Contains(test, `if [ "$REDFIRST_PROBE_INDEX" = 1 ]`)
			if staged != tc.staged {
				t.Errorf("the %s test.sh stages its first run: %v, want %v:\n%s",
					tc.preset, staged, tc.staged, test)
			}
		})
	}
}

// TestInitHooks_TheHarnessAcceptsWhatInitWrote closes the loop between the two
// halves of tier 2: the generator and the runner agree on the file names, the
// executable bit and the shebang.
//
// node-postgres-docker stays out: its services are the point of it, and a
// machine without docker would be testing the machine.
func TestInitHooks_TheHarnessAcceptsWhatInitWrote(t *testing.T) {
	t.Parallel()

	for _, p := range templates() {
		if p == cliux.PresetNodePostgres {
			continue
		}
		t.Run(string(p), func(t *testing.T) {
			t.Parallel()

			repo := testkit.NewRepo(t)
			if _, _, err := cliux.InitHooks(repo.Dir, p); err != nil {
				t.Fatalf("init --hooks %s: %v", p, err)
			}
			repo.Commit("chore: add the redfirst hooks")

			session, err := harness.Open(t.Context(), harness.Options{
				Source: gitRepo(t, repo.Dir), Base: testkit.FixtureBase, WorkDir: t.TempDir(),
			})
			if err != nil {
				t.Fatalf("the harness refuses the %s set: %v", p, err)
			}
			if err := session.Close(t.Context()); err != nil {
				t.Errorf("close the session: %v", err)
			}
		})
	}
}

// TestInitHooks_BlankTestShRefusesRatherThanReportGreen keeps the stub from
// acquitting a diff nobody tested. Exit 0 out of an unfilled hook would read as
// a suite that passed.
func TestInitHooks_BlankTestShRefusesRatherThanReportGreen(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	written(t, root, cliux.PresetBlank)

	cmd := exec.CommandContext(t.Context(), filepath.Join(root, harness.HooksDir, "test.sh"))
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("the blank test.sh exited zero, which reads as a green suite:\n%s", out)
	}
	if !strings.Contains(string(out), "has not been filled in") {
		t.Errorf("the blank test.sh does not say why it refused:\n%s", out)
	}
}

// TestInitHooks_PicksThePresetFromTheMarkers is the auto path: somebody who
// names no preset gets one, and gets told which.
func TestInitHooks_PicksThePresetFromTheMarkers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		markers map[string]string
		want    cliux.Preset
	}{
		{"node alone", map[string]string{"package.json": "{}"}, cliux.PresetNodeUnit},
		{
			name:    "node beside a compose file",
			markers: map[string]string{"package.json": "{}", "compose.yaml": "services:\n"},
			want:    cliux.PresetNodePostgres,
		},
		{"python", map[string]string{"pyproject.toml": "[project]\n"}, cliux.PresetPythonPytest},
		{"go", map[string]string{"go.mod": "module example.com/x\n"}, cliux.PresetGo},
		{"nothing recognised", map[string]string{"Makefile": "all:\n"}, cliux.PresetBlank},
		{
			name:    "a compose file with no stack behind it",
			markers: map[string]string{"docker-compose.yml": "services:\n"},
			want:    cliux.PresetBlank,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			for name, body := range tc.markers {
				if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
					t.Fatalf("write %s: %v", name, err)
				}
			}

			got, _, err := cliux.InitHooks(root, cliux.PresetAuto)
			if err != nil {
				t.Fatalf("init --hooks auto: %v", err)
			}
			if got != tc.want {
				t.Errorf("picked %s, want %s", got, tc.want)
			}
		})
	}
}

// TestInitHooks_RefusesToGuessBetweenTwoStacks keeps a monorepo from being
// handed the test.sh of the wrong half of itself. Deleting generated files is a
// worse first minute than answering one question.
func TestInitHooks_RefusesToGuessBetweenTwoStacks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for name, body := range map[string]string{"package.json": "{}", "go.mod": "module example.com/x\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	_, _, err := cliux.InitHooks(root, cliux.PresetAuto)
	if err == nil {
		t.Fatal("two stacks were resolved into one preset")
	}
	if !strings.Contains(err.Error(), "--hooks <preset>") {
		t.Errorf("the refusal does not say how to answer it: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(root, harness.HooksDir)); len(entries) != 0 {
		t.Errorf("a refused preset still wrote %d files", len(entries))
	}
}

// TestInitHooks_WritesNothingWhenOneOfTheSetIsInTheWay keeps half a hook set
// out of a repository. The contract has no name for that state.
func TestInitHooks_WritesNothingWhenOneOfTheSetIsInTheWay(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, harness.HooksDir), 0o755); err != nil {
		t.Fatalf("create the hook directory: %v", err)
	}
	existing := filepath.Join(root, harness.HooksDir, "env-down.sh")
	if err := os.WriteFile(existing, []byte("#!/bin/sh\necho mine\n"), 0o755); err != nil {
		t.Fatalf("write env-down.sh: %v", err)
	}

	_, _, err := cliux.InitHooks(root, cliux.PresetGo)
	if err == nil {
		t.Fatal("the generator wrote over a hook somebody else had written")
	}

	entries, readErr := os.ReadDir(filepath.Join(root, harness.HooksDir))
	if readErr != nil {
		t.Fatalf("read the hook directory: %v", readErr)
	}
	if len(entries) != 1 {
		t.Errorf("the refusal left %d files behind, want only the one that was there", len(entries))
	}
	if got := string(mustRead(t, existing)); !strings.Contains(got, "echo mine") {
		t.Errorf("env-down.sh was overwritten:\n%s", got)
	}
}

func TestInitHooks_RejectsAPresetItHasNoTemplateFor(t *testing.T) {
	t.Parallel()

	_, _, err := cliux.InitHooks(t.TempDir(), cliux.Preset("rust-cargo"))
	if err == nil {
		t.Fatal("an unknown preset was accepted")
	}
	if !strings.Contains(err.Error(), cliux.PresetNames()) {
		t.Errorf("the refusal does not list what is on offer: %v", err)
	}
}

// TestInitHooks_TemplatesMatchTheGolden pins the generated scripts. They are
// the product of this command, and a change to one has to be a decision.
func TestInitHooks_TemplatesMatchTheGolden(t *testing.T) {
	t.Parallel()

	for _, p := range templates() {
		t.Run(string(p), func(t *testing.T) {
			t.Parallel()

			files := written(t, t.TempDir(), p)
			names := make([]string, 0, len(files))
			for name := range files {
				names = append(names, name)
			}
			slices.Sort(names)

			var b strings.Builder
			for _, name := range names {
				b.WriteString("--- " + harness.HooksDir + "/" + name + " ---\n")
				b.WriteString(files[name])
			}
			testkit.Golden(t, "hooks-"+string(p)+".txt", []byte(b.String()))
		})
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}
