package exec_test

import (
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

// TestRedGreen_ACaseNameTwoFilesShareIsARefusal closes the channel a set keyed
// on the bare name leaves open.
//
// The diff adds two cases under one name: an honest one, red on base and green
// on head, and a fake one green on both. The two collapse into a single entry,
// the honest one answers for both, and the fake reaches a human with a green
// verdict. Duplicate case names across files are ordinary — `go test` reports
// the package as the classname, so TestParse in two packages is one name and
// two cases.
func TestRedGreen_ACaseNameTwoFilesShareIsARefusal(t *testing.T) {
	t.Parallel()

	v := fixture{head: func(r *testkit.Repo) {
		r.Write("src/total.js", totalFixed)
		// Sorted order decides which one the name would resolve to, and the
		// honest one has to come first for the fake to be the one lost.
		r.Write("src/a-discount.test.js", honestCase)
		r.Write("src/z-note.test.js", fakeCase)
	}}.judge(t)

	got := v.assertRefusal(t, domain.GateRedGreen, wantRefusal{
		message:     "two test files carry one case name, the added case cannot be told from the other",
		remediation: domain.RemediationFixTest,
		exit:        1,
	})

	if len(got.Evidence) != 1 {
		t.Fatalf("evidence = %v, want the one name two files carry", got.Evidence)
	}
	e := got.Evidence[0]
	if e.Case != "applies a discount" {
		t.Errorf("evidence names %q, want the shared case name", e.Case)
	}
	want := "the name is carried by src/a-discount.test.js and src/z-note.test.js"
	if e.Detail != want {
		t.Errorf("detail = %q, want %q", e.Detail, want)
	}
}

// TestRedGreen_CompileFailureOnBaseWithoutANewCaseIsARefusal covers the row of
// the step 7 table an empty C_new falls under.
//
// The diff edits an existing test file so that it no longer builds on base and
// adds no case at all. The base probe then names nothing, the head run's names
// stand in for C_new, and every one of them was already there. That is "test
// files changed but no new test case appeared", not a pass: falling back to the
// file set here would clear any diff whose test edit merely fails to build
// without the fix.
func TestRedGreen_CompileFailureOnBaseWithoutANewCaseIsARefusal(t *testing.T) {
	t.Parallel()

	v := fixture{head: func(r *testkit.Repo) {
		r.Write("src/total.js", totalFixed)
		r.Write("src/discount.js", discountSource)
		// A file-level requirement base cannot meet, and not one new case.
		r.Write("src/total.test.js", "// requires: applyDiscount\n"+totalTest)
	}}.judge(t)

	v.assertRefusal(t, domain.GateRedGreen, wantRefusal{
		message:     "test files changed but no new test case appeared",
		remediation: domain.RemediationAddTest,
		exit:        1,
	})
}
