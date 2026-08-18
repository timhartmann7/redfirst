package main

import (
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/testkit"
)

const (
	digestSource = "export function total(items) {\n  return 0\n}\n"
	digestFixed  = "export function total(items) {\n  return items.length\n}\n"
	digestTest   = "test('counts', () => {\n  expect(total([1])).toBe(1)\n})\n"
	digestNotes  = "one\ntwo\nthree\nfour\n"
)

// digestRepo builds a repository whose head carries the change mutate wrote.
// Every fixture starts from the same base, so two of them differ in exactly
// what their head did.
func digestRepo(t *testing.T, message string, mutate func(r *testkit.Repo)) *testkit.Repo {
	t.Helper()
	return digestRepoFrom(t, digestSource, message, mutate)
}

// digestRepoFrom is digestRepo with the base content of src/total.js chosen by
// the caller: two fixtures can then reach one head tree from two bases.
func digestRepoFrom(t *testing.T, seed, message string, mutate func(r *testkit.Repo)) *testkit.Repo {
	t.Helper()

	r := testkit.NewRepo(t)
	r.Write("src/total.js", seed)
	r.Write("src/total.test.js", digestTest)
	r.Write("notes.txt", digestNotes)
	r.Commit("feat: seed the tree")

	r.Branch(testkit.FixtureHead)
	mutate(r)
	r.Commit(message)
	return r
}

func digestOf(t *testing.T, r *testkit.Repo, extra ...string) string {
	t.Helper()
	return runReport(t, r.Dir, extra...).DiffDigest
}

// TestDiffDigest_SamePatchAgreesAcrossAttempts is the property the orchestrator
// deduplicates on: the same tree written again is the same patch, whatever the
// commit around it looks like.
func TestDiffDigest_SamePatchAgreesAcrossAttempts(t *testing.T) {
	t.Parallel()

	fix := func(r *testkit.Repo) { r.Write("src/total.js", digestFixed) }
	first := digestOf(t, digestRepo(t, "fix: count the items", fix))
	second := digestOf(t, digestRepo(t, "fix: second attempt, same edit", fix))

	if first != second {
		t.Errorf("two attempts writing the same tree gave %q and %q", first, second)
	}
	if !strings.HasPrefix(first, "sha256:") {
		t.Errorf("digest = %q, want a named hash", first)
	}
}

// TestDiffDigest_DiffersWhenTheDiffDiffers walks the ways one attempt can
// differ from another. The last case is the reason the digest is built from
// object ids: the added line and both counts match, and only what got deleted
// tells the two patches apart.
func TestDiffDigest_DiffersWhenTheDiffDiffers(t *testing.T) {
	t.Parallel()

	base := digestOf(t, digestRepo(t, "fix: count the items", func(r *testkit.Repo) {
		r.Write("src/total.js", digestFixed)
		r.Write("notes.txt", "two\nthree\nfour\nfive\n")
	}))

	tests := []struct {
		name   string
		mutate func(r *testkit.Repo)
	}{
		{
			name: "a different edit to the same file",
			mutate: func(r *testkit.Repo) {
				r.Write("src/total.js", digestFixed+"\n")
				r.Write("notes.txt", "two\nthree\nfour\nfive\n")
			},
		},
		{
			name: "one more file in the diff",
			mutate: func(r *testkit.Repo) {
				r.Write("src/total.js", digestFixed)
				r.Write("notes.txt", "two\nthree\nfour\nfive\n")
				r.Write("src/extra.js", "export const extra = 1\n")
			},
		},
		{
			name: "the same line added and a different line deleted",
			mutate: func(r *testkit.Repo) {
				r.Write("src/total.js", digestFixed)
				r.Write("notes.txt", "one\nthree\nfour\nfive\n")
			},
		},
		{
			// The tree entry is the mode as much as the content: a retry that
			// only makes the file executable is a different patch, and an
			// orchestrator taking it for a repeat would throw away the fix.
			name: "the same content with the executable bit set",
			mutate: func(r *testkit.Repo) {
				r.WriteScript("src/total.js", digestFixed)
				r.Write("notes.txt", "two\nthree\nfour\nfive\n")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := digestOf(t, digestRepo(t, "fix: another attempt", tc.mutate)); got == base {
				t.Errorf("digest %q describes both patches", got)
			}
		})
	}
}

