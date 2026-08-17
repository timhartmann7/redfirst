package config_test

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/timhartmann7/redfirst/internal/config"
	"github.com/timhartmann7/redfirst/internal/domain"
)

// failingReader delivers a prefix and then dies the way git does: the exit
// status of `git show` reaches the reader, not the opener, because git reports
// a missing or unfetchable blob only once the stream is already running.
type failingReader struct {
	prefix string
	err    error
	sent   bool
}

func (r *failingReader) Read(p []byte) (int, error) {
	if !r.sent {
		r.sent = true
		return copy(p, r.prefix), nil
	}
	return 0, r.err
}

func (r *failingReader) Close() error { return nil }

type brokenStreamSource struct{ err error }

func (brokenStreamSource) Exists(context.Context, string, string) (bool, error) { return true, nil }

func (s brokenStreamSource) ShowFile(context.Context, string, string) (io.ReadCloser, error) {
	return &failingReader{prefix: "version = 1\n", err: s.err}, nil
}

// TestConfig_GitFailureWhileStreamingIsExit2 separates a broken repository from
// a broken config. A corrupt object store or a blobless partial clone lets the
// tree entry exist while the blob does not, so the failure lands mid-stream.
// Calling that "the config is invalid" tells the orchestrator a retry is
// pointless when the run only needed repeating.
func TestConfig_GitFailureWhileStreamingIsExit2(t *testing.T) {
	t.Parallel()

	src := brokenStreamSource{
		err: fmt.Errorf("%w: git show main:redfirst.toml: fatal: bad object", domain.ErrHarness),
	}

	_, _, err := config.Load(t.Context(), src, "main", "redfirst.toml")
	if err == nil {
		t.Fatal("a dead stream was accepted as a config")
	}
	if got := domain.ExitCode(err); got != 2 {
		t.Errorf("exit code = %d, want 2: the repository broke, not the file (%v)", got, err)
	}
}

// TestConfig_BrokenTomlStaysExit3 is the other half of the pair: a stream that
// delivers real bytes and real nonsense is still a configuration error.
func TestConfig_BrokenTomlStaysExit3(t *testing.T) {
	t.Parallel()

	_, _, err := config.Load(t.Context(), withConfig("version = = 1"), "main", "redfirst.toml")
	if got := domain.ExitCode(err); got != 3 {
		t.Errorf("exit code = %d, want 3: the file exists and is invalid (%v)", got, err)
	}
}
