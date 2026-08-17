package paths_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/gates/paths"
)

// defaultLists are the two lists the built-in defaults ship with, cut down to
// what these tests need.
var defaultLists = lists{
	protected: []string{".github/workflows/redfirst.yml", "CLAUDE.md", ".claude/**"},
	guarded:   []string{".github/**", "**/*.lock", "pnpm-lock.yaml"},
}

func TestProtectedPaths_DeclaresItsIdentityAndRequirements(t *testing.T) {
	t.Parallel()

	g := paths.NewProtectedPaths(domain.Config{})

	if got := g.ID(); got != domain.GateProtectedPaths {
		t.Errorf("ID = %q, want %q", got, domain.GateProtectedPaths)
	}
	if got := g.Requires(); !slices.Equal(got, []domain.Capability{domain.CapDiff}) {
		t.Errorf("Requires = %v, want the diff alone", got)
	}
}

// TestProtectedPaths_RejectsRename holds the S1 acceptance criterion: a rename
// gets caught on both of its paths. Judging the destination alone would let an
// agent move CLAUDE.md aside, and judging the source alone would let it write
// a new one.
func TestProtectedPaths_RejectsRename(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		lists      lists
		file       domain.FileChange
		wantFail   bool
		wantFile   string
		wantDetail string
		wantMsg    string
	}{
		{
			name:       "TheSourcePathOfARenameIsProtected",
			lists:      defaultLists,
			file:       renamed(".github/workflows/redfirst.yml", ".github/workflows/ci.yml"),
			wantFail:   true,
			wantFile:   ".github/workflows/redfirst.yml",
			wantDetail: "renamed to .github/workflows/ci.yml, under paths.protected",
			wantMsg:    "1 file under paths.protected",
		},
		{
			name:       "TheDestinationPathOfARenameIsProtected",
			lists:      defaultLists,
			file:       renamed("docs/notes.md", "CLAUDE.md"),
			wantFail:   true,
			wantFile:   "CLAUDE.md",
			wantDetail: "renamed from docs/notes.md, under paths.protected",
			wantMsg:    "1 file under paths.protected",
		},
		{
			name:  "ARenameThatTouchesNeitherListPasses",
			lists: defaultLists,
			file:  renamed("src/total.js", "src/order/total.js"),
		},
		{
			name:       "ACopyIsCaughtOnItsSourceToo",
			lists:      defaultLists,
			file:       copied(".claude/rules.md", "docs/rules.md"),
			wantFail:   true,
			wantFile:   ".claude/rules.md",
			wantDetail: "copied to docs/rules.md, under paths.protected",
			wantMsg:    "1 file under paths.protected",
		},
		{
			name:       "ARenameOutOfTheAllowedAreaIsRefused",
			lists:      lists{allowed: []string{"src/**"}},
			file:       renamed("src/total.js", "docs/total.js"),
			wantFail:   true,
			wantFile:   "docs/total.js",
			wantDetail: "renamed from src/total.js, outside paths.allowed",
			wantMsg:    "1 file outside paths.allowed",
		},
		{
			name:       "ARenameIntoTheAllowedAreaIsRefusedOnItsSource",
			lists:      lists{allowed: []string{"src/**"}},
			file:       renamed("docs/total.js", "src/total.js"),
			wantFail:   true,
			wantFile:   "docs/total.js",
			wantDetail: "renamed to src/total.js, outside paths.allowed",
			wantMsg:    "1 file outside paths.allowed",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			res := runGate(t, paths.NewProtectedPaths(domain.Config{}), pathsConfig(t, tc.lists), tc.file)

			if !tc.wantFail {
				assertPass(t, res)
				return
			}
			assertRefusal(t, res)
			assertEvidence(t, res, tc.wantFile, tc.wantDetail)
			// One rename is one offence: counting it on both of its paths
			// would double every refusal a refactor produces.
			if res.Message != tc.wantMsg {
				t.Errorf("message = %q, want %q", res.Message, tc.wantMsg)
			}
			if len(res.Evidence) != 1 {
				t.Errorf("evidence = %v, want one entry for one file", res.Evidence)
			}
		})
	}
}

