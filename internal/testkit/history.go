package testkit

import (
	"fmt"
	"testing"
)

// The two fixtures below are histories rather than diffs: `redfirst audit`
// reads what a repository merged, so the shape of the history is the thing
// under test and the findings inside it have to be known exactly.

const auditTotalSource = `export function total(items) {
  return items.reduce((sum, item) => sum + item.price, 0)
}
`

const auditTotalFixed = `export function total(items) {
  return items.reduce((sum, item) => sum + item.price * (1 - (item.discount ?? 0)), 0)
}
`

// auditQuietSource swallows the error instead of fixing it: suppression-scan
// fills the reviewer block from lines like this one.
const auditQuietSource = `export function total(items) {
  try {
    return items.reduce((sum, item) => sum + item.price, 0)
  } catch (err) {}
}
`

const auditTotalTest = `import { total } from './total'

test('adds prices', () => {
  expect(total([{ price: 10 }])).toBe(10)
})
`

const auditDiscountCase = `
test('applies a discount', () => {
  expect(total([{ price: 10, discount: 0.5 }])).toBe(5)
})
`

const auditSkippedCase = `
test.skip('rounds the total', () => {
  expect(total([{ price: 0.1 }])).toBe(0.1)
})
`

const auditOnlyCase = `
test.only('drops an empty cart', () => {
  expect(total([])).toBe(0)
})
`

const auditLegacyTest = `test('the old way', () => {
  expect(1).toBe(1)
})
`

const auditWorkflow = `name: ci
on: [push]
`

// auditBigPRFiles is what it takes to pass the default budget of 20 files.
const auditBigPRFiles = 25

// AuditHistory builds the audit-history fixture: a repository whose pull
// requests land as merge commits. Every unit but the first carries exactly one
// kind of finding, so a summary over it is checkable by hand.
func AuditHistory(t *testing.T) *Repo {
	t.Helper()

	r := NewRepo(t)
	r.Write("README.md", "# shop\n")
	r.Write(".github/workflows/ci.yml", auditWorkflow)
	r.Write("src/total.js", auditTotalSource)
	r.Write("src/total.test.js", auditTotalTest)
	r.Write("src/legacy.test.js", auditLegacyTest)
	r.Commit("chore: seed the tree")

	pullRequest(r, 1, "discount", "fix: apply the item discount", func() {
		r.Write("src/total.js", auditTotalFixed)
		r.Write("src/total.test.js", auditTotalTest+auditDiscountCase)
	})
	pullRequest(r, 2, "skip", "test: park the rounding case", func() {
		// Appended, nothing removed: the unit trips no-disabled-tests alone.
		r.Write("src/total.test.js", auditTotalTest+auditDiscountCase+auditSkippedCase)
	})
	pullRequest(r, 3, "cleanup", "chore: drop the legacy test", func() {
		r.Remove("src/legacy.test.js")
	})
	pullRequest(r, 4, "infra", "ci: run on pull requests too", func() {
		r.Write(".github/workflows/ci.yml", auditWorkflow+"  pull_request:\n")
	})
	pullRequest(r, 5, "quiet", "fix: stop the total from throwing", func() {
		r.Write("src/total.js", auditQuietSource)
	})
	pullRequest(r, 6, "split", "refactor: split the order module", func() {
		for i := range auditBigPRFiles {
			r.Write(fmt.Sprintf("src/order/part%02d.js", i), fmt.Sprintf("export const part%02d = %d\n", i, i))
		}
	})
	pullRequest(r, 7, "only", "test: focus on the empty cart", func() {
		// The one unit that trips two gates: the focused case arrives and the
		// two cases before it disappear.
		r.Write("src/total.test.js", auditTotalTest+auditOnlyCase)
	})
	pullRequest(r, 8, "instructions", "docs: tell the agent about the shop", func() {
		// A refusal and a reviewer's line in one unit, over different files:
		// the summary counts both, and counts the two guarded files once.
		r.Write("CLAUDE.md", "# shop\n\nRun the tests before you commit.\n")
		r.Write(".github/workflows/ci.yml", auditWorkflow+"  workflow_dispatch:\n")
		r.Write("pnpm-lock.yaml", "lockfileVersion: '9.0'\n")
	})

	return r
}

// AuditSquashHistory builds the audit-squash-history fixture: the same kind of
// work landed as squashed commits, where the pull request number in the subject
// is the only trace of the merge left.
func AuditSquashHistory(t *testing.T) *Repo {
	t.Helper()

	r := NewRepo(t)
	r.Write("README.md", "# shop\n")
	r.Write(".github/workflows/ci.yml", auditWorkflow)
	r.Write("src/total.js", auditTotalSource)
	r.Write("src/total.test.js", auditTotalTest)
	r.Write("src/legacy.test.js", auditLegacyTest)
	r.Commit("chore: seed the tree")

	r.Write("src/total.js", auditTotalFixed)
	r.Write("src/total.test.js", auditTotalTest+auditDiscountCase)
	r.Commit("fix: apply the item discount (#11)")

	r.Write("src/total.test.js", auditTotalTest+auditDiscountCase+auditSkippedCase)
	r.Commit("test: park the rounding case (#12)")

	r.Write(".github/workflows/ci.yml", auditWorkflow+"  pull_request:\n")
	r.Commit("ci: run on pull requests too (#13)")

	r.Remove("src/legacy.test.js")
	r.Commit("chore: drop the legacy test (#14)")

	return r
}

// pullRequest lands one branch on base as a merge commit, the way a forge
// merges a pull request: the branch keeps its own commit and the merge carries
// the number.
func pullRequest(r *Repo, number int, branch, subject string, change func()) {
	r.t.Helper()

	r.Branch("feature/" + branch)
	change()
	r.Commit(subject)
	r.Checkout(FixtureBase)
	r.Merge("feature/"+branch, fmt.Sprintf("Merge pull request #%d from shop/%s", number, branch))
}
