package config_test

import (
	"slices"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
)

func TestConfig_RejectsVersionOtherThanOne(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
	}{
		{name: "a later version this binary cannot read", body: "version = 2\n"},
		{name: "version zero", body: "version = 0\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			loadRejects(t, tc.body)
		})
	}
}

// An absent version key is fine: Defaults() sets it to 1.
func TestConfig_AcceptsAbsentVersionKey(t *testing.T) {
	t.Parallel()

	if got := loadOK(t, "[budget]\nmax_files = 8\n").Version; got != 1 {
		t.Errorf("version = %d, want 1", got)
	}
}

func TestConfig_RejectsNonEmptyScopeList(t *testing.T) {
	t.Parallel()

	body := `[[scope]]
name = "payments"
match = ["packages/payments/**"]
hooks_dir = ".redfirst/payments"
budget = { max_files = 3, max_added_lines = 80 }
`
	loadRejects(t, body, "scopes are not implemented yet")
}

func TestConfig_RejectsLoweringRedGreenOrSuiteGreenToWarn(t *testing.T) {
	t.Parallel()

	rejected := []struct {
		name string
		body string
	}{
		{name: "red-green lowered to warn", body: "[severity]\nred-green = \"warn\"\n"},
		{name: "suite-green lowered to warn", body: "[severity]\nsuite-green = \"warn\"\n"},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			loadRejects(t, tc.body)
		})
	}

	accepted := []struct {
		name string
		body string
	}{
		{name: "red-green kept at fail", body: "[severity]\nred-green = \"fail\"\n"},
		{name: "another gate lowered to warn", body: "[severity]\nnew-test-present = \"warn\"\n"},
	}
	for _, tc := range accepted {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			loadOK(t, tc.body)
		})
	}
}

func TestConfig_RejectsSamePatternInProtectedAndGuarded(t *testing.T) {
	t.Parallel()

	body := "[paths]\nprotected = [\"src/**\", \"**/*.lock\"]\nguarded = [\"**/*.lock\"]\n"
	loadRejects(t, body, "**/*.lock")
}

func TestConfig_RejectsUnknownGateID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "in severity",
			body: "[severity]\nred_green = \"fail\"\n",
			want: "red_green",
		},
		{
			name: "in gates.disabled",
			body: "[gates]\ndisabled = [\"blast_radius\"]\n",
			want: "blast_radius",
		},
		{
			name: "a numeric code instead of a name",
			body: "[gates]\ndisabled = [\"G09\"]\n",
			want: "G09",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			loadRejects(t, tc.body, tc.want)
		})
	}
}

func TestConfig_RejectsInvalidGlob(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
	}{
		{name: "unclosed character class", body: "[paths]\nprotected = [\"src/[abc\"]\n"},
		{name: "unclosed alternation", body: "[tests]\npatterns = [\"src/{a,b\"]\n"},
		{name: "empty pattern", body: "[paths]\nguarded = [\"\"]\n"},
		{name: "a number where a glob belongs", body: "[paths]\nprotected = [7]\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			loadRejects(t, tc.body)
		})
	}
}

func TestConfig_RejectsInvalidRegex(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
	}{
		{name: "in tests.forbid_disabled", body: "[tests]\nforbid_disabled = [\"([a-\"]\n"},
		{name: "in suppression.patterns", body: "[suppression]\npatterns = [\"*ts-ignore\"]\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			loadRejects(t, tc.body)
		})
	}
}

func TestConfig_RejectsProbeRunsBelowOne(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
	}{
		{name: "zero runs would make red-green vacuous", body: "[red_green]\nprobe_runs = 0\n"},
		{name: "a negative count", body: "[red_green]\nprobe_runs = -1\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			loadRejects(t, tc.body)
		})
	}

	t.Run("a single run is legal", func(t *testing.T) {
		t.Parallel()
		if got := loadOK(t, "[red_green]\nprobe_runs = 1\n").RedGreen.ProbeRuns; got != 1 {
			t.Errorf("probe_runs = %d, want 1", got)
		}
	})
}