// TestProtectedPaths_RejectsEveryStatusOnAProtectedPath covers the statuses the
// spec names: addition, modification, deletion and rename, plus the binary
// file that carries no line counts to judge.
func TestProtectedPaths_RejectsEveryStatusOnAProtectedPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		file       domain.FileChange
		wantDetail string
	}{
		{"AddedFile", added("CLAUDE.md"), "added, under paths.protected"},
		{"ModifiedFile", modified("CLAUDE.md"), "modified, under paths.protected"},
		{"DeletedFile", deleted("CLAUDE.md"), "deleted, under paths.protected"},
		{"BinaryFile", binaryFile(".claude/logo.png"), "modified, under paths.protected"},
		{
			"TypeChangedFile",
			domain.FileChange{Path: "CLAUDE.md", Status: domain.FileTypeChange},
			"status T, under paths.protected",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			res := runGate(t, paths.NewProtectedPaths(domain.Config{}), pathsConfig(t, defaultLists), tc.file)

			assertRefusal(t, res)
			assertEvidence(t, res, tc.file.Path, tc.wantDetail)
			if res.Message != "1 file under paths.protected" {
				t.Errorf("message = %q, want it to count the one file", res.Message)
			}
		})
	}
}

func TestProtectedPaths_PassesWhenTheDiffStaysOutOfTheLists(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		files []domain.FileChange
	}{
		{"EmptyDiff", nil},
		{"OrdinaryDiffWithoutTestFiles", []domain.FileChange{modified("src/total.js"), added("src/order.js")}},
		{"BinaryFileOutsideEveryList", []domain.FileChange{binaryFile("assets/logo.png")}},
		{"DeletionOutsideEveryList", []domain.FileChange{deleted("src/legacy.js")}},
		{"APathThatOnlyLooksLikeAProtectedOne", []domain.FileChange{modified("docs/CLAUDE.md.tmpl")}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			res := runGate(t, paths.NewProtectedPaths(domain.Config{}), pathsConfig(t, defaultLists), tc.files...)

			assertPass(t, res)
			if len(res.Warnings) != 0 {
				t.Errorf("warnings = %v, want none", res.Warnings)
			}
		})
	}
}

// TestProtectedPaths_AllowedListRestrictsTheWholeDiff covers the second half of
// the gate: an empty list is off, a filled one refuses everything outside it.
func TestProtectedPaths_AllowedListRestrictsTheWholeDiff(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		lists    lists
		files    []domain.FileChange
		wantFail bool
		wantMsg  string
	}{
		{
			name:  "AnEmptyAllowedListLeavesEveryPathAlone",
			lists: lists{allowed: nil},
			files: []domain.FileChange{modified("docs/notes.md"), added("scripts/deploy.sh")},
		},
		{
			name:  "EveryFileInsideTheAllowedAreaPasses",
			lists: lists{allowed: []string{"src/**", "docs/**"}},
			files: []domain.FileChange{modified("src/total.js"), added("docs/notes.md")},
		},
		{
			name:     "AFileOutsideTheAllowedAreaIsRefused",
			lists:    lists{allowed: []string{"src/**"}},
			files:    []domain.FileChange{modified("src/total.js"), added("scripts/deploy.sh")},
			wantFail: true,
			wantMsg:  "1 file outside paths.allowed",
		},
		{
			name: "ProtectedOutranksAllowedSoTheReportNamesTheStrongerRule",
			lists: lists{
				protected: []string{"CLAUDE.md"},
				allowed:   []string{"CLAUDE.md", "src/**"},
			},
			files:    []domain.FileChange{modified("CLAUDE.md")},
			wantFail: true,
			wantMsg:  "1 file under paths.protected",
		},
		{
			name: "AMixedRefusalCountsBothRulesInOneMessage",
			lists: lists{
				protected: []string{"CLAUDE.md"},
				allowed:   []string{"src/**"},
			},
			files: []domain.FileChange{
				modified("CLAUDE.md"),
				added(".mcp.json"),
				modified("src/total.js"),
			},
			wantFail: true,
			wantMsg:  "1 file under paths.protected, 1 file outside paths.allowed",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			res := runGate(t, paths.NewProtectedPaths(domain.Config{}), pathsConfig(t, tc.lists), tc.files...)

			if !tc.wantFail {
				assertPass(t, res)
				return
			}
			assertRefusal(t, res)
			if res.Message != tc.wantMsg {
				t.Errorf("message = %q, want %q", res.Message, tc.wantMsg)
			}
		})
	}
}

