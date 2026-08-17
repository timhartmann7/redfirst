package tests_test

import (
	"slices"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/gates/tests"
	"github.com/timhartmann7/redfirst/internal/runner"
)

// suppressionRules carries the built-in pattern list.
func suppressionRules(t *testing.T, off bool) domain.Config {
	t.Helper()

	return testsConfig(t, rules{
		patterns: []string{"**/*.test.ts"},
		suppression: []string{
			`catch\s*\(\s*\w*\s*\)\s*\{\s*\}`,
			`@ts-ignore`,
			`@ts-expect-error`,
			`eslint-disable`,
			`:\s*any\b`,
		},
		suppressionOff: off,
	})
}

// TestSuppressionScan_LeavesTheVerdictAlone is the fourth acceptance criterion
// of the slice. The gate asks a human to look; whether the silenced symptom
// matters is a judgement no deterministic check can make, so the exit code
// stays where the other gates left it.
func TestSuppressionScan_LeavesTheVerdictAlone(t *testing.T) {
	t.Parallel()

	cfg := suppressionRules(t, false)

	res := runGate(t, tests.NewSuppressionScan(cfg), cfg,
		modified("src/total.ts", 1, "  } catch (e) {}"),
		added("src/order.ts", "// @ts-ignore", "const total: any = compute()"),
	)

	assertStatus(t, res, domain.StatusWarn)
	if res.Remediation != "" {
		t.Errorf("remediation = %q, want none: the gate accuses the diff of nothing", res.Remediation)
	}
	if len(res.Evidence) != 0 {
		t.Errorf("evidence = %+v, want none: the findings belong in the reviewer block", res.Evidence)
	}
	// The verdict is a property of the run, not of one line in it.
	if err := runner.Outcome([]domain.GateResult{res}); err != nil {
		t.Fatalf("outcome = %v, want none", err)
	}
	if code := domain.ExitCode(runner.Outcome([]domain.GateResult{res})); code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestSuppressionScan_WarningCarriesFileLineAndPattern(t *testing.T) {
	t.Parallel()

	cfg := suppressionRules(t, false)

	res := runGate(t, tests.NewSuppressionScan(cfg), cfg,
		modified("src/total.ts", 0, "const rate = 1", "  } catch (e) {}"))

	if len(res.Warnings) != 1 {
		t.Fatalf("warnings = %+v, want one", res.Warnings)
	}
	want := domain.Warning{
		Kind:    domain.WarnSuppression,
		File:    "src/total.ts",
		Line:    2,
		Pattern: `catch\s*\(\s*\w*\s*\)\s*\{\s*\}`,
		Detail:  "} catch (e) {}",
	}
	if res.Warnings[0] != want {
		t.Errorf("warning = %+v, want %+v", res.Warnings[0], want)
	}
	if want := "1 added line to review"; res.Message != want {
		t.Errorf("message = %q, want %q", res.Message, want)
	}
}

// TestSuppressionScan_ReadsEveryFileInTheDiff separates this gate from
// no-disabled-tests: a symptom gets silenced wherever it was loud, and the
// suppression list is not a rule about tests.
func TestSuppressionScan_ReadsEveryFileInTheDiff(t *testing.T) {
	t.Parallel()

	cfg := suppressionRules(t, false)

	res := runGate(t, tests.NewSuppressionScan(cfg), cfg,
		added("src/order.test.ts", "const stub: any = {}"),
		modified("infra/deploy.sh", 0, "# eslint-disable"),
	)

	assertStatus(t, res, domain.StatusWarn)
	want := []string{"src/order.test.ts", "infra/deploy.sh"}
	got := make([]string, 0, len(res.Warnings))
	for _, w := range res.Warnings {
		got = append(got, w.File)
	}
	if !slices.Equal(got, want) {
		t.Errorf("warned about %v, want %v in diff order", got, want)
	}
}

func TestSuppressionScan_PassesOnADiffThatSilencesNothing(t *testing.T) {
	t.Parallel()

	cfg := suppressionRules(t, false)
	cases := []struct {
		name  string
		files []domain.FileChange
	}{
		{"EmptyDiff", nil},
		{"OrdinaryLines", []domain.FileChange{added("src/total.ts", "export const rate = 1")}},
		{
			// The pattern sat on a line the diff removed, which is the
			// direction that needs no reviewer.
			name:  "ASuppressionTheDiffDeleted",
			files: []domain.FileChange{modified("src/total.ts", 2, "export const rate = 1")},
		},
		{"ADeletedFile", []domain.FileChange{removed("src/legacy.ts", 80)}},
		{"ABinaryFile", []domain.FileChange{binary("assets/logo.png", domain.FileModified)}},
		{"APureRename", []domain.FileChange{renamed("src/a.ts", "src/b.ts", 0)}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertPass(t, runGate(t, tests.NewSuppressionScan(cfg), cfg, tc.files...))
		})
	}
}

func TestSuppressionScan_SkipsWhileSuppressionIsOff(t *testing.T) {
	t.Parallel()

	cfg := suppressionRules(t, true)

	res := runGate(t, tests.NewSuppressionScan(cfg), cfg, added("src/total.ts", "  } catch (e) {}"))

	assertStatus(t, res, domain.StatusSkip)
	if want := "suppression.enabled is false"; res.Reason != want {
		t.Errorf("reason = %q, want %q", res.Reason, want)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("warnings = %+v, want none from a gate that did not run", res.Warnings)
	}
}

func TestSuppressionScan_ReadsTheDiffAlone(t *testing.T) {
	t.Parallel()

	cfg := suppressionRules(t, false)

	got := tests.NewSuppressionScan(cfg).Requires()
	if !slices.Equal(got, []domain.Capability{domain.CapDiff}) {
		t.Errorf("Requires = %v, want the diff alone: the gate is static", got)
	}
}
