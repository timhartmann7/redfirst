package exec_test

import (
	"slices"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

// TestRedGreen_CleanFixPasses is the honest scenario: one case added to an
// existing test file, red on base because the fix is missing and green on head
// because it is not.
func TestRedGreen_CleanFixPasses(t *testing.T) {
	t.Parallel()

	v := fixture{head: honestFix}.judge(t)

	got := v.assertPass(t, domain.GateRedGreen)
	if got.Message != "1 new case red on base, green on head" {
		t.Errorf("message = %q, want the count of what it proved", got.Message)
	}
	if v.exit != 0 {
		t.Errorf("exit code = %d, want 0", v.exit)
	}
}

// TestRedGreen_GreenOnBaseIsARefusal is the fixture the whole gate exists for.
// A test that passes without the fix proves nothing, and the verdict may not
// come out zero.
func TestRedGreen_GreenOnBaseIsARefusal(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// fake-test puts the fake case in a file of its own,
		// fake-case-in-existing-file appends it to a file that already exists.
		// A gate that probed files would find nothing to probe in the second.
		head func(r *testkit.Repo)
		file string
	}{
		{
			name: "InANewTestFile",
			head: func(r *testkit.Repo) {
				r.Write("src/total.js", totalFixed)
				r.Write("src/discount.test.js", fakeCase)
			},
			file: "src/discount.test.js",
		},
		{
			name: "AppendedToAFileThatAlreadyExisted",
			head: func(r *testkit.Repo) {
				r.Write("src/total.js", totalFixed)
				r.Write("src/total.test.js", totalTest+fakeCase)
			},
			file: "src/total.test.js",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			v := fixture{head: tc.head}.judge(t)
			got := v.assertRefusal(t, domain.GateRedGreen, wantRefusal{
				message:     "test is green on base, it does not cover the fix",
				remediation: domain.RemediationFixTest,
				exit:        1,
			})

			if len(got.Evidence) != 1 {
				t.Fatalf("evidence = %v, want the one case that failed to fail", got.Evidence)
			}
			e := got.Evidence[0]
			if e.Case != "applies a discount" || e.File != tc.file {
				t.Errorf("evidence names %q in %q, want the case in %q", e.Case, e.File, tc.file)
			}
			if !slices.Equal(e.BaseRuns, []string{"pass", "pass", "pass"}) {
				t.Errorf("base runs = %v, want a pass in every one of them", e.BaseRuns)
			}
		})
	}
}

// TestRedGreen_ModifiedTestFileWithoutANewCaseIsARefusal covers no-new-case: the
// diff rewrote a test file and left nothing behind that could catch a bug.
func TestRedGreen_ModifiedTestFileWithoutANewCaseIsARefusal(t *testing.T) {
	t.Parallel()

	v := fixture{head: func(r *testkit.Repo) {
		r.Write("src/total.js", totalFixed)
		// Lines added and none removed, so append-only is satisfied and the
		// gate that follows it is the one with something to say.
		r.Write("src/total.test.js", totalTest+"\n// the fix needs no new case, apparently\n")
	}}.judge(t)

	v.assertRefusal(t, domain.GateRedGreen, wantRefusal{
		message:     "test files changed but no new test case appeared",
		remediation: domain.RemediationAddTest,
		exit:        1,
	})
}

// TestRedGreen_MissingCaseNamesWithAModifiedFileNeedAHuman covers no-case-names.
// The harness on base decides whether results carry names and the agent cannot
// reach it, so a retry would burn the budget on a task that was valid all along.
func TestRedGreen_MissingCaseNamesWithAModifiedFileNeedAHuman(t *testing.T) {
	t.Parallel()

	v := fixture{hooks: testkit.Hooks{Nameless: true}, head: honestFix}.judge(t)

	v.assertRefusal(t, domain.GateRedGreen, wantRefusal{
		message:     "cannot verify added cases: test results lack per-case names",
		remediation: domain.RemediationHumanRequired,
		exit:        5,
	})
}

// TestRedGreen_MissingCaseNamesWithNewFilesOnlyProbeByFile is the other row of
// the same table. A new file holds no case base already had, so the file set
// carries as much as the case names would, and the run costs nobody a refusal.
func TestRedGreen_MissingCaseNamesWithNewFilesOnlyProbeByFile(t *testing.T) {
	t.Parallel()

	v := fixture{
		hooks: testkit.Hooks{Nameless: true},
		head: func(r *testkit.Repo) {
			r.Write("src/total.js", totalFixed)
			r.Write("src/discount.test.js", honestCase)
		},
	}.judge(t)

	got := v.assertPass(t, domain.GateRedGreen)
	if v.exit != 0 {
		t.Errorf("exit code = %d, want 0", v.exit)
	}
	if len(got.Warnings) != 1 || got.Warnings[0].Kind != domain.WarnConfig {
		t.Errorf("warnings = %v, want the report to say the check ran by file", got.Warnings)
	}
}

