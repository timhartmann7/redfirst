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

// Open resolves dir to the top level of its work tree.
//
// Anchoring at the top level is not tidiness. `git diff` names files relative
// to the repository root, while `ls-tree` and `show` resolve their paths
// against the current directory. Left at a subdirectory the two disagree: the
// diff covers the whole repository while redfirst.toml and .redfirst/ are
// looked up under the subdirectory prefix and never found, and the run judges
// a full diff by built-in defaults while reporting config=defaults as the
// truth. Invariant 5 says the rules come from base, so they have to be found.
func Open(ctx context.Context, dir string) (*Repo, error) {
	r := &Repo{dir: dir}

	top, err := r.runLine(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	if top == "" {
		return nil, fmt.Errorf("%w: %s is not inside a git work tree", domain.ErrHarness, dir)
	}
	r.dir = top
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

// ObjectID is the id of the object a path holds in a ref: a blob for a file, a
// tree for a directory. A tree id covers everything under it, so one call
// identifies the whole hook directory.
//
// A missing path is an error rather than an empty answer: the callers ask only
// for paths Exists has already found, and an id nobody could read is a broken
// repository.
func (r *Repo) ObjectID(ctx context.Context, ref, path string) (string, error) {
	id, err := r.runLine(ctx, "rev-parse", "--verify", "--end-of-options", ref+":"+path)
	if err != nil {
		return "", fmt.Errorf("cannot read the object id of %q in %q: %w", path, ref, err)
	}
	return id, nil
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
