package harness

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// The five scripts of the hook contract. Three are required for tier 2; a
// project with no external services leaves two of those as empty stubs.
const (
	hookEnv   = "env.sh"
	hookUp    = "env-up.sh"
	hookReset = "env-reset.sh"
	hookTest  = "test.sh"
	hookDown  = "env-down.sh"
)

const (
	// teardownGrace bounds env-down.sh once the run's own deadline has passed.
	// It is not a rule about how long a project's teardown may take, which is
	// what the run timeout covers, but the last stop before a hung script keeps
	// redfirst alive for ever.
	teardownGrace = 2 * time.Minute
	// killGrace is how long os/exec keeps reading the output pipes after the
	// context died. Past it the copying goroutines are abandoned, so a
	// grandchild holding a pipe open cannot keep the run alive.
	killGrace = 2 * time.Second
	// outputTail is how much of a hook's output the failure message carries.
	// The end of the output is where the reason sits.
	outputTail = 4 << 10
)

// sourceThenExec sources env.sh in the shell that goes on to execute the hook,
// which is what "exports variables ahead of the rest" means. The hook keeps its
// own interpreter: exec honours its shebang.
const sourceThenExec = `. "$1" || exit 1; shift; exec "$@"`

// hooks are the scripts of the base ref, copied into the run directory.
type hooks struct {
	dir   string
	env   string
	up    string
	reset string
	test  string
	down  string
}

// discover reads what the export produced.
//
// The three required scripts have to be there. A base ref that carries
// .redfirst/ says the project meant tier 2, so half a hook set is a broken
// harness, exit code 2, rather than a capability that quietly goes missing.
func discover(dir string) (hooks, error) {
	h := hooks{dir: dir}
	executed := []struct {
		name     string
		into     *string
		required bool
	}{
		{hookUp, &h.up, true},
		{hookTest, &h.test, true},
		{hookDown, &h.down, true},
		{hookReset, &h.reset, false},
	}

	for _, s := range executed {
		path, found, err := runnable(dir, s.name)
		if err != nil {
			return hooks{}, err
		}
		if !found && s.required {
			return hooks{}, fmt.Errorf("%w: %s/%s is missing, and tier 2 runs nothing without it",
				domain.ErrHarness, HooksDir, s.name)
		}
		*s.into = path
	}

	// env.sh is sourced rather than executed, so it needs no executable bit.
	path, _, err := present(dir, hookEnv)
	if err != nil {
		return hooks{}, err
	}
	h.env = path
	return h, nil
}

// runnable is present plus the executable bit. A script committed without it is
// the specific thing to say: left to exec it would arrive as a permission error
// naming a path in a temporary directory nobody has heard of.
func runnable(dir, name string) (string, bool, error) {
	path, found, err := present(dir, name)
	if !found || err != nil {
		return "", found, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", false, fmt.Errorf("%w: reading %s/%s: %w", domain.ErrHarness, HooksDir, name, err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return "", false, fmt.Errorf("%w: %s/%s is not executable in the base ref",
			domain.ErrHarness, HooksDir, name)
	}
	return path, true, nil
}

// present reports where a hook is, and whether the base ref carries it at all.
func present(dir, name string) (string, bool, error) {
	path := filepath.Join(dir, name)
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("%w: reading %s/%s: %w", domain.ErrHarness, HooksDir, name, err)
	case info.IsDir():
		return "", false, fmt.Errorf("%w: %s/%s is a directory", domain.ErrHarness, HooksDir, name)
	}
	return path, true, nil
}

// outcome is what one hook invocation produced.
type outcome struct {
	// ok is the exit code being zero.
	ok bool
	// output is the tail of what the hook printed, for the message a failure
	// carries: a hook error has to name the specific problem.
	output string
}

// run executes one hook in dir and waits for it. It reports whether the script
// exited zero; a non-zero exit is an answer rather than a failure, and only a
// script that could not run at all, or one the deadline killed, is an error.
func (h hooks) run(ctx context.Context, script, dir string, env []string) (outcome, error) {
	var out tail
	cmd := h.command(ctx, script, dir, env)
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	if err == nil {
		return outcome{ok: true, output: out.report()}, nil
	}

	var exit *exec.ExitError
	if errors.As(err, &exit) && ctx.Err() == nil {
		return outcome{output: out.report()}, nil
	}
	return outcome{}, fmt.Errorf("%w: %s/%s: %w%s",
		domain.ErrHarness, HooksDir, filepath.Base(script), err, out.report())
}

// mustRun executes a hook whose whole contract is exit code 0. env-up.sh,
// env-reset.sh and env-down.sh have no verdict to give: a non-zero exit there
// is a broken harness, which is exit code 2 and not the agent's fault.
func (h hooks) mustRun(ctx context.Context, script, dir string, env []string) error {
	res, err := h.run(ctx, script, dir, env)
	if err != nil {
		return err
	}
	if !res.ok {
		return fmt.Errorf("%w: %s/%s exited non-zero%s",
			domain.ErrHarness, HooksDir, filepath.Base(script), res.output)
	}
	return nil
}

// command builds the invocation. The child gets a process group of its own, so
// a deadline reaches everything the hook started rather than the script alone.
func (h hooks) command(ctx context.Context, script, dir string, env []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, script)
	if h.env != "" {
		cmd = exec.CommandContext(ctx, "/bin/sh", "-c", sourceThenExec, "sh", h.env, script)
	}
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		// The leader dies through os.Process, which is the one call that cannot
		// land on a pid the operating system has already handed to somebody
		// else: it reports ErrProcessDone once Wait has reaped the child, and
		// os/exec reads that as "it finished on its own". Only once that signal
		// lands do we know the group id still names our children, and a
		// deadline has to reach them: killing the script alone leaves the
		// containers and the test runner it started behind.
		if err := cmd.Process.Kill(); err != nil {
			return err
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = killGrace
	return cmd
}

// tail keeps the last outputTail bytes written to it. A suite printing a
// megabyte must not land in memory whole, and the end is where the failure is.
type tail struct {
	buf []byte
}

func (t *tail) Write(p []byte) (int, error) {
	n := len(p)
	if len(p) > outputTail {
		p = p[len(p)-outputTail:]
	}
	t.buf = append(t.buf, p...)
	if len(t.buf) > outputTail {
		t.buf = t.buf[len(t.buf)-outputTail:]
	}
	return n, nil
}

// report is the hook's own output, ready to follow an error message. A hook
// that failed silently adds nothing.
func (t *tail) report() string {
	out := strings.TrimSpace(string(t.buf))
	if out == "" {
		return ""
	}
	return "\n" + out
}
