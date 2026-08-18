package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/harness"
)

// Source is the slice of git the probe needs: the head content of the files it
// overlays onto the base working copy. It is declared here, at the consumer;
// internal/gitx satisfies it.
type Source interface {
	ShowFile(ctx context.Context, ref, path string) (io.ReadCloser, error)
}

// Probe drives the project's own harness for the gates that need it, and is
// what the runner puts into domain.Input.
//
// It owns the order of the phases as much as the runs inside them. Base first,
// then head, always: state can leak forward only, and the base probe is the one
// run in the whole system that expects red, so a leak there produces a pass
// rather than a refusal.
//
// A Probe belongs to one run and is driven from one goroutine.
type Probe struct {
	session *harness.Session
	source  Source
	diff    domain.Diff
	cfg     domain.Config

	base *harness.Worktree
	// suite is the working copy of the suite phase, and suiteTree is what it
	// holds: the base suite replaces the head one rather than paying for a
	// working copy of its own.
	suite     *harness.Worktree
	suiteTree string

	inventory cached
	overlaid  cached
	head      cached
	suiteRuns cached
	baseSuite cached
}

// NewProbe builds the probe over a session that is already up.
func NewProbe(session *harness.Session, source Source, diff domain.Diff, cfg domain.Config) *Probe {
	return &Probe{session: session, source: source, diff: diff, cfg: cfg}
}

// cached memoises one probe, the error as much as the runs: a second gate
// asking for the same phase gets the same answer rather than a second attempt,
// which is what "the same input yields the same verdict" means here.
type cached struct {
	done bool
	runs domain.Runs
	err  error
}

func (c *cached) get(f func() (domain.Runs, error)) (domain.Runs, error) {
	if !c.done {
		c.done = true
		c.runs, c.err = f()
	}
	return c.runs, c.err
}

// Inventory runs the diff's test files as base has them, once, with nothing
// overlaid. It is what the case names of the run come from.
func (p *Probe) Inventory(ctx context.Context) (domain.Runs, error) {
	return p.inventory.get(func() (domain.Runs, error) {
		files := p.basePaths()
		if len(files) == 0 {
			// The diff adds test files and modifies none, so base holds no case
			// this run could lose. A typical fix skips this run entirely.
			return nil, nil
		}
		w, err := p.baseWorktree(ctx)
		if err != nil {
			return nil, err
		}
		return p.repeat(ctx, w, files, 1)
	})
}

// Overlaid puts the head versions of those files onto the base tree and runs
// them probe_runs times. The fix does not travel.
func (p *Probe) Overlaid(ctx context.Context) (domain.Runs, error) {
	return p.overlaid.get(func() (domain.Runs, error) {
		// The inventory reads the base tree as base has it, so it finishes
		// before anything lands on top.
		if _, err := p.Inventory(ctx); err != nil {
			return nil, err
		}
		files := p.headPaths()
		if len(files) == 0 {
			// The diff removed test files and added none, so head carries
			// nothing to probe. An empty filter would run the whole suite, and
			// a probe that ran everything would answer a question nobody asked.
			return nil, nil
		}
		w, err := p.baseWorktree(ctx)
		if err != nil {
			return nil, err
		}
		if err := p.overlay(ctx, w.Dir()); err != nil {
			return nil, err
		}
		return p.repeat(ctx, w, files, p.cfg.RedGreen.ProbeRuns)
	})
}

// Head runs the same files on head, probe_runs times.
func (p *Probe) Head(ctx context.Context) (domain.Runs, error) {
	return p.head.get(func() (domain.Runs, error) {
		// The whole base phase first, not the inventory alone. Whoever asks for
		// head first, the run that expects red has to be behind us by then.
		if _, err := p.Overlaid(ctx); err != nil {
			return nil, err
		}
		files := p.headPaths()
		if len(files) == 0 {
			return nil, nil
		}
		w, err := p.session.Worktree(ctx, domain.PhaseHead, p.diff.Head)
		if err != nil {
			return nil, err
		}
		return p.repeat(ctx, w, files, p.cfg.RedGreen.ProbeRuns)
	})
}

// Suite runs the whole suite on head, unfiltered, once.
func (p *Probe) Suite(ctx context.Context) (domain.Runs, error) {
	return p.suiteRuns.get(func() (domain.Runs, error) {
		w, err := p.suiteWorktree(ctx, p.diff.Head)
		if err != nil {
			return nil, err
		}
		return p.repeat(ctx, w, nil, 1)
	})
}

// Retry reruns the given files in the working copy the suite already built.
func (p *Probe) Retry(ctx context.Context, files []string) (domain.Runs, error) {
	if p.cfg.Suite.RetryFailed < 1 || len(files) == 0 {
		return nil, nil
	}
	w, err := p.suiteWorktree(ctx, p.diff.Head)
	if err != nil {
		return nil, err
	}
	return p.repeat(ctx, w, files, p.cfg.Suite.RetryFailed)
}

