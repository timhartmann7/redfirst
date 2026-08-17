package gitx

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// killGrace is how long os/exec keeps waiting on the output pipes after the
// context died. Past it the copying goroutines are abandoned, so a grandchild
// holding the pipe open cannot keep a verify run alive forever.
const killGrace = 2 * time.Second

// command builds a git invocation rooted at the repository. The child gets its
// own process group and a Cancel that signals the group rather than the
// leader: killing git alone would leave whatever it started running.
func (r *Repo) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", r.dir}, args...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = killGrace
	return cmd
}

// runLine executes git and returns its output as one trimmed line. It serves
// the short answers, a sha or a yes and no; diff output goes through runStream.
func (r *Repo) runLine(ctx context.Context, args ...string) (string, error) {
	cmd := r.command(ctx, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", gitError(args, err, stderr.Bytes())
	}
	return strings.TrimSpace(stdout.String()), nil
}

// runStream executes git and hands its stdout to parse one NUL-terminated
// record at a time. Diff output is never read whole: a thousand-file diff must
// not land in memory as one string.
func (r *Repo) runStream(ctx context.Context, parse func(*bufio.Scanner) error, args ...string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	cmd := r.command(ctx, args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("%w: git stdout pipe: %w", domain.ErrInternal, err)
	}
	if err := cmd.Start(); err != nil {
		return gitError(args, err, stderr.Bytes())
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Split(scanNUL)
	perr := parse(scanner)
	if perr == nil {
		perr = scanner.Err()
	}
	if perr != nil {
		// Walking away mid-stream would leave git blocked on a write nobody
		// reads, so the process group dies before we wait on it.
		cancel()
	}
	if err := cmd.Wait(); err != nil && perr == nil {
		return gitError(args, err, stderr.Bytes())
	}
	return perr
}

// gitError reports a failed invocation as a harness failure: the repository or
// the environment is broken, which is exit code 2 and not the agent's fault.
func gitError(args []string, err error, stderr []byte) error {
	detail := strings.TrimSpace(string(stderr))
	if detail == "" {
		detail = err.Error()
	}
	return fmt.Errorf("%w: git %s: %s", domain.ErrHarness, strings.Join(args, " "), detail)
}

// scanNUL splits git's -z output. NUL is the one byte a path cannot hold, so a
// path carrying a newline or a quote still arrives as a single record.
func scanNUL(data []byte, atEOF bool) (int, []byte, error) {
	if i := bytes.IndexByte(data, 0); i >= 0 {
		return i + 1, data[:i], nil
	}
	if !atEOF {
		return 0, nil, nil
	}
	if len(data) > 0 {
		return 0, nil, fmt.Errorf("%w: git output ends without a NUL terminator: %q",
			domain.ErrInternal, data)
	}
	return 0, nil, nil
}

// next pulls the record that has to follow the one just read. Git cutting a
// record group short is our own parsing bug rather than a broken repository.
func next(s *bufio.Scanner, what string) (string, error) {
	if !s.Scan() {
		if err := s.Err(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("%w: git output ends before %s", domain.ErrInternal, what)
	}
	return s.Text(), nil
}
