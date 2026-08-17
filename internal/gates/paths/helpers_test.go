package paths_test

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

// lists names the three path lists a test fills.
type lists struct {
	protected []string
	guarded   []string
	allowed   []string
}

// pathsConfig compiles the lists the way the config loader does: once, ahead
// of the run, which is the whole reason a gate only ever matches.
func pathsConfig(t *testing.T, l lists) domain.Config {
	t.Helper()

	return domain.Config{Paths: domain.Paths{
		Protected: patternSet(t, l.protected),
		Guarded:   patternSet(t, l.guarded),
		Allowed:   patternSet(t, l.allowed),
	}}
}

func patternSet(t *testing.T, raw []string) domain.PatternSet {
	t.Helper()

	set, err := domain.NewPatternSet(raw)
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

func added(path string) domain.FileChange {
	return domain.FileChange{Path: path, Status: domain.FileAdded, Added: 10}
}

func modified(path string) domain.FileChange {
	return domain.FileChange{Path: path, Status: domain.FileModified, Added: 6, Deleted: 4}
}

func deleted(path string) domain.FileChange {
	return domain.FileChange{Path: path, Status: domain.FileDeleted, Deleted: 12}
}

func renamed(from, to string) domain.FileChange {
	return domain.FileChange{Path: to, OldPath: from, Status: domain.FileRenamed, Added: 2, Deleted: 1}
}

func copied(from, to string) domain.FileChange {
	return domain.FileChange{Path: to, OldPath: from, Status: domain.FileCopied, Added: 3}
}

// binaryFile carries no line counts: git prints "-" on both sides and gitx
// turns that into the flag.
func binaryFile(path string) domain.FileChange {
	return domain.FileChange{Path: path, Status: domain.FileModified, Binary: true}
}

func assertPass(t *testing.T, res domain.GateResult) {
	t.Helper()

	if res.Status != domain.StatusPass {
		t.Errorf("status = %q, want %q (message %q)", res.Status, domain.StatusPass, res.Message)
	}
	if res.Remediation != "" {
		t.Errorf("remediation = %q, want none: only a refusal carries one", res.Remediation)
	}
	if len(res.Evidence) != 0 {
		t.Errorf("evidence = %v, want none", res.Evidence)
	}
}
