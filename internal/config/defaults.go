// Package config holds the rule set compiled into the binary and the loader
// that joins the config from the base ref onto it.
package config

import (
	"fmt"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// SourceDefaults is the value ConfigSource takes when no file was found.
const SourceDefaults = "defaults"

// Defaults returns the rule set compiled into the binary. It judges human PRs
// on tiers 0 and 1, so it stays wide: a gate that fires on an ordinary diff
// gets the tool deleted rather than the diff fixed. The strict set for agent
// branches comes from `redfirst init --config`, not from here.
func Defaults() domain.Config {
	return domain.Config{
		Version: 1,
		Budget:  domain.Budget{MaxFiles: 20, MaxAddedLines: 800, MaxDeletedLines: 400},
		Paths: domain.Paths{
			// Blocking: an edit here changes the rules the agent is judged by.
			Protected: mustPatterns([]string{
				".redfirst/**", "redfirst.toml", ".github/workflows/redfirst.yml",
				"CLAUDE.md", "AGENTS.md", ".mcp.json", ".claude/**", ".cursor/**",
			}),
			// Report line only, no effect on the verdict.
			Guarded: mustPatterns([]string{
				".github/**",
				"Dockerfile*", "docker-compose*.yml",
				"**/migrations/**",
				"**/*.lock", "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "go.sum",
			}),
		},
		Tests: domain.Tests{
			Patterns: mustPatterns([]string{
				"**/*.test.*", "**/*.spec.*", "**/*_test.go", "**/test_*.py",
				"tests/**", "spec/**", "__tests__/**",
			}),
			// Test surface: files that decide test outcomes without holding
			// test cases themselves.
			Fixtures: mustPatterns([]string{
				"**/__snapshots__/**", "**/*.snap", "**/*.golden",
				"**/testdata/**", "**/fixtures/**", "**/__mocks__/**",
				"**/conftest.py", "**/setupTests.*", "**/*.setup.ts", "**/*.setup.js",
				"jest.config.*", "vitest.config.*", "playwright.config.*",
			}),
			// append-only rather than strict: strict leaves no legal diff for
			// renaming a symbol that tests cover.
			Immutability:         domain.ImmutabilityAppendOnly,
			FixturesImmutability: domain.ImmutabilityWarn,
			RequireNew:           false,
			ForbidDisabled: mustRegexes([]string{
				`\.skip\(`, `\.only\(`, `\bxit\(`, `\bxdescribe\(`,
			}),
		},
		RedGreen:    domain.RedGreen{Enabled: true, ProbeRuns: 3, CompileFailureOnBase: domain.CompileFailureWeakRed},
		Suite:       domain.Suite{RetryFailed: 1},
		Suppression: defaultSuppression(),
		Severity:    map[domain.GateID]domain.Severity{domain.GateDiffBudget: domain.SeverityWarn},
		Scopes:      domain.Scopes{AllowCrossScope: true},
	}
}

func defaultSuppression() domain.Suppression {
	return domain.Suppression{
		Enabled: true,
		Patterns: mustRegexes([]string{
			`catch\s*\(\s*\w*\s*\)\s*\{\s*\}`,
			`@ts-ignore`,
			`@ts-expect-error`,
			`eslint-disable`,
			`:\s*any\b`,
		}),
	}
}

// The built-in lists are constants of this package, so a pattern that fails to
// compile is our own bug and there is no run left to degrade into.
func mustPatterns(raw []string) domain.PatternSet {
	set, err := domain.NewPatternSet(raw)
	if err != nil {
		panic(fmt.Errorf("%w: built-in default globs: %w", domain.ErrInternal, err))
	}
	return set
}

func mustRegexes(raw []string) domain.RegexSet {
	set, err := domain.NewRegexSet(raw)
	if err != nil {
		panic(fmt.Errorf("%w: built-in default expressions: %w", domain.ErrInternal, err))
	}
	return set
}
