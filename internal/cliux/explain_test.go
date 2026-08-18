package cliux_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/cliux"
	"github.com/timhartmann7/redfirst/internal/config"
	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

func explain(t *testing.T, cfg domain.Config, source, path string) string {
	t.Helper()

	var out bytes.Buffer
	if err := cliux.Explain(&out, cfg, source, path); err != nil {
		t.Fatalf("explain: %v", err)
	}
	return out.String()
}

// TestExplain_PrintsTheWholeEffectiveRuleSet pins the output against a golden
// file: a rule that stops being printed is a rule a reader cannot argue with,
// and the whole point of the command is that the set is complete.
func TestExplain_PrintsTheWholeEffectiveRuleSet(t *testing.T) {
	t.Parallel()

	testkit.Golden(t, "explain-defaults.txt", []byte(explain(t, config.Defaults(), config.SourceDefaults, "")))
}

// TestExplain_NamesTheSourceOfTheRules covers the first line: rules from a
// config on base and built-in ones lead to different arguments.
func TestExplain_NamesTheSourceOfTheRules(t *testing.T) {
	t.Parallel()

	got, _, _ := strings.Cut(explain(t, config.Defaults(), "base:redfirst.toml", ""), "\n")
	if !strings.Contains(got, "config=base:redfirst.toml") {
		t.Errorf("header = %q, want the config source in it", got)
	}
}

// TestExplain_PathNamesTheGlobThatDecided walks the rules one file can fall
// under. Every answer has to carry the pattern that produced it.
func TestExplain_PathNamesTheGlobThatDecided(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want []string
	}{
		{
			name: "an instruction file blocks the diff",
			path: "CLAUDE.md",
			want: []string{"paths.protected    yes · CLAUDE.md · an edit here blocks the diff"},
		},
		{
			name: "a lockfile only reaches the report",
			path: "pnpm-lock.yaml",
			want: []string{"paths.guarded      yes · pnpm-lock.yaml · an edit here reaches the report, not the verdict"},
		},
		{
			name: "a test file carries its immutability mode and stays out of the budget",
			path: "src/total.test.ts",
			want: []string{
				"tests.patterns     yes · **/*.test.* · tests.immutability = append-only applies",
				"diff-budget        not counted",
				`no-disabled-tests  added lines scanned for \.skip\(`,
			},
		},
		{
			name: "a snapshot is test surface rather than a test file",
			path: "src/__snapshots__/total.snap",
			want: []string{
				"tests.patterns     no",
				"tests.fixtures     yes · **/__snapshots__/** · tests.fixtures_immutability = warn applies",
			},
		},
		{
			name: "an ordinary source file counts against the budget",
			path: "src/total.ts",
			want: []string{
				"paths.protected    no",
				"diff-budget        counted",
				"no-disabled-tests  not scanned · not a test file",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := explain(t, config.Defaults(), config.SourceDefaults, tc.path)
			if !strings.Contains(got, "path               "+tc.path+"\n") {
				t.Errorf("output does not open with the path asked about:\n%s", got)
			}
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("output holds no line %q:\n%s", want, got)
				}
			}
		})
	}
}

// TestExplain_AllowedListReadsTheOtherWayRound covers the one list whose empty
// value opens the rules rather than closing them.
func TestExplain_AllowedListReadsTheOtherWayRound(t *testing.T) {
	t.Parallel()

	cfg := config.Defaults()
	allowed, err := domain.NewPatternSet([]string{"src/**"})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	cfg.Paths.Allowed = allowed

	inside := explain(t, cfg, config.SourceDefaults, "src/total.ts")
	if !strings.Contains(inside, "paths.allowed      yes · src/**") {
		t.Errorf("an allowed path does not name its pattern:\n%s", inside)
	}
	got := explain(t, cfg, config.SourceDefaults, "docs/guide.md")
	if !strings.Contains(got, "paths.allowed      no · the diff may touch allowed paths only") {
		t.Errorf("a path outside the allowed list is not called out:\n%s", got)
	}
}

// TestExplain_DisabledGateDropsItsSeverity keeps the two out of each other's
// way: a gate that does not run has no severity to argue about.
func TestExplain_DisabledGateDropsItsSeverity(t *testing.T) {
	t.Parallel()

	cfg := config.Defaults()
	cfg.Gates.Disabled = []domain.GateID{domain.GateBlastRadius}

	got := explain(t, cfg, config.SourceDefaults, "")
	if strings.Contains(got, "severity.blast-radius") {
		t.Errorf("a disabled gate still prints a severity:\n%s", got)
	}
	if !strings.Contains(got, "gates.disabled                     blast-radius") {
		t.Errorf("the disabled gate is not listed:\n%s", got)
	}
}
