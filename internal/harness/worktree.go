package harness

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// Worktree is one phase's working copy.
//
// Every run inside the phase shares it. The tree is identical byte for byte
// between those runs, so a rebuild would change nothing, and recreating the
// copy per run would mean probe_runs dependency installs and builds for one
// pull request. The copy is never shared across phases: a dist directory
// compiled on base that survived into the head phase would turn a fix that does
// not exist green.
type Worktree struct {
	session *Session
	phase   domain.Phase
	dir     string
	// runs is $REDFIRST_PROBE_INDEX, counted inside this phase and starting
	// at one.
	runs int
}

// Worktree lays tree out as the working copy of a phase. A phase started twice
// is built again from scratch.
func (s *Session) Worktree(ctx context.Context, phase domain.Phase, tree string) (*Worktree, error) {
	dir := filepath.Join(s.dir, "work-"+string(phase))
	if err := os.RemoveAll(dir); err != nil {
		return nil, fmt.Errorf("%w: clear %s: %w", domain.ErrHarness, dir, err)
	}
	if err := s.opts.Source.Export(ctx, tree, dir); err != nil {
		return nil, fmt.Errorf("cannot lay %q out as the %s working copy: %w", tree, phase, err)
	}
	return &Worktree{session: s, phase: phase, dir: dir}, nil
}

// Dir is the working copy, which is what $REDFIRST_WORKDIR names. red-green
// overlays the head versions of the test files onto it.
func (w *Worktree) Dir() string { return w.dir }

// Run executes test.sh once inside the working copy and reads what it wrote.
// An empty filter runs the whole suite.
func (w *Worktree) Run(ctx context.Context, filter []string) (res domain.Run, err error) {
	s := w.session
	if err = s.beforeRun(ctx); err != nil {
		return domain.Run{}, err
	}
	if s.mode == ModeFresh {
		defer func() {
			if downErr := s.down(ctx); err == nil {
				err = downErr
			}
		}()
	}

	w.runs++
	s.runs++

	results := filepath.Join(s.dir, "results", fmt.Sprintf("%s-%d", w.phase, w.runs))
	if mkErr := os.MkdirAll(filepath.Dir(results), 0o755); mkErr != nil {
		return domain.Run{}, fmt.Errorf("%w: create the results directory: %w", domain.ErrHarness, mkErr)
	}
	// A phase started again numbers its runs from one again, and a hook that
	// dies before writing has to leave the run with nothing rather than with
	// what the run before it wrote.
	if rmErr := os.Remove(results); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
		return domain.Run{}, fmt.Errorf("%w: clear %s: %w", domain.ErrHarness, results, rmErr)
	}

	run, err := s.hooks.run(ctx, s.hooks.test, w.dir, w.env(results, filter))
	if err != nil {
		return domain.Run{}, err
	}
	cases, err := readResults(results)
	if err != nil {
		return domain.Run{}, err
	}
	return domain.Run{Index: w.runs, Green: run.ok, Cases: cases}, nil
}

// beforeRun readies the services for one more run: fresh mode brings them up
// around every run, reused mode resets the data between them.
func (s *Session) beforeRun(ctx context.Context) error {
	if s.mode == ModeFresh {
		return s.up(ctx)
	}
	if s.runs > 0 {
		return s.reset(ctx)
	}
	return nil
}

// env is what test.sh gets on top of what every hook gets.
func (w *Worktree) env(results string, filter []string) []string {
	return append(w.session.env(),
		"REDFIRST_WORKDIR="+w.dir,
		// Space separated, because the hook interpolates it into its own
		// command line: `vitest run $REDFIRST_FILTER`.
		"REDFIRST_FILTER="+strings.Join(filter, " "),
		"REDFIRST_RESULTS="+results,
		"REDFIRST_PHASE="+string(w.phase),
		"REDFIRST_PROBE_INDEX="+strconv.Itoa(w.runs),
	)
}