// TestRedGreen_MixedProbeIsAFlakeAndNotAPass is the flaky-new-test fixture, and
// the one place in the whole system where randomness could produce an
// acquittal. A fake test stamped as a real
// one on a single accidental red reaches a human with a green verdict, so the
// gate demands the same answer from every run.
func TestRedGreen_MixedProbeIsAFlakeAndNotAPass(t *testing.T) {
	t.Parallel()

	// Asked on its own: the suite on head meets the same flake first, and its
	// refusal would short-circuit the gate under test.
	v := fixture{
		only: []domain.GateID{domain.GateRedGreen},
		head: func(r *testkit.Repo) {
			r.Write("src/total.js", totalFixed)
			r.Write("src/total.test.js", totalTest+flakyCase)
		},
	}.judge(t)

	got := v.assertRefusal(t, domain.GateRedGreen, wantRefusal{
		message:     "flaky test, base probe is not deterministic",
		remediation: domain.RemediationFixTest,
		exit:        1,
	})
	if len(got.Evidence) != 1 {
		t.Fatalf("evidence = %v, want the case that could not make up its mind", got.Evidence)
	}
	// The fixture flake follows $REDFIRST_PROBE_INDEX, and the inventory is run
	// one of the base phase, so the three probes are runs two to four.
	if want := []string{"pass", "fail", "pass"}; !slices.Equal(got.Evidence[0].BaseRuns, want) {
		t.Errorf("base runs = %v, want %v", got.Evidence[0].BaseRuns, want)
	}
}

// TestRedGreen_TestThatNeedsANewAPIPassesWithANoteOnHowItWasJudged covers
// test-needs-new-api. The run dies before the assertions on base, so no case
// name appears and the check falls back to the file set. That claims less than
// "the test catches the bug", and the report has to say so.
func TestRedGreen_TestThatNeedsANewAPIPassesWithANoteOnHowItWasJudged(t *testing.T) {
	t.Parallel()

	v := fixture{head: func(r *testkit.Repo) {
		r.Write("src/discount.js", discountSource)
		r.Write("src/discount.test.js", newAPITest)
	}}.judge(t)

	got := v.assertPass(t, domain.GateRedGreen)
	if v.exit != 0 {
		t.Errorf("exit code = %d, want 0", v.exit)
	}
	if len(got.Warnings) != 1 {
		t.Fatalf("warnings = %v, want one line about the red not coming from an assertion", got.Warnings)
	}
	if got.Warnings[0].Kind != domain.WarnConfig {
		t.Errorf("warning kind = %q, want %q", got.Warnings[0].Kind, domain.WarnConfig)
	}
}

// TestRedGreen_CompileFailureOnBaseCanBeConfiguredIntoARefusal is the third row
// of red_green.compile_failure_on_base, and the one that blocks.
func TestRedGreen_CompileFailureOnBaseCanBeConfiguredIntoARefusal(t *testing.T) {
	t.Parallel()

	v := fixture{
		cfg: func(c *domain.Config) { c.RedGreen.CompileFailureOnBase = domain.CompileFailureFail },
		head: func(r *testkit.Repo) {
			r.Write("src/discount.js", discountSource)
			r.Write("src/discount.test.js", newAPITest)
		},
	}.judge(t)

	v.assertRefusal(t, domain.GateRedGreen, wantRefusal{
		message:     "the added tests do not build on base, and the config counts that as a violation",
		remediation: domain.RemediationFixTest,
		exit:        1,
	})
}

// TestRedGreen_DeletedTestFileLeavesNoCaseToProve is a boundary the gate has to
// answer rather than crash on: the diff removed a test file and added none, so
// there is nothing on head to run.
func TestRedGreen_DeletedTestFileLeavesNoCaseToProve(t *testing.T) {
	t.Parallel()

	// test-immutability refuses the deletion first and would cut the run off,
	// so the gate under test is asked on its own.
	v := fixture{
		only: []domain.GateID{domain.GateRedGreen},
		head: func(r *testkit.Repo) {
			r.Write("src/total.js", totalFixed)
			r.Remove("src/total.test.js")
		},
	}.judge(t)

	v.assertRefusal(t, domain.GateRedGreen, wantRefusal{
		message:     "test files changed but no new test case appeared",
		remediation: domain.RemediationAddTest,
		exit:        1,
	})
}

