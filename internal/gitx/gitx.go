// Package gitx is the git adapter: every fact about the repository under
// judgement travels through here. Git runs as the system binary and never as a
// Go library, so redfirst sees exactly what a developer sees by hand.
package gitx

import (
	"context"
	"fmt"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// Repo is a git repository reached through the system git binary.
type Repo struct {
	dir string
}

// Open verifies that dir is inside a git work tree.
func Open(dir string) (*Repo, error) {
	r := &Repo{dir: dir}

	// One bounded call before the caller holds any context of its own: this is
	// the point where a wrong directory has to be caught.
	inside, err := r.runLine(context.Background(), "rev-parse", "--is-inside-work-tree")
	if err != nil {
		return nil, err
	}
	if inside != "true" {
		return nil, fmt.Errorf("%w: %s is not inside a git work tree", domain.ErrHarness, dir)
	}
	return r, nil
}

// Resolve turns a ref into a commit sha. Annotated tags get peeled, so the
// caller always holds a commit and never a tag object.
func (r *Repo) Resolve(ctx context.Context, ref string) (string, error) {
	sha, err := r.runLine(ctx, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("cannot resolve ref %q: %w", ref, err)
	}
	return sha, nil
}

// MergeBase is the commit the branch forked from.
func (r *Repo) MergeBase(ctx context.Context, a, b string) (string, error) {
	sha, err := r.runLine(ctx, "merge-base", "--end-of-options", a, b)
	if err != nil {
		return "", fmt.Errorf("no merge base for %q and %q: %w", a, b, err)
	}
	return sha, nil
}

// Exists reports whether a path is present in a ref, file or directory alike.
// An absent path is an answer rather than a failure; an unknown ref is not.
func (r *Repo) Exists(ctx context.Context, ref, path string) (bool, error) {
	// --literal-pathspecs: a path holding a glob character names that one file
	// and nothing else, otherwise git would answer for whatever it matched.
	out, err := r.runLine(ctx, "--literal-pathspecs", "ls-tree", "--name-only", "-z", ref, "--", path)
	if err != nil {
		return false, fmt.Errorf("cannot look up %q in %q: %w", path, ref, err)
	}
	// The name itself carries nothing: an entry printed at all means the path
	// is in the tree.
	return out != "", nil
}
