package testkit

import "testing"

// Branch names shared by every fixture repository, so a test never spells them
// out twice.
const (
	FixtureBase = "main"
	FixtureHead = "fix/discount"
)

const cleanFixSource = `export function total(items) {
  let sum = 0
  for (const item of items) {
    sum += item.price
  }
  return sum
}
`

const cleanFixFixedSource = `export function total(items) {
  let sum = 0
  for (const item of items) {
    sum += item.price * (1 - (item.discount ?? 0))
  }
  return sum
}
`

// The `// case:` lines are what the fixture harness reads; see harness.go.
// A reader of the fixture sees the same two cases the runner reports.
const cleanFixTest = `import { total } from './total'

// case: adds prices
test('adds prices', () => {
  expect(total([{ price: 10 }, { price: 5 }])).toBe(15)
})
`

const cleanFixAddedCase = `
// case: applies a discount | needs: discount
test('applies a discount', () => {
  expect(total([{ price: 10, discount: 0.5 }])).toBe(5)
})
`

// CleanFix builds the clean-fix fixture: an honest bug fix with a test case
// appended to an existing file and nothing else touched. Every gate is meant
// to pass on it. The returned repository sits on FixtureHead.
func CleanFix(t testing.TB) *Repo {
	t.Helper()

	r := NewRepo(t)
	writeCleanFixBase(r)
	r.Commit("feat: add the order total")

	r.Branch(FixtureHead)
	r.Write("src/total.js", cleanFixFixedSource)
	r.Write("src/total.test.js", cleanFixTest+cleanFixAddedCase)
	r.Commit("fix: apply the item discount to the total")

	return r
}

const hookedFixConfig = `version = 1

[tests]
immutability = "cases"
`

// HookedFix is clean-fix with the tier 2 files added to base: a .redfirst/
// directory and a config asking for immutability = "cases". Both come from
// base, so the head commit cannot reach them. Every gate passes on it, the two
// that run the harness included.
func HookedFix(t testing.TB) *Repo {
	t.Helper()

	r := NewRepo(t)
	writeCleanFixBase(r)
	r.Write("redfirst.toml", hookedFixConfig)
	WriteHooks(r, Hooks{})
	r.Commit("feat: add the order total and the redfirst hooks")

	r.Branch(FixtureHead)
	r.Write("src/total.js", cleanFixFixedSource)
	r.Write("src/total.test.js", cleanFixTest+cleanFixAddedCase)
	r.Commit("fix: apply the item discount to the total")

	return r
}

func writeCleanFixBase(r *Repo) {
	r.Write("README.md", "# shop\n")
	r.Write("src/total.js", cleanFixSource)
	r.Write("src/total.test.js", cleanFixTest)
}

const renamedSymbolSource = `export function orderTotal(items) {
  let sum = 0
  for (const item of items) {
    sum += item.price
  }
  return sum
}
`

// renamedSymbolTest is the same case under a new import. The case name does not
// move with the symbol, which is the whole reason mode cases exists.
const renamedSymbolTest = `import { orderTotal } from './total'

// case: adds prices
test('adds prices', () => {
  expect(orderTotal([{ price: 10 }, { price: 5 }])).toBe(15)
})
`

// RenamedSymbol builds the renamed-symbol fixture: a refactor that renames a
// function the tests cover, so the test file loses the lines carrying the old
// name.
//
// strict refuses the edit outright and append-only refuses it on the deleted
// lines, which leaves the agent no legal diff for a task that is entirely
// legitimate. The base ref asks for immutability = "cases", which judges the
// outcome instead: the case name survives, so the refactor passes.
func RenamedSymbol(t testing.TB) *Repo {
	t.Helper()

	r := NewRepo(t)
	writeCleanFixBase(r)
	r.Write("redfirst.toml", hookedFixConfig)
	WriteHooks(r, Hooks{})
	r.Commit("feat: add the order total and the redfirst hooks")

	r.Branch(FixtureHead)
	r.Write("src/total.js", renamedSymbolSource)
	r.Write("src/total.test.js", renamedSymbolTest)
	r.Commit("refactor: rename total to orderTotal")

	return r
}