// TestDiffDigest_SeparatesRenamesByTheirSource covers the one field of a change
// that a head tree cannot show: two branches move a file of identical content
// onto the same destination, and only the path it came from tells them apart.
func TestDiffDigest_SeparatesRenamesByTheirSource(t *testing.T) {
	t.Parallel()

	twins := func(r *testkit.Repo) {
		r.Write("docs/one.md", digestNotes)
		r.Write("docs/two.md", digestNotes)
	}
	move := func(from string) *testkit.Repo {
		r := testkit.NewRepo(t)
		twins(r)
		r.Commit("docs: write the two notes")

		r.Branch(testkit.FixtureHead)
		r.Rename(from, "docs/merged.md")
		r.Commit("docs: merge the notes")
		return r
	}

	first, second := digestOf(t, move("docs/one.md")), digestOf(t, move("docs/two.md"))
	if first == second {
		t.Errorf("two renames onto one path share the digest %q", first)
	}
}

// TestDiffDigest_SeparatesOneHeadTreeReachedFromTwoBases covers the commit the
// digest opens with. The same head content written over two different bases is
// two different patches, and every other field of the two records agrees.
func TestDiffDigest_SeparatesOneHeadTreeReachedFromTwoBases(t *testing.T) {
	t.Parallel()

	fix := func(r *testkit.Repo) { r.Write("src/total.js", digestFixed) }
	first := digestOf(t, digestRepoFrom(t, digestSource, "fix: count the items", fix))
	second := digestOf(t, digestRepoFrom(t, digestSource+"\n", "fix: count the items", fix))

	if first == second {
		t.Errorf("two bases under one head tree share the digest %q", first)
	}
}

// TestBaseProbeKey_NamesTheProbeItsInputsDecide covers what the key stands for:
// the tree the probe runs on, the hooks that run it and the test files overlaid
// onto it. Change any of the three and the key has to move.
func TestBaseProbeKey_NamesTheProbeItsInputsDecide(t *testing.T) {
	t.Parallel()

	repo := testkit.HookedFix(t)
	first := runReport(t, repo.Dir).BaseProbeKey
	if first == "" {
		t.Fatal("a diff with hooks and test files carries no base probe key")
	}
	if second := runReport(t, repo.Dir).BaseProbeKey; second != first {
		t.Errorf("the same run gave %q and then %q", first, second)
	}

	repo.Write("src/total.test.js", "test('other', () => {})\n")
	repo.Commit("test: rewrite the case")
	afterTest := runReport(t, repo.Dir).BaseProbeKey
	if afterTest == first {
		t.Error("the key survived a change to the test file the probe overlays")
	}

	repo.Checkout(testkit.FixtureBase)
	repo.WriteScript(".redfirst/test.sh", "#!/bin/sh\necho rewritten\n")
	repo.Commit("chore: rewrite the test hook")
	repo.Checkout(testkit.FixtureHead)
	// Against afterTest, not against first: the key already moved for the test
	// file, so comparing with first would pass whatever the hooks did, and the
	// one input that separates this key from a diff digest would go unchecked.
	if afterHook := runReport(t, repo.Dir).BaseProbeKey; afterHook == afterTest {
		t.Error("the key survived a change to the hooks that run the probe")
	}
}

// TestBaseProbeKey_AbsentWhereNoProbeWouldRun keeps the key from describing a
// probe that never happens: without hooks or without test files in the diff,
// red-green is unavailable and there is nothing to identify.
func TestBaseProbeKey_AbsentWhereNoProbeWouldRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		repo  func(t *testing.T) *testkit.Repo
		extra []string
	}{
		{
			name: "no hooks on base",
			repo: testkit.CleanFix,
		},
		{
			name:  "hooks refused with --no-hooks",
			repo:  testkit.HookedFix,
			extra: []string{"--no-hooks"},
		},
		{
			name: "hooks, but the diff touches no test file",
			repo: func(t *testing.T) *testkit.Repo {
				t.Helper()

				r := testkit.NewRepo(t)
				r.Write("src/total.js", digestSource)
				r.Write("src/total.test.js", digestTest)
				r.WriteScript(".redfirst/env-up.sh", "#!/bin/sh\n")
				r.WriteScript(".redfirst/test.sh", "#!/bin/sh\n")
				r.WriteScript(".redfirst/env-down.sh", "#!/bin/sh\n")
				r.Commit("feat: seed the tree with hooks")

				r.Branch(testkit.FixtureHead)
				r.Write("src/total.js", digestFixed)
				r.Commit("fix: touch the sources alone")
				return r
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rep := runReport(t, tc.repo(t).Dir, tc.extra...)
			if rep.BaseProbeKey != "" {
				t.Errorf("base_probe_key = %q where no probe would run", rep.BaseProbeKey)
			}
			if rep.DiffDigest == "" {
				t.Error("diff_digest is empty: every run judges a patch")
			}
		})
	}
}
