package tests_test

import (
	"context"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// gate is the shape the runner consumes. The tests speak through it, so a gate
// that stops answering one of the three questions fails here.
type gate interface {
	ID() domain.GateID
	Requires() []domain.Capability
	Run(ctx context.Context, in domain.Input) (domain.GateResult, error)
}

// rules is the part of [tests] and [suppression] a case fills.
type rules struct {
	patterns       []string
	fixtures       []string
	immutability   domain.Immutability
	fixturesMode   domain.Immutability
	requireNew     bool
	forbidDisabled []string
	suppression    []string
	suppressionOff bool
}

// testsConfig compiles the lists the way the config loader does: once, ahead of
// the run, which is the whole reason a gate only ever matches.
func testsConfig(t *testing.T, r rules) domain.Config {
	t.Helper()

	return domain.Config{
		Tests: domain.Tests{
			Patterns:             patternSet(t, r.patterns),
			Fixtures:             patternSet(t, r.fixtures),
			Immutability:         r.immutability,
			FixturesImmutability: r.fixturesMode,
			RequireNew:           r.requireNew,
			ForbidDisabled:       regexSet(t, r.forbidDisabled),
		},
		Suppression: domain.Suppression{
			Enabled:  !r.suppressionOff,
			Patterns: regexSet(t, r.suppression),
		},
	}
}

func patternSet(t *testing.T, raw []string) domain.PatternSet {
	t.Helper()

	set, err := domain.NewPatternSet(raw)
	if err != nil {
		t.Fatalf("compile %v: %v", raw, err)
	}
	return set
}

func regexSet(t *testing.T, raw []string) domain.RegexSet {
	t.Helper()

	set, err := domain.NewRegexSet(raw)
	if err != nil {
		t.Fatalf("compile %v: %v", raw, err)
	}
	return set
}

// runGate calls the gate on a diff of the given files.
func runGate(t *testing.T, g gate, cfg domain.Config, files ...domain.FileChange) domain.GateResult {
	t.Helper()
	return runGateOnDiff(t, g, cfg, domain.Diff{Files: files})
}

func runGateOnDiff(t *testing.T, g gate, cfg domain.Config, d domain.Diff) domain.GateResult {
	t.Helper()

	res, err := g.Run(context.Background(), domain.Input{Diff: d, Config: cfg})
	if err != nil {
		t.Fatalf("%s: %v", g.ID(), err)
	}
	return res
}

// withLines fills the added lines of a change, numbered from one. Only the
// report reads a number, so a case that asserts on one says which line it means
// by the order it wrote them in.
func withLines(f domain.FileChange, lines ...string) domain.FileChange {
	f.Added = len(lines)
	for i, text := range lines {
		f.AddedLines = append(f.AddedLines, domain.AddedLine{Number: i + 1, Text: text})
	}
	return f
}

func added(path string, lines ...string) domain.FileChange {
	return withLines(domain.FileChange{Path: path, Status: domain.FileAdded}, lines...)
}

func modified(path string, deleted int, lines ...string) domain.FileChange {
	f := withLines(domain.FileChange{Path: path, Status: domain.FileModified}, lines...)
	f.Deleted = deleted
	return f
}

func removed(path string, lines int) domain.FileChange {
	return domain.FileChange{Path: path, Status: domain.FileDeleted, Deleted: lines}
}

func renamed(from, to string, deleted int, lines ...string) domain.FileChange {
	f := withLines(domain.FileChange{Path: to, OldPath: from, Status: domain.FileRenamed}, lines...)
	f.Deleted = deleted
	return f
}

// binary carries no line counts: git prints "-" on both sides and gitx turns
// that into the flag.
func binary(path string, status domain.FileStatus) domain.FileChange {
	return domain.FileChange{Path: path, Status: status, Binary: true}
}

func assertStatus(t *testing.T, res domain.GateResult, want domain.GateStatus) {
	t.Helper()

	if res.Status != want {
		t.Errorf("status = %q, want %q (message %q, evidence %+v)",
			res.Status, want, res.Message, res.Evidence)
	}
}

// assertPass also holds the part of the contract a status alone does not: only
// a refusal carries a remediation, and a clean run points at no file.
func assertPass(t *testing.T, res domain.GateResult) {
	t.Helper()

	assertStatus(t, res, domain.StatusPass)
	if res.Remediation != "" {
		t.Errorf("remediation = %q, want none: only a refusal carries one", res.Remediation)
	}
	if len(res.Evidence) != 0 {
		t.Errorf("evidence = %+v, want none", res.Evidence)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("warnings = %+v, want none", res.Warnings)
	}
}

// assertRefusal holds what section 5 of the spec asks of every failing gate: a
// remediation the orchestrator can act on, and evidence naming what broke.
func assertRefusal(t *testing.T, res domain.GateResult, want domain.Remediation) {
	t.Helper()

	assertStatus(t, res, domain.StatusFail)
	if res.Remediation != want {
		t.Errorf("remediation = %q, want %q", res.Remediation, want)
	}
	if res.Message == "" {
		t.Error("message is empty: a refusal has to say what is wrong")
	}
}

// evidenceFiles is what the refusal points at, in the order it reported.
func evidenceFiles(res domain.GateResult) []string {
	files := make([]string, 0, len(res.Evidence))
	for _, e := range res.Evidence {
		files = append(files, e.File)
	}
	return files
}
