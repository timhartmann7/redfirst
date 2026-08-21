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
