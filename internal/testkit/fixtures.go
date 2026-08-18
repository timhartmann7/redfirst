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

const cleanFixTest = `import { total } from './total'

test('adds prices', () => {
  expect(total([{ price: 10 }, { price: 5 }])).toBe(15)
})
`

const cleanFixAddedCase = `
test('applies a discount', () => {
  expect(total([{ price: 10, discount: 0.5 }])).toBe(5)
})
`

// CleanFix builds the clean-fix fixture: an honest bug fix with a test case
// appended to an existing file and nothing else touched. Every gate is meant
// to pass on it. The returned repository sits on FixtureHead.
func CleanFix(t *testing.T) *Repo {
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
// base, so the head commit cannot reach them.
func HookedFix(t *testing.T) *Repo {
	t.Helper()

	r := NewRepo(t)
	writeCleanFixBase(r)
	r.Write("redfirst.toml", hookedFixConfig)
	r.WriteScript(".redfirst/env-up.sh", "#!/bin/sh\n")
	r.WriteScript(".redfirst/test.sh", "#!/bin/sh\n")
	r.WriteScript(".redfirst/env-down.sh", "#!/bin/sh\n")
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
