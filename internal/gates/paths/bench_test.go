package paths_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/timhartmann7/redfirst/internal/config"
	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/gates/paths"
)

// benchFiles is the diff size section 10 of the spec sets the budget against:
// verify --no-hooks on 1000 files inside 300 ms.
const benchFiles = 1000

// benchDiff builds a diff of that size out of the shapes a real one holds:
// source files, a test file every twentieth, a lockfile and a workflow.
func benchDiff(n int) domain.Diff {
	files := make([]domain.FileChange, 0, n)
	for i := range n {
		f := domain.FileChange{
			Path:    fmt.Sprintf("src/pkg%02d/mod%03d.js", i%20, i),
			Status:  domain.FileModified,
			Added:   3,
			Deleted: 1,
		}
		switch {
		case i%20 == 0:
			f.Path = fmt.Sprintf("src/pkg%02d/mod%03d.test.js", i%20, i)
		case i%97 == 0:
			f.Path = fmt.Sprintf("vendor/dep%03d/package-lock.json", i)
		case i%89 == 0:
			f.Path = fmt.Sprintf(".github/workflows/job%03d.yml", i)
		}
		files = append(files, f)
	}
	return domain.Diff{Files: files}
}

// benchmarkGate runs one gate over a diff of n files. The config is the
// built-in one, so the pattern lists are the real ones and they arrive
// compiled: the loop below measures matching, never compilation.
func benchmarkGate(b *testing.B, new func(domain.Config) gate, n int) {
	b.Helper()

	cfg := config.Defaults()
	in := domain.Input{Diff: benchDiff(n), Config: cfg}
	g := new(cfg)
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		if _, err := g.Run(ctx, in); err != nil {
			b.Fatalf("%s: %v", g.ID(), err)
		}
	}
}

// plainDiff is a diff no list in the built-in config matches: ordinary source
// files, no test file, no guarded path, nothing protected. A gate walking it
// fills neither evidence nor warnings, so an allocation count taken over it
// holds matching alone and no slice growing behind it.
func plainDiff(n int) domain.Diff {
	files := make([]domain.FileChange, 0, n)
	for i := range n {
		files = append(files, domain.FileChange{
			Path:    fmt.Sprintf("src/pkg%02d/mod%03d.js", i%20, i),
			Status:  domain.FileModified,
			Added:   3,
			Deleted: 1,
		})
	}
	return domain.Diff{Files: files}
}

// allocSlack absorbs the noise a sampled allocation count carries. Compiling
// one glob per file costs thousands, so the margin decides nothing.
const allocSlack = 4

// TestPathGates_GlobsCompileOnceNotPerFile holds the third S1 acceptance
// criterion. The pattern lists compile when the config loads, and a gate only
// matches against them, so ten times the files cost the same allocations. A
// gate that compiled its globs inside the loop would allocate in step with the
// diff instead, which is what the two measurements below compare.
func TestPathGates_GlobsCompileOnceNotPerFile(t *testing.T) {
	// No t.Parallel: AllocsPerRun measures the whole process.
	cfg := config.Defaults()
	// A budget nothing can exceed, so neither size produces evidence and the
	// two runs differ in the number of files and in nothing else.
	cfg.Budget = domain.Budget{MaxFiles: 1 << 20, MaxAddedLines: 1 << 20, MaxDeletedLines: 1 << 20}

	cases := []struct {
		name string
		new  func(domain.Config) gate
	}{
		{"ProtectedPaths", func(c domain.Config) gate { return paths.NewProtectedPaths(c) }},
		{"DiffBudget", func(c domain.Config) gate { return paths.NewDiffBudget(c) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := tc.new(cfg)
			small := gateAllocs(t, g, cfg, benchFiles/10)
			large := gateAllocs(t, g, cfg, benchFiles)

			if large > small+allocSlack {
				t.Errorf("%.0f allocations on %d files against %.0f on %d: the gate is compiling per file",
					large, benchFiles, small, benchFiles/10)
			}
		})
	}
}

// gateAllocs measures one gate over a diff of the given size, after checking
// that the run reports nothing: an evidence entry or a warning would put the
// growth of a result slice into the count and hide what the count is for.
func gateAllocs(t *testing.T, g gate, cfg domain.Config, files int) float64 {
	t.Helper()

	in := domain.Input{Diff: plainDiff(files), Config: cfg}
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

func BenchmarkProtectedPaths_1000Files(b *testing.B) {
	benchmarkGate(b, func(c domain.Config) gate { return paths.NewProtectedPaths(c) }, benchFiles)
}

func BenchmarkDiffBudget_1000Files(b *testing.B) {
	benchmarkGate(b, func(c domain.Config) gate { return paths.NewDiffBudget(c) }, benchFiles)
}
