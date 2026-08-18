package config_test

import (
	"slices"
	"testing"

	"github.com/timhartmann7/redfirst/internal/config"
	"github.com/timhartmann7/redfirst/internal/domain"
)

func TestDefaults_ListsTranscribeTheSpec(t *testing.T) {
	t.Parallel()

	def := config.Defaults()
	cases := []struct {
		name string
		got  []string
		want []string
	}{
		{
			name: "paths.protected holds only files that change the rules",
			got:  def.Paths.Protected.Raw(),
			want: []string{
				".redfirst/**", "redfirst.toml", ".github/workflows/redfirst.yml",
				"CLAUDE.md", "AGENTS.md", ".mcp.json", ".claude/**", ".cursor/**",
			},
		},
		{
			name: "paths.guarded holds infrastructure, migrations and lockfiles",
			got:  def.Paths.Guarded.Raw(),
			want: []string{
				".github/**",
				"Dockerfile*", "docker-compose*.yml",
				"**/migrations/**",
				"**/*.lock", "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "go.sum",
			},
		},
		{
			name: "paths.allowed is empty, so no allowlist applies",
			got:  def.Paths.Allowed.Raw(),
			want: nil,
		},
		{
			name: "tests.patterns covers the five preset languages",
			got:  def.Tests.Patterns.Raw(),
			want: []string{
				"**/*.test.*", "**/*.spec.*", "**/*_test.go", "**/test_*.py",
				"tests/**", "spec/**", "__tests__/**",
			},
		},
		{
			name: "tests.fixtures covers the test surface",
			got:  def.Tests.Fixtures.Raw(),
			want: []string{
				"**/__snapshots__/**", "**/*.snap", "**/*.golden",
				"**/testdata/**", "**/fixtures/**", "**/__mocks__/**",
				"**/conftest.py", "**/setupTests.*", "**/*.setup.ts", "**/*.setup.js",
				"jest.config.*", "vitest.config.*", "playwright.config.*",
			},
		},
		{
			name: "tests.forbid_disabled catches a test disabled in the same breath",
			got:  def.Tests.ForbidDisabled.Raw(),
			want: []string{`\.skip\(`, `\.only\(`, `\bxit\(`, `\bxdescribe\(`},
		},
		{
			name: "tests.known_flaky starts empty",
			got:  def.Tests.KnownFlaky,
			want: nil,
		},
		{
			name: "suppression.patterns are the five from the spec",
			got:  def.Suppression.Patterns.Raw(),
			want: []string{
				`catch\s*\(\s*\w*\s*\)\s*\{\s*\}`,
				`@ts-ignore`,
				`@ts-expect-error`,
				`eslint-disable`,
				`:\s*any\b`,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !slices.Equal(tc.got, tc.want) {
				t.Errorf("list = %#v\nwant %#v", tc.got, tc.want)
			}
		})
	}
}

func TestDefaults_ScalarsTranscribeTheSpec(t *testing.T) {
	t.Parallel()

	def := config.Defaults()
	cases := []struct {
		name      string
		got, want any
	}{
		{name: "version", got: def.Version, want: 1},
		{name: "budget stays wide enough for a human PR", got: def.Budget, want: domain.Budget{
			MaxFiles: 20, MaxAddedLines: 800, MaxDeletedLines: 400,
		}},
		{
			name: "test files are append-only, not strict",
			got:  def.Tests.Immutability, want: domain.ImmutabilityAppendOnly,
		},
		{
			name: "the test surface only warns",
			got:  def.Tests.FixturesImmutability, want: domain.ImmutabilityWarn,
		},
		{name: "new-test-present is off", got: def.Tests.RequireNew, want: false},
		{name: "red-green is on", got: def.RedGreen.Enabled, want: true},
		{name: "probe_runs", got: def.RedGreen.ProbeRuns, want: 3},
		{
			name: "a test that fails to build on base counts as a flagged red",
			got:  def.RedGreen.CompileFailureOnBase, want: domain.CompileFailureWeakRed,
		},
		{name: "suite retries a failed test once", got: def.Suite.RetryFailed, want: 1},
		{name: "suppression-scan is on", got: def.Suppression.Enabled, want: true},
		{name: "no gate is disabled", got: len(def.Gates.Disabled), want: 0},
		{name: "cross-scope diffs are allowed", got: def.Scopes.AllowCrossScope, want: true},
		{name: "no scope is declared", got: len(def.Scope), want: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.got != tc.want {
				t.Errorf("got %v, want %v", tc.got, tc.want)
			}
		})
	}
}

