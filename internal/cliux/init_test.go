package cliux_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/cliux"
	"github.com/timhartmann7/redfirst/internal/config"
	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

// pinned is a release standing in for a published one. The constants a real
// binary carries are filled by the release step, and a test that waited for
// them would check nothing until the first tag.
var pinned = cliux.Release{
	Version: "v1.4.2",
	SHA256:  "9c1f4ade7b0c2d5e8f36a1b47c90de23f581aa6470b3ec19d2f8c05a7e46bd31",
}

func read(t *testing.T, root, rel string) string {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(content)
}

// TestInit_WorkflowPinsTheVersionAndTheChecksum is the acceptance criterion of
// the generator: a workflow that names a version without a checksum lets a
// moved tag decide what judges the repository.
func TestInit_WorkflowPinsTheVersionAndTheChecksum(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path, err := cliux.InitCI(root, pinned)
	if err != nil {
		t.Fatalf("init --ci: %v", err)
	}
	if path != cliux.WorkflowPath {
		t.Errorf("wrote %q, want %q", path, cliux.WorkflowPath)
	}

	got := read(t, root, cliux.WorkflowPath)
	for _, want := range []string{
		"REDFIRST_VERSION: " + pinned.Version,
		"REDFIRST_SHA256: " + pinned.SHA256,
		"sha256sum --check --strict",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the workflow carries no %q:\n%s", want, got)
		}
	}
	testkit.Golden(t, "init-workflow.yml", []byte(got))
}

// TestInit_RefusesAReleaseItCannotPin keeps the weaker file from being written
// quietly. Ambiguity resolves toward the refusal, never toward the check that
// still runs but proves less.
func TestInit_RefusesAReleaseItCannotPin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		release cliux.Release
	}{
		{name: "a build that predates the first release", release: cliux.Release{}},
		{name: "a version with no checksum", release: cliux.Release{Version: "v1.4.2"}},
		{name: "a checksum with no version", release: cliux.Release{SHA256: pinned.SHA256}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			if _, err := cliux.InitCI(root, tc.release); err == nil {
				t.Fatal("an unpinnable release generated a workflow")
			}
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(cliux.WorkflowPath))); err == nil {
				t.Error("the refusal left a workflow behind")
			}
		})
	}
}

// TestInit_ConfigCarriesTheStrictSetForAgentBranches checks the generated file
// where it counts: after it has been read back the way a run reads it.
func TestInit_ConfigCarriesTheStrictSetForAgentBranches(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if _, err := cliux.InitConfig(root); err != nil {
		t.Fatalf("init --config: %v", err)
	}
	generated := read(t, root, cliux.ConfigPath)
	testkit.Golden(t, "init-config.toml", []byte(generated))

	cfg, source, err := config.Load(t.Context(), source{content: generated}, "base", cliux.ConfigPath)
	if err != nil {
		t.Fatalf("the generated config does not load: %v", err)
	}
	if source != "base:"+cliux.ConfigPath {
		t.Errorf("config source = %q", source)
	}

	if want := (domain.Budget{MaxFiles: 8, MaxAddedLines: 300, MaxDeletedLines: 120}); cfg.Budget != want {
		t.Errorf("budget = %+v, want %+v", cfg.Budget, want)
	}
	if cfg.Tests.Immutability != domain.ImmutabilityCases {
		t.Errorf("tests.immutability = %q, want %q", cfg.Tests.Immutability, domain.ImmutabilityCases)
	}
	if cfg.Tests.FixturesImmutability != domain.ImmutabilityStrict {
		t.Errorf("tests.fixtures_immutability = %q, want %q",
			cfg.Tests.FixturesImmutability, domain.ImmutabilityStrict)
	}
	if !cfg.Tests.RequireNew {
		t.Error("tests.require_new is false, the template judges an agent branch")
	}
	if got := cfg.SeverityOf(domain.GateDiffBudget); got != domain.SeverityFail {
		t.Errorf("severity.diff-budget = %q, want %q", got, domain.SeverityFail)
	}
}

