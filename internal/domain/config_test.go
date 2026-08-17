package domain_test

import (
	"errors"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
)

func TestConfig_SeverityOfDefaultsToFail(t *testing.T) {
	t.Parallel()

	cfg := domain.Config{Severity: map[domain.GateID]domain.Severity{
		domain.GateDiffBudget: domain.SeverityWarn,
	}}

	if got := cfg.SeverityOf(domain.GateDiffBudget); got != domain.SeverityWarn {
		t.Errorf("SeverityOf(diff-budget) = %q, want warn", got)
	}
	if got := cfg.SeverityOf(domain.GateProtectedPaths); got != domain.SeverityFail {
		t.Errorf("SeverityOf(protected-paths) = %q, want fail: an unset gate blocks", got)
	}
}

func TestConfig_GateDisabledReadsTheDisabledList(t *testing.T) {
	t.Parallel()

	cfg := domain.Config{Gates: domain.Gates{Disabled: []domain.GateID{domain.GateBlastRadius}}}

	if !cfg.GateDisabled(domain.GateBlastRadius) {
		t.Error("blast-radius should be disabled")
	}
	if cfg.GateDisabled(domain.GateRedGreen) {
		t.Error("red-green should be enabled")
	}
}

func TestConfig_IsTestSurfaceCoversPatternsAndFixtures(t *testing.T) {
	t.Parallel()

	patterns, err := domain.NewPatternSet([]string{"**/*.test.ts"})
	if err != nil {
		t.Fatalf("NewPatternSet: %v", err)
	}
	fixtures, err := domain.NewPatternSet([]string{"**/__snapshots__/**"})
	if err != nil {
		t.Fatalf("NewPatternSet: %v", err)
	}
	cfg := domain.Config{Tests: domain.Tests{Patterns: patterns, Fixtures: fixtures}}

	cases := map[string]bool{
		"src/total.test.ts":                  true,
		"src/__snapshots__/total.snap":       true,
		"src/total.ts":                       false,
		"docs/__snapshots__.md":              false,
		"packages/web/src/order.test.ts":     true,
		"packages/web/__snapshots__/a/b.txt": true,
	}
	for path, want := range cases {
		if got := cfg.IsTestSurface(path); got != want {
			t.Errorf("IsTestSurface(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestConfig_UnknownEnumValuesAreConfigErrors(t *testing.T) {
	t.Parallel()

	var (
		severity    domain.Severity
		immutable   domain.Immutability
		compileFail domain.CompileFailure
	)
	cases := map[string]error{
		"severity":                severity.UnmarshalText([]byte("block")),
		"immutability":            immutable.UnmarshalText([]byte("frozen")),
		"compile_failure_on_base": compileFail.UnmarshalText([]byte("green")),
	}
	for name, err := range cases {
		if !errors.Is(err, domain.ErrConfig) {
			t.Errorf("%s: error = %v, want ErrConfig", name, err)
		}
	}
}

func TestConfig_ImmutabilityAcceptsWarnForTheFixtureSurface(t *testing.T) {
	t.Parallel()

	var mode domain.Immutability
	if err := mode.UnmarshalText([]byte("warn")); err != nil {
		t.Fatalf("warn rejected: %v", err)
	}
	if mode != domain.ImmutabilityWarn {
		t.Errorf("mode = %q, want warn", mode)
	}
}