// TestRedGreen_UnavailableWithoutTestFilesInTheDiff is the capability boundary:
// a diff that touches no test file leaves the gate nothing to probe, and the
// runner says so instead of the gate.
func TestRedGreen_UnavailableWithoutTestFilesInTheDiff(t *testing.T) {
	t.Parallel()

	v := fixture{head: func(r *testkit.Repo) {
		r.Write("src/total.js", totalFixed)
	}}.judge(t)

	got := v.gate(t, domain.GateRedGreen)
	if got.Status != domain.StatusUnavailable || got.Reason != "no test files in diff" {
		t.Errorf("got %q / %q, want unavailable / no test files in diff", got.Status, got.Reason)
	}
	if v.exit != 0 {
		t.Errorf("exit code = %d, want 0: a diff with no test file accuses nobody here", v.exit)
	}
}

// TestRedGreen_RedOnHeadIsTheFixNotTheTest is the mirror of a green base: the
// test does catch something, and the diff did not fix it. The remediation
// points at the sources rather than at the test, which is what separates this
// refusal from every other one the gate can reach.
func TestRedGreen_RedOnHeadIsTheFixNotTheTest(t *testing.T) {
	t.Parallel()

	v := fixture{
		only: []domain.GateID{domain.GateRedGreen},
		head: func(r *testkit.Repo) {
			r.Write("src/total.js", totalFixed)
			r.Write("src/total.test.js", totalTest+unfixableCase)
		},
	}.judge(t)

	got := v.assertRefusal(t, domain.GateRedGreen, wantRefusal{
		message:     "test is red on head, the fix does not make it pass",
		remediation: domain.RemediationFixCode,
		exit:        1,
	})
	if len(got.Evidence) != 1 {
		t.Fatalf("evidence = %v, want the case the fix left red", got.Evidence)
	}
	e := got.Evidence[0]
	if !slices.Equal(e.BaseRuns, []string{"fail", "fail", "fail"}) {
		t.Errorf("base runs = %v, want a failure in every one", e.BaseRuns)
	}
	if !slices.Equal(e.HeadRuns, []string{"fail", "fail", "fail"}) {
		t.Errorf("head runs = %v, want a failure in every one", e.HeadRuns)
	}
}

// TestRedGreen_MixedProbeOnHeadIsAFlakeToo holds K-of-K on the other side. A
// regression test that only sometimes passes with the fix in place has no value
// either, and the gate cuts it off rather than accept the run that suited it.
func TestRedGreen_MixedProbeOnHeadIsAFlakeToo(t *testing.T) {
	t.Parallel()

	v := fixture{
		only: []domain.GateID{domain.GateRedGreen},
		head: func(r *testkit.Repo) {
			r.Write("src/total.js", totalFixed)
			r.Write("src/total.test.js", totalTest+flakyOnHeadCase)
		},
	}.judge(t)

	got := v.assertRefusal(t, domain.GateRedGreen, wantRefusal{
		message:     "flaky test, head probe is not deterministic",
		remediation: domain.RemediationFixTest,
		exit:        1,
	})
	if len(got.Evidence) != 1 || got.Evidence[0].Case != "applies a discount" {
		t.Fatalf("evidence = %v, want the case that could not make up its mind", got.Evidence)
	}
	if want := []string{"fail", "pass", "fail"}; !slices.Equal(got.Evidence[0].HeadRuns, want) {
		t.Errorf("head runs = %v, want %v", got.Evidence[0].HeadRuns, want)
	}
}

// TestRedGreen_WithoutCaseNamesAFakeTestIsStillCaughtByFile covers the
// file-level fallback where it has to bite. The harness names nothing and the
// diff adds one new test file, so the probe is judged whole: green on base is a
// refusal there exactly as it is per case.
func TestRedGreen_WithoutCaseNamesAFakeTestIsStillCaughtByFile(t *testing.T) {
	t.Parallel()

	v := fixture{
		hooks: testkit.Hooks{Nameless: true},
		head: func(r *testkit.Repo) {
			r.Write("src/total.js", totalFixed)
			r.Write("src/discount.test.js", fakeCase)
		},
	}.judge(t)

	got := v.assertRefusal(t, domain.GateRedGreen, wantRefusal{
		message:     "test is green on base, it does not cover the fix",
		remediation: domain.RemediationFixTest,
		exit:        1,
	})
	if len(got.Evidence) != 1 {
		t.Fatalf("evidence = %v, want the one line a file-level judgement can show", got.Evidence)
	}
	if !slices.Equal(got.Evidence[0].BaseRuns, []string{"pass", "pass", "pass"}) {
		t.Errorf("base runs = %v, want the whole probe green in every run", got.Evidence[0].BaseRuns)
	}
}