// BaseSuite runs the whole suite on the merge base.
func (p *Probe) BaseSuite(ctx context.Context) (domain.Runs, error) {
	return p.baseSuite.get(func() (domain.Runs, error) {
		// The suite phase is done with its head copy by the time anybody asks
		// this, so the base tree replaces it in place. The base phase copy is
		// left alone: red-green may still be holding it, overlaid.
		w, err := p.suiteWorktree(ctx, p.diff.MergeBase)
		if err != nil {
			return nil, err
		}
		return p.repeat(ctx, w, nil, 1)
	})
}

// repeat runs the hook n times in one working copy and collects what it said.
// Every run of a phase shares the copy: the tree is identical byte for byte
// between them, so a rebuild would change nothing, and a copy per run would
// mean probe_runs dependency installs for one pull request.
func (p *Probe) repeat(ctx context.Context, w *harness.Worktree, filter []string, n int) (domain.Runs, error) {
	out := make(domain.Runs, 0, n)
	for range n {
		run, err := w.Run(ctx, filter)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, nil
}

func (p *Probe) baseWorktree(ctx context.Context) (*harness.Worktree, error) {
	if p.base == nil {
		w, err := p.session.Worktree(ctx, domain.PhaseBase, p.diff.MergeBase)
		if err != nil {
			return nil, err
		}
		p.base = w
	}
	return p.base, nil
}

func (p *Probe) suiteWorktree(ctx context.Context, tree string) (*harness.Worktree, error) {
	if p.suite == nil || p.suiteTree != tree {
		w, err := p.session.Worktree(ctx, domain.PhaseSuite, tree)
		if err != nil {
			return nil, err
		}
		p.suite, p.suiteTree = w, tree
	}
	return p.suite, nil
}

// basePaths is the test surface of the diff as base has it: what the inventory
// run is filtered to.
func (p *Probe) basePaths() []string {
	var files []string
	for _, f := range p.diff.TestSurface(p.cfg) {
		if path, ok := f.BasePath(); ok {
			files = append(files, path)
		}
	}
	return files
}

// headPaths is the same surface as head has it: what the probes are filtered to
// once the head versions are in place. A file the diff deleted appears in
// neither probe, because filtering a runner to a path that is not there asks it
// to collect a file nobody wrote.
func (p *Probe) headPaths() []string {
	var files []string
	for _, f := range p.diff.TestSurface(p.cfg) {
		if path, ok := f.HeadPath(); ok {
			files = append(files, path)
		}
	}
	return files
}

// overlay puts the head versions of the diff's test files onto the base working
// copy, whole and alone. Nothing else travels: the base probe is the run that
// proves the added test catches the bug, and it only proves it without the fix.
func (p *Probe) overlay(ctx context.Context, dir string) error {
	for _, f := range p.diff.TestSurface(p.cfg) {
		head, onHead := f.HeadPath()
		if base, onBase := f.BasePath(); onBase && base != head {
			// A test the diff deleted, or moved out of the surface, is not on
			// head, and leaving the base copy behind would let it answer for a
			// file the agent removed.
			if err := os.Remove(filepath.Join(dir, filepath.FromSlash(base))); err != nil &&
				!errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("%w: overlay %s onto the base copy: %w", domain.ErrHarness, base, err)
			}
		}
		if !onHead {
			continue
		}
		if err := p.write(ctx, dir, f); err != nil {
			return err
		}
	}
	return nil
}

// write copies one file out of head into the working copy, the mode included:
// a runner that executes the file needs the bit the tree carries.
func (p *Probe) write(ctx context.Context, dir string, f domain.FileChange) (err error) {
	src, err := p.source.ShowFile(ctx, p.diff.Head, f.Path)
	if err != nil {
		return fmt.Errorf("%w: read %s out of head: %w", domain.ErrHarness, f.Path, err)
	}
	defer func() { _ = src.Close() }()

	target := filepath.Join(dir, filepath.FromSlash(f.Path))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("%w: overlay %s onto the base copy: %w", domain.ErrHarness, f.Path, err)
	}
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fileMode(f.Mode))
	if err != nil {
		return fmt.Errorf("%w: overlay %s onto the base copy: %w", domain.ErrHarness, f.Path, err)
	}
	defer func() {
		if closeErr := out.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("%w: overlay %s onto the base copy: %w", domain.ErrHarness, f.Path, closeErr)
		}
	}()

	if _, err := io.Copy(out, src); err != nil {
		return fmt.Errorf("%w: overlay %s onto the base copy: %w", domain.ErrHarness, f.Path, err)
	}
	// The base version may already sit there with a mode of its own, and
	// OpenFile leaves an existing file's mode alone.
	if err := out.Chmod(fileMode(f.Mode)); err != nil {
		return fmt.Errorf("%w: overlay %s onto the base copy: %w", domain.ErrHarness, f.Path, err)
	}
	return nil
}

// fileMode reads the permission bits out of the tree entry git printed. An
// entry that carries none falls back to a plain file: the executable bit is the
// one part of a mode that changes what the harness runs, and inventing it is
// worse than leaving it off.
func fileMode(mode string) os.FileMode {
	bits, err := strconv.ParseUint(mode, 8, 32)
	if err != nil {
		return 0o644
	}
	if perm := os.FileMode(bits) & 0o777; perm != 0 {
		return perm
	}
	return 0o644
}