// TestInit_ConfigKeepsTheBuiltInLists holds the other half of "generated from
// the built-in defaults": an escape that mangles a pattern would leave a
// denylist with a hole in it and a config that still loads.
func TestInit_ConfigKeepsTheBuiltInLists(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if _, err := cliux.InitConfig(root); err != nil {
		t.Fatalf("init --config: %v", err)
	}
	cfg, _, err := config.Load(t.Context(), source{content: read(t, root, cliux.ConfigPath)}, "base", cliux.ConfigPath)
	if err != nil {
		t.Fatalf("the generated config does not load: %v", err)
	}
	defaults := config.Defaults()

	tests := []struct {
		name      string
		got, want []string
	}{
		{name: "paths.protected", got: cfg.Paths.Protected.Raw(), want: defaults.Paths.Protected.Raw()},
		{name: "paths.guarded", got: cfg.Paths.Guarded.Raw(), want: defaults.Paths.Guarded.Raw()},
		{name: "tests.patterns", got: cfg.Tests.Patterns.Raw(), want: defaults.Tests.Patterns.Raw()},
		{name: "tests.fixtures", got: cfg.Tests.Fixtures.Raw(), want: defaults.Tests.Fixtures.Raw()},
		{
			name: "tests.forbid_disabled",
			got:  cfg.Tests.ForbidDisabled.Raw(), want: defaults.Tests.ForbidDisabled.Raw(),
		},
		{
			name: "suppression.patterns",
			got:  cfg.Suppression.Patterns.Raw(), want: defaults.Suppression.Patterns.Raw(),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if !slices.Equal(tc.got, tc.want) {
				t.Errorf("%s = %q, want the built-in list %q", tc.name, tc.got, tc.want)
			}
		})
	}
}

// TestInit_WritesTheSameBytesTwice is determinism where a reader can see it:
// two repositories set up from one binary carry identical files.
func TestInit_WritesTheSameBytesTwice(t *testing.T) {
	t.Parallel()

	first, second := t.TempDir(), t.TempDir()
	for _, root := range []string{first, second} {
		if _, err := cliux.InitCI(root, pinned); err != nil {
			t.Fatalf("init --ci: %v", err)
		}
		if _, err := cliux.InitConfig(root); err != nil {
			t.Fatalf("init --config: %v", err)
		}
	}
	for _, rel := range []string{cliux.WorkflowPath, cliux.ConfigPath} {
		if read(t, first, rel) != read(t, second, rel) {
			t.Errorf("two runs generated different %s", rel)
		}
	}
}

// TestInit_RefusesToOverwriteWhatIsAlreadyThere keeps a second run from
// throwing away the edits somebody made to the first one.
func TestInit_RefusesToOverwriteWhatIsAlreadyThere(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rel  string
		gen  func(root string) (string, error)
	}{
		{
			name: "the workflow",
			rel:  cliux.WorkflowPath,
			gen:  func(root string) (string, error) { return cliux.InitCI(root, pinned) },
		},
		{name: "the config", rel: cliux.ConfigPath, gen: cliux.InitConfig},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			if _, err := tc.gen(root); err != nil {
				t.Fatalf("first run: %v", err)
			}
			mine := "# mine\n"
			if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(tc.rel)), []byte(mine), 0o644); err != nil {
				t.Fatalf("write %s: %v", tc.rel, err)
			}

			if _, err := tc.gen(root); err == nil {
				t.Fatal("the second run overwrote the file")
			}
			if got := read(t, root, tc.rel); got != mine {
				t.Errorf("%s = %q, want the file left alone", tc.rel, got)
			}
		})
	}
}

// source stands in for the base ref: config.Load reads through it, so the
// generated file gets read exactly the way a run reads one.
type source struct{ content string }

func (s source) Exists(context.Context, string, string) (bool, error) { return true, nil }

func (s source) ShowFile(context.Context, string, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(s.content)), nil
}
