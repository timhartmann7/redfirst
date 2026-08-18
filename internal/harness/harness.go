// Package harness runs the scripts a project keeps in .redfirst/. It owns the
// two layers the spec separates: the services, which may be reused between
// runs, and the working copy, which is recreated for every phase because a
// build artifact that survives a phase would turn a nonexistent fix green.
//
// A session is driven from one goroutine.
package harness

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// HooksDir is where a project keeps its hooks. Its presence on the base ref is
// what switches tier 2 on.
const HooksDir = ".redfirst"

// Mode is how the service layer travels between runs. The presence of
// env-reset.sh picks it, not a config flag: reuse without a reset does not
// exist, because data left behind by the base run would decide the head run.
type Mode string

const (
	// ModeFresh brings the services up and down around every run.
	ModeFresh Mode = "fresh"
	// ModeReused brings them up once and resets the data between runs.
	ModeReused Mode = "reused"
)

// Source is the slice of git the harness needs. internal/gitx satisfies it.
type Source interface {
	Export(ctx context.Context, ref, dir string) error
}

// Options are the choices the CLI passes in.
type Options struct {
	Source Source
	// Base is where the hooks come from. All code that judges comes from base,
	// and the hooks judge. Pass a resolved commit rather than a branch name: a
	// ref that moves mid-run would hand two phases two different rule sets.
	Base string
	// WorkDir is where the run directory goes; empty means the system temp.
	WorkDir string
	// Fresh forbids service reuse whatever the hook set offers (--fresh-env).
	Fresh bool
}

// Session is one environment: brought up once, torn down once.
type Session struct {
	opts  Options
	hooks hooks
	mode  Mode
	dir   string
	// runID is the unique prefix hooks put into container, network and port
	// names, so two branches checked at once do not fight over resources.
	runID string
	// runs counts the test.sh invocations of the whole session, which is what
	// decides whether a reset is due. The probe index counts inside a phase.
	runs   int
	closed bool
}

// Open copies the hooks out of the base ref and brings the services up.
//
// The caller closes the session through a defer. Teardown has to survive a
// panic, a deadline and a SIGINT, and a deferred Close is what makes that true.
func Open(ctx context.Context, opts Options) (s *Session, err error) {
	dir, err := os.MkdirTemp(opts.WorkDir, "redfirst-")
	if err != nil {
		return nil, fmt.Errorf("%w: create the run directory: %w", domain.ErrHarness, err)
	}
	// Absolute from here on. Every hook runs with a working directory of its
	// own, and a relative --work-dir would send it looking for the scripts
	// under the working copy instead.
	if dir, err = filepath.Abs(dir); err != nil {
		return nil, fmt.Errorf("%w: resolve the run directory: %w", domain.ErrHarness, err)
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(dir)
		}
	}()

	// The run id comes from the directory the operating system just made
	// unique for us, so nothing here reads a clock or a counter.
	s = &Session{opts: opts, dir: dir, runID: filepath.Base(dir)}

	hooksDir := filepath.Join(dir, "hooks")
	if err = opts.Source.Export(ctx, opts.Base+":"+HooksDir, hooksDir); err != nil {
		return nil, fmt.Errorf("cannot read %s out of %s: %w", HooksDir, opts.Base, err)
	}
	if s.hooks, err = discover(hooksDir, dir); err != nil {
		return nil, err
	}

	s.mode = ModeFresh
	if s.hooks.reset != "" && !opts.Fresh {
		s.mode = ModeReused
	}
	if s.mode == ModeReused {
		if err = s.up(ctx); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Mode is how this session treats the services. It reaches the first line of
// the report: somebody digging into a strange refusal has to see it at once.
func (s *Session) Mode() Mode { return s.mode }

// Close runs env-down.sh and removes everything the session created. It is safe
// to call twice, and it tears the environment down on a context that is already
// dead: that is the case it exists for.
func (s *Session) Close(ctx context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true

	var err error
	if s.mode == ModeReused {
		err = s.down(ctx)
	}
	if rmErr := os.RemoveAll(s.dir); err == nil && rmErr != nil {
		err = fmt.Errorf("%w: remove %s: %w", domain.ErrHarness, s.dir, rmErr)
	}
	return err
}

// up brings the services up, and tears down whatever came up before the script
// gave in: env-up.sh failing halfway is the ordinary way a compose file fails.
func (s *Session) up(ctx context.Context) error {
	if err := s.hooks.mustRun(ctx, s.hooks.up, s.hooks.dir, s.env()); err != nil {
		return errors.Join(err, s.down(ctx))
	}
	return nil
}

// down runs env-down.sh on a context of its own.
//
// By the time a deadline or a SIGINT reaches here the run's context is already
// cancelled, and teardown still has to happen. A deadline of its own keeps a
// hanging teardown from holding the process open for ever, which would leave
// the containers running that this script exists to stop.
func (s *Session) down(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), teardownGrace)
	defer cancel()

	return s.hooks.mustRun(ctx, s.hooks.down, s.hooks.dir, s.env())
}

func (s *Session) reset(ctx context.Context) error {
	return s.hooks.mustRun(ctx, s.hooks.reset, s.hooks.dir, s.env())
}

// env is what every hook gets. Only test.sh gets more.
func (s *Session) env() []string {
	return []string{"REDFIRST_RUN_ID=" + s.runID}
}
