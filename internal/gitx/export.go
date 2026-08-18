package gitx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// Export writes the contents of a tree into dir, creating it if it is missing.
// ref names a commit or a subtree of one (`<sha>:.redfirst`), whose contents
// land at the root of dir.
//
// The tree travels through a temporary index, and both obvious alternatives are
// closed to us:
//
//   - `git worktree add` records the checkout under .git/worktrees of the
//     repository being judged, and invariant 6 leaves redfirst nothing to write
//     there. A killed run would also leave that record behind.
//   - `git archive` applies the export-ignore and export-subst attributes of
//     the very tree it is archiving. One line of `.gitattributes` on the branch
//     under judgement would drop a failing test out of the working copy or
//     rewrite a file on its way in, and the suite would go green over a file
//     nobody ran. A working copy has to be the tree, byte for byte.
//
// Redirecting the index through GIT_INDEX_FILE keeps the repository untouched:
// read-tree writes the index we hand it and nothing else.
func (r *Repo) Export(ctx context.Context, ref, dir string) error {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("%w: resolve %s: %w", domain.ErrHarness, dir, err)
	}
	// checkout-index refuses to run without a work tree on disk.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("%w: create %s: %w", domain.ErrHarness, dir, err)
	}

	index, err := os.CreateTemp("", "redfirst-index-")
	if err != nil {
		return fmt.Errorf("%w: create a temporary git index: %w", domain.ErrHarness, err)
	}
	defer func() { _ = os.Remove(index.Name()) }()
	if err := index.Close(); err != nil {
		return fmt.Errorf("%w: create a temporary git index: %w", domain.ErrHarness, err)
	}

	env := []string{"GIT_INDEX_FILE=" + index.Name()}
	if _, err := r.runLineEnv(ctx, env, "read-tree", "--end-of-options", ref); err != nil {
		return fmt.Errorf("cannot read %q into an index: %w", ref, err)
	}
	if _, err := r.runLineEnv(ctx, env, "--work-tree="+dir, "checkout-index", "--all", "--force"); err != nil {
		return fmt.Errorf("cannot write %q into %s: %w", ref, dir, err)
	}
	return nil
}
