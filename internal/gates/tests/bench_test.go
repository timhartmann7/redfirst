package tests_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/timhartmann7/redfirst/internal/config"
	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/gates/tests"
)

const (
	// benchFiles is the diff size section 10 of the spec sets the budget
	// against: verify --no-hooks on 1000 files inside 300 ms.
	benchFiles = 1000
	// benchLines is how many lines each of them adds. Two gates read every one
	// of them, so this is the hot path of the slice, not the file count.
	benchLines = 4
)

// benchDiff builds a diff of that size out of the shapes a real one holds:
// source files, a test file every twentieth, and lines that match none of the
// pattern lists. Nothing matches on purpose: a run that fills evidence measures
// the growth of a slice on top of the matching it is meant to measure.
func benchDiff(n int) domain.Diff {
	files := make([]domain.FileChange, 0, n)
	for i := range n {
		path := fmt.Sprintf("src/pkg%02d/mod%03d.js", i%20, i)
		if i%20 == 0 {
			path = fmt.Sprintf("src/pkg%02d/mod%03d.test.js", i%20, i)
		}
		lines := make([]domain.AddedLine, 0, benchLines)
		for l := range benchLines {
			lines = append(lines, domain.AddedLine{
				Number: l + 1,
				Text:   fmt.Sprintf("  const value%03d = compute(%d, %d)", i, i, l),
			})
		}
		files = append(files, domain.FileChange{
			Path: path, Status: domain.FileModified, Added: len(lines), AddedLines: lines,
		})
	}
	return domain.Diff{Files: files}
}

func benchmarkGate(b *testing.B, newGate func(domain.Config) gate, n int) {
	b.Helper()

	cfg := config.Defaults()
	cfg.Tests.RequireNew = true
	in := domain.Input{Diff: benchDiff(n), Config: cfg}
	g := newGate(cfg)
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		if _, err := g.Run(ctx, in); err != nil {
			b.Fatalf("%s: %v", g.ID(), err)
		}
	}
}

// maxAllocsPerLine is the ceiling one added line may cost a gate.
//
// The number is loose on purpose, and the gap it guards is wide. Matching the
// built-in expression list against a line allocates nothing, or two under the
// race detector, which reuses the regexp machine pool less eagerly. Compiling
// that same list per line costs about a hundred and forty. Anything between the
// two is noise; anything above it is a gate compiling inside its own loop.
const maxAllocsPerLine = 10

// TestTestGates_PatternsCompileOncePerRunNotPerLine is the counterpart of the
// S1 check, one level down. The pattern and expression lists compile when the
// config loads, and a gate only matches against them.
//
// A ceiling rather than the growth between two diff sizes, which is what S1
// compares: a gate compiling per line would allocate in step with the diff, and
// so does the cost of matching, so growth alone tells the two apart in neither
// direction. The size of the per-line cost does.
func TestTestGates_PatternsCompileOncePerRunNotPerLine(t *testing.T) {
	// No t.Parallel: AllocsPerRun measures the whole process.
	cfg := config.Defaults()
	cfg.Tests.RequireNew = true

	cases := []struct {
		name    string
		newGate func(domain.Config) gate
	}{
		{"TestImmutability", func(c domain.Config) gate { return tests.NewTestImmutability(c) }},
		{"NewTestPresent", func(c domain.Config) gate { return tests.NewNewTestPresent(c) }},
		{"NoDisabledTests", func(c domain.Config) gate { return tests.NewNoDisabledTests(c) }},
		{"SuppressionScan", func(c domain.Config) gate { return tests.NewSuppressionScan(c) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lines := float64(benchFiles * benchLines)
			perLine := gateAllocs(t, tc.newGate(cfg), cfg, benchFiles) / lines

			if perLine > maxAllocsPerLine {
				t.Errorf("%.1f allocations per added line, the ceiling is %d: the gate compiles inside its loop",
					perLine, maxAllocsPerLine)
			}
		})
	}
}

// gateAllocs measures one gate over a diff of the given size, after checking
// that the run reports nothing: an evidence entry or a warning would put the
// growth of a result slice into the count and hide what the count is for.
func gateAllocs(t *testing.T, g gate, cfg domain.Config, files int) float64 {
	t.Helper()

	in := domain.Input{Diff: benchDiff(files), Config: cfg}
	ctx := context.Background()
	res, err := g.Run(ctx, in)
	if err != nil {
		t.Fatalf("%s: %v", g.ID(), err)
	}
	if len(res.Evidence) != 0 || len(res.Warnings) != 0 {
		t.Fatalf("%s reports %d evidence and %d warnings on a diff nothing matches",
			g.ID(), len(res.Evidence), len(res.Warnings))
	}
	return testing.AllocsPerRun(20, func() {
		_, _ = g.Run(ctx, in)
	})
}

func BenchmarkTestImmutability_1000Files(b *testing.B) {
	benchmarkGate(b, func(c domain.Config) gate { return tests.NewTestImmutability(c) }, benchFiles)
}

func BenchmarkNewTestPresent_1000Files(b *testing.B) {
	benchmarkGate(b, func(c domain.Config) gate { return tests.NewNewTestPresent(c) }, benchFiles)
}

func BenchmarkNoDisabledTests_1000Files(b *testing.B) {
	benchmarkGate(b, func(c domain.Config) gate { return tests.NewNoDisabledTests(c) }, benchFiles)
}

func BenchmarkSuppressionScan_1000Files(b *testing.B) {
	benchmarkGate(b, func(c domain.Config) gate { return tests.NewSuppressionScan(c) }, benchFiles)
}
