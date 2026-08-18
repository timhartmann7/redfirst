package gitx_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/gitx"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

// openFixture opens the repository the builder has just assembled.
func openFixture(t *testing.T, r *testkit.Repo) *gitx.Repo {
	t.Helper()

	repo, err := gitx.Open(t.Context(), r.Dir)
	if err != nil {
		t.Fatalf("open %s: %v", r.Dir, err)
	}
	return repo
}

// diffFixture takes the diff every fixture is built for: base to head, with the
// tree entry of the file on head dropped. Mode and object id are content rather
// than shape, and a want literal carrying a hash would say nothing to a reader;
// TestGitx_CarriesTheHeadTreeEntry checks them on their own.
func diffFixture(t *testing.T, r *testkit.Repo) domain.Diff {
	t.Helper()

	d := rawDiffFixture(t, r)
	for i := range d.Files {
		d.Files[i].Mode, d.Files[i].Object = "", ""
	}
	return d
}

// rawDiffFixture is diffFixture with everything git reported left in place.
func rawDiffFixture(t *testing.T, r *testkit.Repo) domain.Diff {
	t.Helper()

	d, err := openFixture(t, r).Diff(t.Context(), testkit.FixtureBase, testkit.FixtureHead)
	if err != nil {
		t.Fatalf("diff %s to %s: %v", testkit.FixtureBase, testkit.FixtureHead, err)
	}
	return d
}

// fileByPath picks one change out of a diff by its head path.
func fileByPath(t *testing.T, d domain.Diff, path string) domain.FileChange {
	t.Helper()

	for _, f := range d.Files {
		if f.Path == path {
			return f
		}
	}
	t.Fatalf("diff holds no file %q, only %v", path, headPaths(d))
	return domain.FileChange{}
}

func headPaths(d domain.Diff) []string {
	paths := make([]string, 0, len(d.Files))
	for _, f := range d.Files {
		paths = append(paths, f.Path)
	}
	return paths
}

// twoBranches is the smallest fixture with a fork: one file changed on head.
func twoBranches(t *testing.T) *testkit.Repo {
	t.Helper()

	r := testkit.NewRepo(t)
	r.Write("src/total.js", "export const rate = 1\n")
	r.Write("docs/guide.md", "# guide\n")
	r.Commit("feat: seed the tree")

	r.Branch(testkit.FixtureHead)
	r.Write("src/total.js", "export const rate = 2\n")
	r.Commit("fix: correct the rate")

	return r
}

func TestGitx_OpenRejectsNonRepository(t *testing.T) {
	t.Parallel()

	if _, err := gitx.Open(t.Context(), t.TempDir()); !errors.Is(err, domain.ErrHarness) {
		t.Fatalf("open of a plain directory: %v, want a harness error", err)
	}
}

func TestGitx_ResolveReturnsTheCommitOfABranch(t *testing.T) {
	t.Parallel()

	r := twoBranches(t)
	repo := openFixture(t, r)
	r.Checkout(testkit.FixtureBase)

	sha, err := repo.Resolve(t.Context(), testkit.FixtureBase)
	if err != nil {
		t.Fatalf("resolve %s: %v", testkit.FixtureBase, err)
	}
	if want := r.Head(); sha != want {
		t.Errorf("resolve %s = %s, want %s", testkit.FixtureBase, sha, want)
	}
}

func TestGitx_MergeBaseIsTheForkPoint(t *testing.T) {
	t.Parallel()

	r := testkit.NewRepo(t)
	r.Write("src/total.js", "export const rate = 1\n")
	fork := r.Commit("feat: seed the tree")

	r.Branch(testkit.FixtureHead)
	r.Write("src/total.js", "export const rate = 2\n")
	r.Commit("fix: correct the rate")

	r.Checkout(testkit.FixtureBase)
	r.Write("CHANGELOG.md", "# changelog\n")
	r.Commit("docs: start a changelog")

	got, err := openFixture(t, r).MergeBase(t.Context(), testkit.FixtureBase, testkit.FixtureHead)
	if err != nil {
		t.Fatalf("merge-base: %v", err)
	}
	if got != fork {
		t.Errorf("merge-base = %s, want the fork commit %s", got, fork)
	}
}