func TestDefaults_SeverityLowersDiffBudgetAlone(t *testing.T) {
	t.Parallel()

	def := config.Defaults()
	if len(def.Severity) != 1 {
		t.Fatalf("severity = %v, want exactly one entry", def.Severity)
	}
	if got := def.SeverityOf(domain.GateDiffBudget); got != domain.SeverityWarn {
		t.Errorf("diff-budget = %q, want %q", got, domain.SeverityWarn)
	}
	for _, id := range domain.AllGateIDs() {
		if id == domain.GateDiffBudget {
			continue
		}
		if got := def.SeverityOf(id); got != domain.SeverityFail {
			t.Errorf("%s = %q, want %q", id, got, domain.SeverityFail)
		}
	}
}

// The defaults are as unswappable as the config from base, so one caller must
// not be able to edit them for the next.
func TestDefaults_AreFreshOnEveryCall(t *testing.T) {
	t.Parallel()

	first := config.Defaults()
	first.Severity[domain.GateRedGreen] = domain.SeverityWarn
	first.Gates.Disabled = append(first.Gates.Disabled, domain.GateProtectedPaths)

	second := config.Defaults()
	if got := second.SeverityOf(domain.GateRedGreen); got != domain.SeverityFail {
		t.Errorf("red-green severity = %q, want %q: the severity map leaked between calls", got, domain.SeverityFail)
	}
	if len(second.Gates.Disabled) != 0 {
		t.Errorf("gates.disabled = %v, want empty: the slice leaked between calls", second.Gates.Disabled)
	}
}

func TestDowngradeCasesWithoutHooks_LowersBothModesAndNamesTheKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		tests    domain.Immutability
		fixtures domain.Immutability
		want     []string
	}{
		{
			name:     "TestFilesAlone",
			tests:    domain.ImmutabilityCases,
			fixtures: domain.ImmutabilityWarn,
			want:     []string{"tests.immutability"},
		},
		{
			// Left at cases this key would take the whole gate to unavailable
			// for want of case names, which is a check switched off rather
			// than a rule made stricter.
			name:     "TestSurfaceAlone",
			tests:    domain.ImmutabilityAppendOnly,
			fixtures: domain.ImmutabilityCases,
			want:     []string{"tests.fixtures_immutability"},
		},
		{
			name:     "BothAtOnce",
			tests:    domain.ImmutabilityCases,
			fixtures: domain.ImmutabilityCases,
			want:     []string{"tests.immutability", "tests.fixtures_immutability"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := config.Defaults()
			cfg.Tests.Immutability, cfg.Tests.FixturesImmutability = tc.tests, tc.fixtures

			if got := config.DowngradeCasesWithoutHooks(&cfg); !slices.Equal(got, tc.want) {
				t.Errorf("downgraded %v, want %v", got, tc.want)
			}
			for _, got := range []domain.Immutability{cfg.Tests.Immutability, cfg.Tests.FixturesImmutability} {
				if got == domain.ImmutabilityCases {
					t.Errorf("mode stayed %q, want it lowered to %q", got, domain.ImmutabilityAppendOnly)
				}
			}
		})
	}
}

func TestDowngradeCasesWithoutHooks_LeavesEveryOtherModeAlone(t *testing.T) {
	t.Parallel()

	modes := []domain.Immutability{
		domain.ImmutabilityStrict,
		domain.ImmutabilityAppendOnly,
		domain.ImmutabilityWarn,
	}

	for _, mode := range modes {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			cfg := config.Defaults()
			cfg.Tests.Immutability, cfg.Tests.FixturesImmutability = mode, mode

			if got := config.DowngradeCasesWithoutHooks(&cfg); len(got) != 0 {
				t.Errorf("downgrade reported %v for %q, want none", got, mode)
			}
			if cfg.Tests.Immutability != mode || cfg.Tests.FixturesImmutability != mode {
				t.Errorf("modes = %q and %q, want %q on both",
					cfg.Tests.Immutability, cfg.Tests.FixturesImmutability, mode)
			}
		})
	}
}
