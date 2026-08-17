package runner_test

import (
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/runner"
)

// testSurfaceConfig marks Go test files and everything under testdata/ as the
// test surface, which is what tests.patterns and tests.fixtures do in a real
// config.
func testSurfaceConfig(t *testing.T) domain.Config {
	t.Helper()

	patterns, err := domain.NewPatternSet([]string{"**/*_test.go"})
	if err != nil {
		t.Fatalf("tests.patterns: %v", err)
	}
	fixtures, err := domain.NewPatternSet([]string{"testdata/**"})
	if err != nil {
		t.Fatalf("tests.fixtures: %v", err)
	}
	return domain.Config{Tests: domain.Tests{Patterns: patterns, Fixtures: fixtures}}
}

func TestCapSet_HasOnlyTheCapabilitiesGiven(t *testing.T) {
	t.Parallel()

	all := []domain.Capability{
		domain.CapDiff, domain.CapHooks, domain.CapTestFiles, domain.CapCaseNames, domain.CapStackTrace,
	}
	set := runner.NewCapSet(domain.CapHooks, domain.CapStackTrace)

	for _, c := range all {
		want := c == domain.CapHooks || c == domain.CapStackTrace
		if got := set.Has(c); got != want {
			t.Errorf("Has(%v) = %v, want %v", c, got, want)
		}
		if runner.NewCapSet().Has(c) {
			t.Errorf("the empty set claims %v", c)
		}
	}
}

func TestCapabilities_HooksNeedBaseHooksAndNoNoHooksFlag(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		env     runner.Env
		wantCap bool
	}{
		{"HooksInBaseAndFlagUnset", runner.Env{HooksPresent: true}, true},
		{"NoHooksFlagOverridesTheHooksInBase", runner.Env{HooksPresent: true, NoHooks: true}, false},
		{"NoHooksInBase", runner.Env{}, false},
		{"NoHooksInBaseAndFlagSet", runner.Env{NoHooks: true}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			caps := runner.Capabilities(tc.env, domain.Diff{}, runner.HarnessProbe{}, domain.Config{})

			if got := caps.Has(domain.CapHooks); got != tc.wantCap {
				t.Errorf("CapHooks = %v, want %v", got, tc.wantCap)
			}
			if !caps.Has(domain.CapDiff) {
				t.Error("CapDiff is missing; it is available in every run")
			}
		})
	}
}

func TestCapabilities_CaseNamesAndStackTraceComeFromTheirOwnSource(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		env       runner.Env
		probe     runner.HarnessProbe
		wantCases bool
		wantTrace bool
	}{
		{name: "NeitherSourceReported"},
		{
			name:      "InventoryRunReturnedCaseNames",
			probe:     runner.HarnessProbe{CaseNames: true},
			wantCases: true,
		},
		{
			name:      "CallerPassedAStackTrace",
			env:       runner.Env{StackTrace: true},
			wantTrace: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			caps := runner.Capabilities(tc.env, domain.Diff{}, tc.probe, domain.Config{})

			if got := caps.Has(domain.CapCaseNames); got != tc.wantCases {
				t.Errorf("CapCaseNames = %v, want %v", got, tc.wantCases)
			}
			if got := caps.Has(domain.CapStackTrace); got != tc.wantTrace {
				t.Errorf("CapStackTrace = %v, want %v", got, tc.wantTrace)
			}
		})
	}
}

func TestRunner_CapTestFilesDerivedFromDiff(t *testing.T) {
	t.Parallel()

	cfg := testSurfaceConfig(t)

	cases := []struct {
		name  string
		files []domain.FileChange
		want  bool
	}{
		{name: "EmptyDiffHasNoTestFiles"},
		{
			name:  "SourceFileAloneIsNotTestSurface",
			files: []domain.FileChange{{Path: "internal/order/total.go", Status: domain.FileModified}},
		},
		{
			name:  "AddedTestFileCounts",
			files: []domain.FileChange{{Path: "internal/order/total_test.go", Status: domain.FileAdded}},
			want:  true,
		},
		{
			name:  "DeletedTestFileCounts",
			files: []domain.FileChange{{Path: "internal/order/total_test.go", Status: domain.FileDeleted}},
			want:  true,
		},
		{
			name: "RenameIntoTheTestSurfaceCounts",
			files: []domain.FileChange{{
				Path: "internal/order/total_test.go", OldPath: "internal/order/helper.go", Status: domain.FileRenamed,
			}},
			want: true,
		},
		{
			name: "RenameOutOfTheTestSurfaceCountsThroughTheOldPath",
			files: []domain.FileChange{{
				Path: "internal/order/helper.go", OldPath: "internal/order/total_test.go", Status: domain.FileRenamed,
			}},
			want: true,
		},
		{
			name:  "BinaryFixtureCounts",
			files: []domain.FileChange{{Path: "testdata/golden.bin", Status: domain.FileModified, Binary: true}},
			want:  true,
		},
		{
			name:  "BinaryFileOutsideTheSurfaceDoesNot",
			files: []domain.FileChange{{Path: "assets/logo.png", Status: domain.FileAdded, Binary: true}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			diff := domain.Diff{Files: tc.files}
			caps := runner.Capabilities(runner.Env{}, diff, runner.HarnessProbe{}, cfg)

			if got := caps.Has(domain.CapTestFiles); got != tc.want {
				t.Errorf("CapTestFiles = %v, want %v", got, tc.want)
			}
		})
	}

	t.Run("AGateNeedingTestFilesFollowsTheDiff", func(t *testing.T) {
		t.Parallel()

		gate := &fakeGate{
			id:       domain.GateRedGreen,
			requires: []domain.Capability{domain.CapDiff, domain.CapTestFiles},
			result:   domain.GateResult{Status: domain.StatusPass},
		}
		diff := domain.Diff{Files: []domain.FileChange{
			{Path: "internal/order/total_test.go", Status: domain.FileAdded},
		}}
		caps := runner.Capabilities(runner.Env{}, diff, runner.HarnessProbe{}, cfg)

		got := run(t, []runner.Entry{entry(gate)}, runner.Options{Config: cfg, Caps: caps})[0]

		if got.Status != domain.StatusPass || gate.runs != 1 {
			t.Errorf("status %q after %d runs, want pass after 1", got.Status, gate.runs)
		}
	})
}