// TestProtectedPaths_GuardedPathsWarnWithoutChangingTheVerdict covers the
// guarded-only fixture: a human who bumped a lockfile gets a line, not a red
// cross.
func TestProtectedPaths_GuardedPathsWarnWithoutChangingTheVerdict(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		files    []domain.FileChange
		wantFail bool
		want     []domain.Warning
	}{
		{
			name:  "AGuardedFileEarnsALineAndTheVerdictStays",
			files: []domain.FileChange{modified("pnpm-lock.yaml"), modified("src/total.js")},
			want:  []domain.Warning{{Kind: domain.WarnGuardedPaths, File: "pnpm-lock.yaml"}},
		},
		{
			name:  "ARenamedGuardedFileNamesWhereItLivesNow",
			files: []domain.FileChange{renamed(".github/dependabot.yml", ".github/renovate.json")},
			want: []domain.Warning{{
				Kind:   domain.WarnGuardedPaths,
				File:   ".github/renovate.json",
				Detail: "renamed from .github/dependabot.yml",
			}},
		},
		{
			name:     "ARefusedFileAsksForNoSecondLook",
			files:    []domain.FileChange{modified(".github/workflows/redfirst.yml")},
			wantFail: true,
		},
		{
			name:  "ADeletedGuardedFileEarnsTheSameLine",
			files: []domain.FileChange{deleted("pnpm-lock.yaml")},
			want:  []domain.Warning{{Kind: domain.WarnGuardedPaths, File: "pnpm-lock.yaml"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			res := runGate(t, paths.NewProtectedPaths(domain.Config{}), pathsConfig(t, defaultLists), tc.files...)

			if tc.wantFail {
				assertRefusal(t, res)
			} else {
				assertPass(t, res)
			}
			if !slices.Equal(res.Warnings, tc.want) {
				t.Errorf("warnings = %v, want %v", res.Warnings, tc.want)
			}
		})
	}
}

func assertRefusal(t *testing.T, res domain.GateResult) {
	t.Helper()

	if res.Status != domain.StatusFail {
		t.Fatalf("status = %q, want %q", res.Status, domain.StatusFail)
	}
	if res.Remediation != domain.RemediationRevertPaths {
		t.Errorf("remediation = %q, want %q", res.Remediation, domain.RemediationRevertPaths)
	}
	if res.Message == "" {
		t.Error("a refusal without a message reads as \"check failed\"")
	}
}

// assertEvidence holds invariant 9: the report says what exactly was violated
// and where, so the evidence names the path that broke the rule.
func assertEvidence(t *testing.T, res domain.GateResult, file, detail string) {
	t.Helper()

	found := false
	for _, e := range res.Evidence {
		found = found || (e.File == file && e.Detail == detail)
		if strings.TrimSpace(e.Detail) == "" {
			t.Errorf("evidence for %s carries no detail", e.File)
		}
	}
	if !found {
		t.Errorf("evidence = %v, want it to hold %s / %s", res.Evidence, file, detail)
	}
}
