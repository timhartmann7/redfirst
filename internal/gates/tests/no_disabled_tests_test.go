package tests_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/gates/tests"
)

// disabledRules carries the built-in forbid list.
func disabledRules(t *testing.T) domain.Config {
	t.Helper()

	return testsConfig(t, rules{
		patterns:       []string{"**/*.test.js", "**/*_test.go", "tests/**"},
		fixtures:       []string{"**/*.snap"},
		forbidDisabled: []string{`\.skip\(`, `\.only\(`, `\bxit\(`, `\bxdescribe\(`},
	})
}

// TestNoDisabledTests_RefusesASkipAddedToAnyTestFile covers the trick the gate
// exists for: writing a test and disabling it in the same breath. The file may
// be a new one or one that has been in the repository for a year, because
// append-only and cases both permit appending to an existing test file.
func TestNoDisabledTests_RefusesASkipAddedToAnyTestFile(t *testing.T) {
	t.Parallel()

	cfg := disabledRules(t)
	cases := []struct {
		name string
		file domain.FileChange
		want string
	}{
		{
			name: "AppendedToAnExistingFile",
			file: modified("src/total.test.js", 0, "test.skip('applies a discount', () => {})"),
			want: "test.skip('applies a discount', () => {})",
		},
		{
			name: "WrittenIntoANewFile",
			file: added("src/discount.test.js", "describe.only('discount', () => {})"),
			want: "describe.only('discount', () => {})",
		},
		{
			name: "CarriedInByARename",
			file: renamed("src/a.test.js", "src/b.test.js", 0, "  xit('is pending', () => {})"),
			want: "xit('is pending', () => {})",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			res := runGate(t, tests.NewNoDisabledTests(cfg), cfg, tc.file)

			assertRefusal(t, res, domain.RemediationRevertPaths)
			if want := "1 added line disabling a test"; res.Message != want {
				t.Errorf("message = %q, want %q", res.Message, want)
			}
			if len(res.Evidence) != 1 {
				t.Fatalf("evidence = %+v, want one entry", res.Evidence)
			}
			// The report needs the line, not only the file: a suite file holds
			// twenty cases and the reviewer has to find the one that was
			// switched off.
			got := res.Evidence[0]
			if got.File != tc.file.Path || got.Line != 1 || got.Detail != tc.want {
				t.Errorf("evidence = %+v, want %s:1 %q", got, tc.file.Path, tc.want)
			}
		})
	}
}

// TestNoDisabledTests_ReadsAddedLinesInTestFilesOnly draws the two boundaries
// of the gate: the line has to be one the diff wrote, and the file has to be
// one tests.patterns claims.
func TestNoDisabledTests_ReadsAddedLinesInTestFilesOnly(t *testing.T) {
	t.Parallel()

	cfg := disabledRules(t)
	cases := []struct {
		name  string
		files []domain.FileChange
	}{
		{"EmptyDiff", nil},
		{
			// The pattern is a rule about tests. A source file naming a method
			// .skip( is a source file.
			name:  "ASkipOutsideTheTestPatterns",
			files: []domain.FileChange{modified("src/queue.js", 0, "return this.skip(count)")},
		},
		{
			// The diff removed the line rather than writing it, which is the
			// direction the gate is happy about.
			name:  "ASkipTheDiffDeleted",
			files: []domain.FileChange{modified("src/total.test.js", 3, "test('applies a discount', () => {})")},
		},
		{
			name:  "ATestFileDeletedWhole",
			files: []domain.FileChange{removed("src/total.test.js", 40)},
		},
		{
			name:  "ABinaryFileUnderTheTestPatterns",
			files: []domain.FileChange{binary("tests/report.test.js", domain.FileModified)},
		},
		{
			name:  "ATestFileRenamedAndNothingWritten",
			files: []domain.FileChange{renamed("src/a.test.js", "src/b.test.js", 0)},
		},
		{
			// A file leaving the test tree stops being judged here, and
			// test-immutability is the gate that has something to say about it.
			name:  "ASkipInAFileRenamedOutOfTheTestTree",
			files: []domain.FileChange{renamed("src/a.test.js", "src/a.js", 0, "it.skip('waits', () => {})")},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertPass(t, runGate(t, tests.NewNoDisabledTests(cfg), cfg, tc.files...))
		})
	}
}

func TestNoDisabledTests_ReportsEveryOffendingLine(t *testing.T) {
	t.Parallel()

	cfg := disabledRules(t)

	res := runGate(t, tests.NewNoDisabledTests(cfg), cfg,
		modified("src/total.test.js", 0,
			"test('applies a discount', () => {})",
			"test.skip('handles a promo', () => {})"),
		added("tests/order.test.js", "xdescribe('orders', () => {})"),
	)

	assertRefusal(t, res, domain.RemediationRevertPaths)
	if want := "2 added lines disabling a test"; res.Message != want {
		t.Errorf("message = %q, want %q", res.Message, want)
	}
	want := []string{"src/total.test.js", "tests/order.test.js"}
	if got := evidenceFiles(res); !slices.Equal(got, want) {
		t.Errorf("evidence = %v, want %v in diff order", got, want)
	}
	if got := res.Evidence[0].Line; got != 2 {
		t.Errorf("line = %d, want 2: the number points at the offending line, not at the file", got)
	}
}

// TestNoDisabledTests_ClipsALineTooLongToRead keeps one minified line from
// pushing every other finding off the screen.
func TestNoDisabledTests_ClipsALineTooLongToRead(t *testing.T) {
	t.Parallel()

	cfg := disabledRules(t)
	long := "test.skip('" + strings.Repeat("very long name ", 40) + "', () => {})"

	res := runGate(t, tests.NewNoDisabledTests(cfg), cfg, added("src/total.test.js", long))

	assertRefusal(t, res, domain.RemediationRevertPaths)
	detail := res.Evidence[0].Detail
	if len([]rune(detail)) > 81 {
		t.Errorf("detail runs to %d runes: %q", len([]rune(detail)), detail)
	}
	if !strings.HasPrefix(detail, "test.skip('very long name") {
		t.Errorf("detail = %q, want the opening of the line", detail)
	}
}

func TestNoDisabledTests_ReadsTheDiffAlone(t *testing.T) {
	t.Parallel()

	cfg := disabledRules(t)

	got := tests.NewNoDisabledTests(cfg).Requires()
	if !slices.Equal(got, []domain.Capability{domain.CapDiff}) {
		t.Errorf("Requires = %v, want the diff alone: the gate is static", got)
	}
}