func TestGitx_UnresolvableRefIsHarnessError(t *testing.T) {
	t.Parallel()

	repo := openFixture(t, twoBranches(t))
	ctx := t.Context()

	tests := []struct {
		name string
		call func() error
	}{
		{"resolve", func() error {
			_, err := repo.Resolve(ctx, "no/such/ref")
			return err
		}},
		{"merge base", func() error {
			_, err := repo.MergeBase(ctx, testkit.FixtureBase, "no/such/ref")
			return err
		}},
		{"diff", func() error {
			_, err := repo.Diff(ctx, testkit.FixtureBase, "no/such/ref")
			return err
		}},
		{"exists", func() error {
			_, err := repo.Exists(ctx, "no/such/ref", "src/total.js")
			return err
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.call(); !errors.Is(err, domain.ErrHarness) {
				t.Fatalf("%s on an unknown ref: %v, want a harness error", tc.name, err)
			}
		})
	}
}

func TestGitx_CancelledContextIsHarnessError(t *testing.T) {
	t.Parallel()

	repo := openFixture(t, twoBranches(t))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := repo.Diff(ctx, testkit.FixtureBase, testkit.FixtureHead); !errors.Is(err, domain.ErrHarness) {
		t.Fatalf("diff on a cancelled context: %v, want a harness error", err)
	}
}

func TestGitx_ExistsFindsFilesAndDirectories(t *testing.T) {
	t.Parallel()

	repo := openFixture(t, twoBranches(t))
	tests := []struct {
		name string
		path string
	}{
		{"a file at the root of a directory", "src/total.js"},
		{"a directory", "src"},
		{"a file next to the changed one", "docs/guide.md"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ok, err := repo.Exists(t.Context(), testkit.FixtureBase, tc.path)
			if err != nil {
				t.Fatalf("exists %q: %v", tc.path, err)
			}
			if !ok {
				t.Errorf("exists %q = false, want true", tc.path)
			}
		})
	}
}

func TestGitx_ExistsIsFalseForMissingPath(t *testing.T) {
	t.Parallel()

	repo := openFixture(t, twoBranches(t))
	// An absent path is an answer rather than a failure, whatever shape it has.
	tests := []struct {
		name string
		path string
	}{
		{"a file nobody ever committed", "nowhere.txt"},
		{"a file inside a directory that exists", "src/nowhere.js"},
		{"a directory that is not there", "src/nested"},
		{"a glob that would match a real file", "src/*.js"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ok, err := repo.Exists(t.Context(), testkit.FixtureBase, tc.path)
			if err != nil {
				t.Fatalf("exists %q: %v", tc.path, err)
			}
			if ok {
				t.Errorf("exists %q = true, want false", tc.path)
			}
		})
	}
}

func TestGitx_ShowFileStreamsFileFromBase(t *testing.T) {
	t.Parallel()

	repo := openFixture(t, twoBranches(t))
	stream, err := repo.ShowFile(t.Context(), testkit.FixtureBase, "src/total.js")
	if err != nil {
		t.Fatalf("show file: %v", err)
	}
	defer func() {
		if cerr := stream.Close(); cerr != nil {
			t.Errorf("close: %v", cerr)
		}
	}()

	// One byte at a time: the reader has to hand back the exit status at EOF
	// and not one byte earlier.
	var got strings.Builder
	if _, err := io.CopyBuffer(&got, stream, make([]byte, 1)); err != nil {
		t.Fatalf("read: %v", err)
	}
	if want := "export const rate = 1\n"; got.String() != want {
		t.Errorf("base content = %q, want %q", got.String(), want)
	}
}

func TestGitx_ShowFileMissingPathIsHarnessError(t *testing.T) {
	t.Parallel()

	repo := openFixture(t, twoBranches(t))
	stream, err := repo.ShowFile(t.Context(), testkit.FixtureBase, "src/nowhere.js")
	if err != nil {
		t.Fatalf("show file: %v", err)
	}
	defer func() { _ = stream.Close() }()

	if _, err := io.ReadAll(stream); !errors.Is(err, domain.ErrHarness) {
		t.Fatalf("read of a missing path: %v, want a harness error", err)
	}
}
