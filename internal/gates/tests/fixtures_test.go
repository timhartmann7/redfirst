package tests_test

import (
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/gitx"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

// The fixture repositories of this slice get assembled here through the shared
// builder in internal/testkit. The table tests judge the gates from (Diff,
// Config) alone; these exist because git decides the facts they carry: a line
// is an added line because git says the diff added it, and it sits at the
// number git gives it on head.

const fixtureSource = `export function total(items) {
  let sum = 0
  for (const item of items) {
    sum += item.price
  }
  return sum
}
`

const fixtureFixedSource = `export function total(items) {
  let sum = 0
  for (const item of items) {
    sum += item.price * (1 - (item.discount ?? 0))
  }
  return sum
}
`

const fixtureTest = `import { total } from './total'

test('adds prices', () => {
  expect(total([{ price: 10 }, { price: 5 }])).toBe(15)
})
`

// fixtureRepo is the base every fixture of this slice forks from: one source
// file and one test file that covers it.
func fixtureRepo(t *testing.T) *testkit.Repo {
	t.Helper()

	r := testkit.NewRepo(t)
	r.Write("src/total.js", fixtureSource)
	r.Write("src/total.test.js", fixtureTest)
	r.Commit("feat: add the order total")
	r.Branch(testkit.FixtureHead)
	return r
}

// fixtureDiff builds the diff the way verify does: merge base to head.
func fixtureDiff(t *testing.T, r *testkit.Repo) domain.Diff {
	t.Helper()

	repo, err := gitx.Open(t.Context(), r.Dir)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	d, err := repo.Diff(t.Context(), testkit.FixtureBase, testkit.FixtureHead)
	if err != nil {
		t.Fatalf("diff fixture: %v", err)
	}
	return d
}

const fixtureSkippedCase = `
test.skip('applies a discount', () => {
  expect(total([{ price: 10, discount: 0.5 }])).toBe(5)
})
`

// skippedTest is the fixture of the same name: a test written and switched off
// in the same breath, which satisfies new-test-present and proves nothing.
func skippedTest(t *testing.T) domain.Diff {
	t.Helper()

	r := fixtureRepo(t)
	r.Write("src/total.test.js", fixtureTest+fixtureSkippedCase)
	r.Write("src/total.js", fixtureFixedSource)
	r.Commit("fix: apply the item discount to the total")

	return fixtureDiff(t, r)
}

// fileOf picks one change out of a diff by the path it had at the merge base,
// and keeps a fixture honest: a case that assumes a deletion says so here
// rather than passing on a diff that turned out to hold something else.
func fileOf(t *testing.T, d domain.Diff, path string, status domain.FileStatus) domain.FileChange {
	t.Helper()

	for _, f := range d.Files {
		if f.Path != path && f.OldPath != path {
			continue
		}
		if f.Status != status {
			t.Fatalf("%s carries status %q, want %q", path, f.Status, status)
		}
		return f
	}
	t.Fatalf("the fixture diff holds no %s, only %+v", path, d.Files)
	return domain.FileChange{}
}

// lineOf is the number an added line has on head, which is what the report
// prints and what a reviewer clicks.
func lineOf(t *testing.T, f domain.FileChange, substring string) int {
	t.Helper()

	for _, l := range f.AddedLines {
		if strings.Contains(l.Text, substring) {
			return l.Number
		}
	}
	t.Fatalf("no added line of %s holds %q, only %+v", f.Path, substring, f.AddedLines)
	return 0
}
