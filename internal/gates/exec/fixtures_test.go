package exec_test

import (
	"context"
	"testing"

	"github.com/timhartmann7/redfirst/internal/config"
	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/gitx"
	"github.com/timhartmann7/redfirst/internal/harness"
	"github.com/timhartmann7/redfirst/internal/runner"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

// The fixtures below are repositories rather than tables. The executing gates
// judge what somebody else's test runner returned, so what they need is a tree
// the runner disagrees with: a case that passes without the fix says nothing on
// paper and everything once it has run.
//
// The `// case:` lines are the fixture harness reading its own test files; see
// internal/testkit/harness.go for the grammar.

const totalSource = `export function total(items) {
  return items.reduce((sum, item) => sum + item.price, 0)
}
`

// totalFixed carries the marker the honest case looks for, and totalSource does
// not. That difference is the whole of red-green.
const totalFixed = `export function total(items) {
  return items.reduce((sum, item) => sum + item.price * (1 - (item.discount ?? 0)), 0)
}
`

const totalTest = `import { total } from './total'

// case: adds prices
test('adds prices', () => {
  expect(total([{ price: 10 }])).toBe(10)
})
`

// honestCase is red without the fix and green with it.
const honestCase = `
// case: applies a discount | needs: discount
test('applies a discount', () => {
  expect(total([{ price: 10, discount: 0.5 }])).toBe(5)
})
`

// fakeCase asserts nothing the fix decides, so it is green on base already.
const fakeCase = `
// case: applies a discount
test('applies a discount', () => {
  expect(true).toBe(true)
})
`

// flakyCase follows the probe index: fail, pass, fail over three runs.
const flakyCase = `
// case: applies a discount | flaky
test('applies a discount', () => {})
`

// newAPITest does not build on base: the function it imports arrives with the
// fix, so the run dies before any assertion and names no case.
const newAPITest = `import { applyDiscount } from './discount'

// requires: applyDiscount
// case: applies a discount
test('applies a discount', () => {
  expect(applyDiscount(10, 0.5)).toBe(5)
})
`

const discountSource = `export function applyDiscount(price, rate) {
  return price * (1 - rate)
}
`

// legacyFlaky is a case the diff never touches. The agent does not answer for
// it, and suite-green has to say so.
const legacyFlaky = `// case: legacy rounding | flaky
test('legacy rounding', () => {})
`

// legacyBroken is red on head and red on base alike: the repository was already
// broken when the agent arrived.
const legacyBroken = `// case: legacy rounding | broken
test('legacy rounding', () => {})
`

// The three below make a case the diff broke without touching its test file:
// green on base, red on head, and nothing in the test file itself changed.
const legacyRounding = `// case: legacy rounding | needs: roundHalfUp
test('legacy rounding', () => {})
`

const legacySource = `export const roundHalfUp = (n) => Math.round(n)
`

const legacyRewritten = `export const roundToEven = (n) => Math.round(n)
`

// fixture is the shape of one repository under judgement.
type fixture struct {
	hooks testkit.Hooks
	// base runs on the base branch before the hooks are committed, head on the
	// branch under judgement.
	base func(r *testkit.Repo)
	head func(r *testkit.Repo)
	// cfg edits the built-in defaults the way a redfirst.toml on base would.
	cfg func(c *domain.Config)
	// only restricts the gate set, the way --gates does. A fixture reaches for
	// it where an earlier gate would refuse first and the short-circuit would
	// hide the answer under test.
	only []domain.GateID
}

// build assembles the repository and returns it on the head branch.
func (f fixture) build(t *testing.T) *testkit.Repo {
	t.Helper()

	r := testkit.NewRepo(t)
	r.Write("src/total.js", totalSource)
	r.Write("src/total.test.js", totalTest)
	if f.base != nil {
		f.base(r)
	}
	testkit.WriteHooks(r, f.hooks)
	r.Commit("feat: add the order total and the hooks")

	r.Branch(testkit.FixtureHead)
	f.head(r)
	r.Commit("fix: apply the item discount to the total")
	return r
}

func (f fixture) config(t *testing.T) domain.Config {
	t.Helper()

	cfg := config.Defaults()
	if f.cfg != nil {
		f.cfg(&cfg)
	}
	return cfg
}

// verdict is what one fixture run produced: the gate lines and the exit code
// the orchestrator would receive.
type verdict struct {
	gates map[domain.GateID]domain.GateResult
	err   error
	exit  int
}

// judge runs every gate over the fixture the way verify does, environment
// included. Nothing here is stubbed: the hooks are real scripts and the case
// names come out of what they wrote.
func (f fixture) judge(t *testing.T) verdict {
	t.Helper()

	repo := f.build(t)
	cfg := f.config(t)

	git, err := gitx.Open(t.Context(), repo.Dir)
	if err != nil {
		t.Fatalf("open %s: %v", repo.Dir, err)
	}
	diff, err := git.Diff(t.Context(), testkit.FixtureBase, testkit.FixtureHead)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}

	session, err := harness.Open(t.Context(), harness.Options{
		Source: git, Base: diff.Base, WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("open the session: %v", err)
	}
	defer func() {
		if err := session.Close(context.WithoutCancel(t.Context())); err != nil {
			t.Errorf("close the session: %v", err)
		}
	}()

	results, err := runner.Run(t.Context(), diff, runner.Options{
		Config: cfg,
		Only:   f.only,
		Caps:   runner.Capabilities(runner.Env{HooksPresent: true}, diff, runner.HarnessProbe{}, cfg),
		Open: func(context.Context) (domain.Probe, error) {
			return runner.NewProbe(session, git, diff, cfg), nil
		},
	})
	if err != nil {
		return verdict{err: err, exit: domain.ExitCode(err)}
	}

	outcome := runner.Outcome(results)
	v := verdict{gates: make(map[domain.GateID]domain.GateResult, len(results)), exit: domain.ExitCode(outcome)}
	for _, res := range results {
		v.gates[res.ID] = res
	}
	return v
}

// gate is one report line, and the test fails rather than reads a zero value
// where the run produced none.
func (v verdict) gate(t *testing.T, id domain.GateID) domain.GateResult {
	t.Helper()

	res, ok := v.gates[id]
	if !ok {
		t.Fatalf("the run carries no %s line (err %v)", id, v.err)
	}
	return res
}

// wantRefusal is one expectation about a gate that refused.
type wantRefusal struct {
	message     string
	remediation domain.Remediation
	exit        int
}

func (v verdict) assertRefusal(t *testing.T, id domain.GateID, want wantRefusal) domain.GateResult {
	t.Helper()

	got := v.gate(t, id)
	if got.Status != domain.StatusFail {
		t.Fatalf("%s is %q (%s %s), want a refusal", id, got.Status, got.Reason, got.Message)
	}
	if got.Message != want.message {
		t.Errorf("%s message = %q, want %q", id, got.Message, want.message)
	}
	if got.Remediation != want.remediation {
		t.Errorf("%s remediation = %q, want %q", id, got.Remediation, want.remediation)
	}
	if v.exit != want.exit {
		t.Errorf("exit code = %d, want %d", v.exit, want.exit)
	}
	return got
}

func (v verdict) assertPass(t *testing.T, id domain.GateID) domain.GateResult {
	t.Helper()

	got := v.gate(t, id)
	if got.Status != domain.StatusPass {
		t.Fatalf("%s is %q, want a pass: %s %s", id, got.Status, got.Reason, got.Message)
	}
	return got
}

// honestFix is the diff every fixture that is not about a refusal carries.
func honestFix(r *testkit.Repo) {
	r.Write("src/total.js", totalFixed)
	r.Write("src/total.test.js", totalTest+honestCase)
}

// unfixableCase is red whatever tree it runs on: the marker it needs is
// nowhere, so the fix does not make it pass.
const unfixableCase = `
// case: applies a discount | needs: nothingHereEver
test('applies a discount', () => {})
`

// flakyOnHeadCase is deterministic on base and undecided on head: red without
// the marker, and following the probe index once it arrives.
const flakyOnHeadCase = `
// case: applies a discount | flaky-with: discount
test('applies a discount', () => {})
`
