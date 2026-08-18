package audit_test

import (
	"errors"
	"testing"

	"github.com/timhartmann7/redfirst/internal/audit"
	"github.com/timhartmann7/redfirst/internal/config"
	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/gitx"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

// options is the set the CLI passes on a bare `redfirst audit`, which every
// test here starts from.
func options() audit.Options {
	return audit.Options{
		Base:   testkit.FixtureBase,
		Unit:   audit.ModeAuto,
		Last:   20,
		Config: config.Defaults(),
	}
}

func run(t *testing.T, r *testkit.Repo, opts audit.Options) audit.Summary {
	t.Helper()

	repo, err := gitx.Open(t.Context(), r.Dir)
	if err != nil {
		t.Fatalf("open %s: %v", r.Dir, err)
	}
	s, err := audit.Run(t.Context(), repo, opts)
	if err != nil {
		t.Fatalf("audit %s: %v", opts.Base, err)
	}
	return s
}

func subjects(s audit.Summary) []string {
	out := make([]string, 0, len(s.PerUnit))
	for _, u := range s.PerUnit {
		out = append(out, u.Subject)
	}
	return out
}

// TestAudit_DetectsMergeHistoryFromTheShapeOfBase covers the one mode with
// exact pull request boundaries: nobody chooses it, someone else's repository
// settings decide it.
func TestAudit_DetectsMergeHistoryFromTheShapeOfBase(t *testing.T) {
	t.Parallel()

	got := run(t, testkit.AuditHistory(t), options())

	if got.Unit != audit.ModeMerge {
		t.Errorf("unit = %q, want %q on a repository that merges", got.Unit, audit.ModeMerge)
	}
	if got.Units != 8 {
		t.Errorf("audited %d units, want the 8 merges: %v", got.Units, subjects(got))
	}
	if got.Caveat == "" {
		t.Error("the header carries no accuracy caveat")
	}
	if got.Days != 14 {
		t.Errorf("days = %d, want the 14 the fixture spans", got.Days)
	}
}

// TestAudit_DetectsSquashHistoryAndWarnsThatOnePRCountsSeveralTimes holds the
// row of the spec table that costs accuracy: a rebase-merged pull request
// arrives as several commits and the header has to say so.
func TestAudit_DetectsSquashHistoryAndWarnsThatOnePRCountsSeveralTimes(t *testing.T) {
	t.Parallel()

	got := run(t, testkit.AuditSquashHistory(t), options())

	if got.Unit != audit.ModeSquash {
		t.Fatalf("unit = %q, want %q on numbered subjects", got.Unit, audit.ModeSquash)
	}
	if got.Caveat != "a PR merged as several commits counts several times" {
		t.Errorf("caveat = %q", got.Caveat)
	}
	if got.PerUnit[0].PR != "#14" {
		t.Errorf("the newest unit carries pr %q, want #14 off its subject", got.PerUnit[0].PR)
	}
}

// TestAudit_DetectsPlainCommitHistoryWhenNothingElseFits covers the third row:
// no merges, no numbers, and no pull request to name.
func TestAudit_DetectsPlainCommitHistoryWhenNothingElseFits(t *testing.T) {
	t.Parallel()

	r := testkit.NewRepo(t)
	for _, subject := range []string{"seed the tree", "add the total", "fix the total"} {
		r.Write("src/total.js", "export const total = () => 0 // "+subject+"\n")
		r.Commit("chore: " + subject)
	}

	got := run(t, r, options())

	if got.Unit != audit.ModeCommit {
		t.Errorf("unit = %q, want %q", got.Unit, audit.ModeCommit)
	}
	for _, u := range got.PerUnit {
		if u.PR != "" {
			t.Errorf("unit %s carries pr %q, and this mode has none to read", u.Commit, u.PR)
		}
	}
}

// TestAudit_HalfTheHistoryIsNotAMajority pins the boundary of the detection
// rule: the spec asks for more than half of the sample, and a repository that
// merges half its work is not a repository that merges.
func TestAudit_HalfTheHistoryIsNotAMajority(t *testing.T) {
	t.Parallel()

	r := testkit.NewRepo(t)
	r.Write("README.md", "# shop\n")
	r.Commit("chore: seed the tree")

	r.Branch("feature/total")
	r.Write("src/total.js", "export const total = () => 0\n")
	r.Commit("feat: add total")
	r.Checkout(testkit.FixtureBase)
	r.Merge("feature/total", "Merge pull request #1 from shop/total")

	r.Write("README.md", "# shop\n\nA shop.\n")
	r.Commit("docs: describe the shop")

	r.Branch("feature/discount")
	r.Write("src/discount.js", "export const discount = () => 0\n")
	r.Commit("feat: add the discount")
	r.Checkout(testkit.FixtureBase)
	r.Merge("feature/discount", "Merge pull request #2 from shop/discount")

	// Two merges out of four first-parent commits, the root included.
	got := run(t, r, options())

	if got.Unit != audit.ModeCommit {
		t.Errorf("unit = %q on a history that is half merges, want %q", got.Unit, audit.ModeCommit)
	}
	if got.Units != 3 {
		t.Errorf("audited %d units, want the 3 that follow the root: %v", got.Units, subjects(got))
	}
}

// TestAudit_UnitFlagOverridesDetection proves the flag reaches the walk: a
// squashed history holds no merge commits, so asking for merges audits nothing.
func TestAudit_UnitFlagOverridesDetection(t *testing.T) {
	t.Parallel()

	opts := options()
	opts.Unit = audit.ModeMerge
	got := run(t, testkit.AuditSquashHistory(t), opts)

	if got.Unit != audit.ModeMerge {
		t.Errorf("unit = %q, want the mode that was asked for", got.Unit)
	}
	if got.Units != 0 {
		t.Errorf("audited %d units, want none: %v", got.Units, subjects(got))
	}
	if len(got.Findings) != 0 {
		t.Errorf("findings over an empty walk: %v", got.Findings)
	}
}

// TestAudit_RootCommitIsNotAUnit keeps the repository's first commit out of the
// numbers: it is the whole tree arriving at once, not a unit of merged history.
func TestAudit_RootCommitIsNotAUnit(t *testing.T) {
	t.Parallel()

	got := run(t, testkit.AuditSquashHistory(t), options())

	if got.Units != 4 {
		t.Fatalf("audited %d units, want the 4 that followed the root: %v", got.Units, subjects(got))
	}
	for _, u := range got.PerUnit {
		if u.Subject == "chore: seed the tree" {
			t.Errorf("the root commit was audited as a unit: %s", u.Commit)
		}
	}
}

// TestAudit_MergeUnitIsMeasuredFromTheForkPoint holds the rule diff-budget
// lives by: a two-dot diff would show whatever landed on base while the branch
// was open as a revert the branch wrote.
func TestAudit_MergeUnitIsMeasuredFromTheForkPoint(t *testing.T) {
	t.Parallel()

	r := testkit.NewRepo(t)
	r.Write("src/total.js", "export const total = () => 0\n")
	r.Commit("chore: seed the tree")

	r.Branch("feature/discount")
	r.Write("src/discount.js", "export const discount = () => 0\n")
	r.Commit("feat: add the discount")

	r.Checkout(testkit.FixtureBase)
	r.Write("src/order.js", "export const order = () => 0\n")
	r.Commit("feat: add the order, on base, while the branch was open")
	r.Merge("feature/discount", "Merge pull request #1 from shop/discount")

	opts := options()
	opts.Unit = audit.ModeMerge
	got := run(t, r, opts)

	if got.Units != 1 {
		t.Fatalf("audited %d units, want the one merge", got.Units)
	}
	unit := got.PerUnit[0]
	if unit.Diff.Files != 1 || unit.Diff.Deleted != 0 {
		t.Errorf("the merge counts %d files and %d deleted lines, want 1 and 0: the commit that "+
			"landed on base is not part of the pull request", unit.Diff.Files, unit.Diff.Deleted)
	}
}

func TestAudit_LastCapsTheUnits(t *testing.T) {
	t.Parallel()

	opts := options()
	opts.Last = 3
	got := run(t, testkit.AuditHistory(t), opts)

	if got.Units != 3 {
		t.Fatalf("audited %d units under --last 3: %v", got.Units, subjects(got))
	}
	if got.PerUnit[0].Subject != "Merge pull request #8 from shop/instructions" {
		t.Errorf("the walk opens at %q, want the newest unit", got.PerUnit[0].Subject)
	}
}

// TestAudit_SinceReplacesLast holds the flag table: the two are alternatives,
// so a cutoff date makes the count irrelevant rather than narrower.
func TestAudit_SinceReplacesLast(t *testing.T) {
	t.Parallel()

	opts := options()
	opts.Last = 1
	opts.Since = "2026-01-13T00:00:00+00:00"
	got := run(t, testkit.AuditHistory(t), opts)

	if got.Units != 3 {
		t.Errorf("audited %d units since the cutoff, want 3: %v", got.Units, subjects(got))
	}
}

func TestAudit_UnknownBaseIsHarnessFailureNotAgentFault(t *testing.T) {
	t.Parallel()

	r := testkit.AuditHistory(t)
	repo, err := gitx.Open(t.Context(), r.Dir)
	if err != nil {
		t.Fatalf("open %s: %v", r.Dir, err)
	}

	opts := options()
	opts.Base = "origin/nowhere"
	if _, err := audit.Run(t.Context(), repo, opts); !errors.Is(err, domain.ErrHarness) {
		t.Fatalf("audit of an unknown base: %v, want a harness error", err)
	}
}

func TestParseMode_RejectsAnUnknownUnit(t *testing.T) {
	t.Parallel()

	if _, err := audit.ParseMode("pull-request"); err == nil {
		t.Error("an unknown unit mode was accepted")
	}
	for _, want := range []audit.Mode{audit.ModeAuto, audit.ModeMerge, audit.ModeSquash, audit.ModeCommit} {
		got, err := audit.ParseMode(string(want))
		if err != nil || got != want {
			t.Errorf("ParseMode(%q) = %q, %v", want, got, err)
		}
	}
}
