package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// ShowFile streams a file out of a ref. The caller closes it. Git reports a
// missing path only after it has started, so that failure surfaces from Read
// rather than from here.
func (r *Repo) ShowFile(ctx context.Context, ref, path string) (io.ReadCloser, error) {
	ctx, cancel := context.WithCancel(ctx)

	stream := &fileStream{args: []string{"show", ref + ":" + path}, cancel: cancel}
	cmd := r.command(ctx, stream.args...)
	cmd.Stderr = &stream.stderr
	stream.cmd = cmd

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("%w: git stdout pipe: %w", domain.ErrInternal, err)
	}
	stream.out = stdout

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, gitError(stream.args, err, stream.stderr.Bytes())
	}
	return stream, nil
}

// fileStream is one running `git show`. Reads come straight off its stdout,
// and the exit status joins the stream at EOF, which is where a missing path
// or an unknown ref shows up.
type fileStream struct {
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	out     io.ReadCloser
	stderr  bytes.Buffer
	args    []string
	waited  bool
	waitErr error
}

func (f *fileStream) Read(p []byte) (int, error) {
	n, err := f.out.Read(p)
	if errors.Is(err, io.EOF) {
		if waitErr := f.wait(); waitErr != nil {
			return n, waitErr
		}
	}
	return n, err
}

// Close reaps git. A caller that stopped before EOF gets the process group
// killed rather than left blocked on a pipe nobody reads.
func (f *fileStream) Close() error {
	early := !f.waited
	f.cancel()
	if early {
		f.waited = true
		_ = f.cmd.Wait()
		return nil
	}
	return f.waitErr
}

func (f *fileStream) wait() error {
	if f.waited {
		return f.waitErr
	}
	f.waited = true
	if err := f.cmd.Wait(); err != nil {
		f.waitErr = gitError(f.args, err, f.stderr.Bytes())
	}
	return f.waitErr
}
