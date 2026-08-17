package domain

import (
	"fmt"
	"slices"
)

// Severity decides whether a gate violation blocks or only reaches the report.
type Severity string

const (
	SeverityFail Severity = "fail"
	SeverityWarn Severity = "warn"
)

// UnmarshalText rejects an unknown severity instead of falling back to a value
// nobody wrote.
func (s *Severity) UnmarshalText(text []byte) error {
	switch v := Severity(text); v {
	case SeverityFail, SeverityWarn:
		*s = v
		return nil
	}
	return fmt.Errorf("%w: severity %q is neither %q nor %q", ErrConfig, text, SeverityFail, SeverityWarn)
}

// Immutability is the rule applied to test files and to the test surface.
type Immutability string

const (
	// ImmutabilityStrict forbids touching an existing test file at all.
	ImmutabilityStrict Immutability = "strict"
	// ImmutabilityAppendOnly allows added lines and no deleted ones.
	ImmutabilityAppendOnly Immutability = "append-only"
	// ImmutabilityCases judges outcomes: no existing case may vanish or regress.
	ImmutabilityCases Immutability = "cases"
	// ImmutabilityWarn only reaches the report. tests.fixtures only.
	ImmutabilityWarn Immutability = "warn"
)

// UnmarshalText accepts every mode. Rejecting "warn" for tests.immutability,
// where it does not apply, is validation's job: only there is the key known.
func (i *Immutability) UnmarshalText(text []byte) error {
	switch v := Immutability(text); v {
	case ImmutabilityStrict, ImmutabilityAppendOnly, ImmutabilityCases, ImmutabilityWarn:
		*i = v
		return nil
	}
	return fmt.Errorf("%w: unknown immutability mode %q", ErrConfig, text)
}

// CompileFailure is how a new test that fails to build on base gets counted.
type CompileFailure string

const (
	CompileFailureRed     CompileFailure = "red"
	CompileFailureWeakRed CompileFailure = "weak-red"
	CompileFailureFail    CompileFailure = "fail"
)

func (c *CompileFailure) UnmarshalText(text []byte) error {
	switch v := CompileFailure(text); v {
	case CompileFailureRed, CompileFailureWeakRed, CompileFailureFail:
		*c = v
		return nil
	}
	return fmt.Errorf("%w: unknown compile_failure_on_base %q", ErrConfig, text)
}

// Config is the effective rule set: built-in defaults with the keys the config
// from base replaced. The unit of replacement is the key, never the section.
type Config struct {
	Version     int                 `toml:"version"`
	Budget      Budget              `toml:"budget"`
	Paths       Paths               `toml:"paths"`
	Tests       Tests               `toml:"tests"`
	RedGreen    RedGreen            `toml:"red_green"`
	Suite       Suite               `toml:"suite"`
	Suppression Suppression         `toml:"suppression"`
	Severity    map[GateID]Severity `toml:"severity"`
	Gates       Gates               `toml:"gates"`
	Scopes      Scopes              `toml:"scopes"`
	Scope       []Scope             `toml:"scope"`
}

type Budget struct {
	MaxFiles        int `toml:"max_files"`
	MaxAddedLines   int `toml:"max_added_lines"`
	MaxDeletedLines int `toml:"max_deleted_lines"`
}

type Paths struct {
	// Protected blocks: an edit here changes the rules the agent is judged by.
	Protected PatternSet `toml:"protected"`
	// Guarded reaches the report and leaves the verdict alone.
	Guarded PatternSet `toml:"guarded"`
	// Allowed, when non-empty, is the only place the diff may touch.
	Allowed PatternSet `toml:"allowed"`
}

type Tests struct {
	Patterns PatternSet `toml:"patterns"`
	// Fixtures is the test surface: files that decide test outcomes without
	// holding test cases themselves.
	Fixtures             PatternSet   `toml:"fixtures"`
	Immutability         Immutability `toml:"immutability"`
	FixturesImmutability Immutability `toml:"fixtures_immutability"`
	RequireNew           bool         `toml:"require_new"`
	ForbidDisabled       RegexSet     `toml:"forbid_disabled"`
	// KnownFlaky comes from base, so the agent cannot extend it.
	KnownFlaky []string `toml:"known_flaky"`
}

type RedGreen struct {
	Enabled bool `toml:"enabled"`
	// ProbeRuns is how many times a probe runs. Every run must agree.
	ProbeRuns            int            `toml:"probe_runs"`
	CompileFailureOnBase CompileFailure `toml:"compile_failure_on_base"`
}

type Suite struct {
	RetryFailed int `toml:"retry_failed"`
}

type Suppression struct {
	Enabled  bool     `toml:"enabled"`
	Patterns RegexSet `toml:"patterns"`
}

type Gates struct {
	// Disabled switches gates off by ID. An empty list means all enabled.
	Disabled []GateID `toml:"disabled"`
}

type Scopes struct {
	AllowCrossScope bool `toml:"allow_cross_scope"`
}

// Scope is parsed and validated in v1; a non-empty list is exit code 3. The
// shape is frozen here so adding scopes in v2 breaks nobody.
type Scope struct {
	Name     string   `toml:"name"`
	Match    []string `toml:"match"`
	HooksDir string   `toml:"hooks_dir"`
	Budget   Budget   `toml:"budget"`
}

// SeverityOf returns the configured severity for a gate, defaulting to fail.
func (c Config) SeverityOf(id GateID) Severity {
	if s, ok := c.Severity[id]; ok {
		return s
	}
	return SeverityFail
}

// GateDisabled reports whether the config switched the gate off.
func (c Config) GateDisabled(id GateID) bool {
	return slices.Contains(c.Gates.Disabled, id)
}

// IsTestSurface reports whether a path decides test outcomes: either a test
// file or part of the test surface.
func (c Config) IsTestSurface(path string) bool {
	return c.Tests.Patterns.Match(path) || c.Tests.Fixtures.Match(path)
}