func TestConfig_RejectsWarnAsTestsImmutability(t *testing.T) {
	t.Parallel()

	t.Run("warn does not apply to test files", func(t *testing.T) {
		t.Parallel()
		loadRejects(t, "[tests]\nimmutability = \"warn\"\n")
	})

	t.Run("an unknown mode", func(t *testing.T) {
		t.Parallel()
		loadRejects(t, "[tests]\nimmutability = \"append_only\"\n")
	})

	t.Run("warn applies to the test surface", func(t *testing.T) {
		t.Parallel()
		cfg := loadOK(t, "[tests]\nfixtures_immutability = \"warn\"\n")
		if cfg.Tests.FixturesImmutability != domain.ImmutabilityWarn {
			t.Errorf("fixtures_immutability = %q, want %q", cfg.Tests.FixturesImmutability, domain.ImmutabilityWarn)
		}
	})
}

// The spec's full example is the config this loader exists to read.
func TestConfig_AcceptsTheSpecFullExample(t *testing.T) {
	t.Parallel()

	cfg := loadOK(t, specFullExample)

	if cfg.Budget.MaxFiles != 8 || cfg.Budget.MaxAddedLines != 300 || cfg.Budget.MaxDeletedLines != 120 {
		t.Errorf("budget = %+v, want 8/300/120", cfg.Budget)
	}
	if cfg.Tests.Immutability != domain.ImmutabilityCases {
		t.Errorf("immutability = %q, want %q", cfg.Tests.Immutability, domain.ImmutabilityCases)
	}
	if !cfg.Tests.RequireNew {
		t.Error("require_new = false, want true")
	}
	if !cfg.GateDisabled(domain.GateBlastRadius) {
		t.Error("blast-radius is enabled, want it disabled")
	}
	if got := cfg.SeverityOf(domain.GateDiffBudget); got != domain.SeverityFail {
		t.Errorf("diff-budget severity = %q, want %q", got, domain.SeverityFail)
	}
	if got, want := cfg.Tests.Fixtures.Raw(), []string{
		"**/__snapshots__/**", "**/*.snap", "**/fixtures/**", "vitest.config.*",
	}; !slices.Equal(got, want) {
		t.Errorf("fixtures = %v, want %v", got, want)
	}
}

const specFullExample = `version = 1

[budget]
max_files = 8
max_added_lines = 300
max_deleted_lines = 120

[paths]
protected = [
  ".redfirst/**",
  "redfirst.toml",
  ".github/workflows/redfirst.yml",
  "CLAUDE.md",
  "AGENTS.md",
  ".mcp.json",
  ".claude/**",
  "drizzle/migrations/**",
  "src/lib/server/auth/**",
  "src/lib/server/billing/**",
]
guarded = [
  ".github/**",
  "Dockerfile*",
  "docker-compose*.yml",
  "Caddyfile",
  "**/*.lock",
]
allowed = []

[tests]
patterns = ["**/*.test.ts", "**/*.spec.ts", "tests/**/*.ts"]
fixtures = ["**/__snapshots__/**", "**/*.snap", "**/fixtures/**", "vitest.config.*"]
immutability = "cases"
fixtures_immutability = "strict"
require_new = true
forbid_disabled = ["\\.skip\\(", "\\.only\\(", "\\bxit\\(", "\\bxdescribe\\("]
known_flaky = []

[red_green]
enabled = true
probe_runs = 3
compile_failure_on_base = "weak-red"

[suite]
retry_failed = 1

[suppression]
enabled = true
patterns = [
  "catch\\s*\\(\\s*\\w*\\s*\\)\\s*\\{\\s*\\}",
  "@ts-ignore",
  "@ts-expect-error",
  "eslint-disable",
  ":\\s*any\\b",
]

[severity]
protected-paths  = "fail"
diff-budget      = "fail"
test-immutability = "fail"
new-test-present = "fail"
no-disabled-tests = "fail"

[gates]
disabled = ["blast-radius"]

[scopes]
allow_cross_scope = true
`
