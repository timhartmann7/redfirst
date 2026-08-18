// Package tests holds the gates that judge what the diff did to the tests.
package tests

import (
	"context"
	"fmt"
	"strings"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// TestImmutability answers two questions: did the agent remove existing
// insurance, and did the agent bend the test surface toward its own result.
//
// The second question needs its own list. Snapshots, golden files, testdata,
// mocks and runner configs decide test outcomes without holding test cases, so
// editing one to match new output turns a suite green while fixing nothing: the
// file falls outside tests.patterns, sits nowhere in paths.protected and holds
// no .skip to find. tests.fixtures is that list, and it carries its own mode.
type TestImmutability struct {
	tests    domain.Immutability
	fixtures domain.Immutability
}

// NewTestImmutability builds the gate. Both modes are read here, at
// construction, because Requires() depends on them and has to stay fixed for a
// whole run.
func NewTestImmutability(cfg domain.Config) *TestImmutability {
	return &TestImmutability{
		tests:    cfg.Tests.Immutability,
		fixtures: cfg.Tests.FixturesImmutability,
	}
}

// ID names the gate in the config, the report and the JSON.
func (g *TestImmutability) ID() domain.GateID { return domain.GateTestImmutability }

// Requires declares what the gate needs before the runner lets it speak. Mode
// cases judges outcomes rather than lines, so it needs the harness that
// produces them and the case names its first run returned; the line-based modes
// read the diff alone.
func (g *TestImmutability) Requires() []domain.Capability {
	if g.tests == domain.ImmutabilityCases || g.fixtures == domain.ImmutabilityCases {
		return []domain.Capability{domain.CapDiff, domain.CapHooks, domain.CapCaseNames}
	}
	return []domain.Capability{domain.CapDiff}
}

// Run judges every file that existed at the merge base and belongs to one of
// the two lists. Files the diff added are nobody's business here: writing a new
// test is legal under every mode.
func (g *TestImmutability) Run(ctx context.Context, in domain.Input) (domain.GateResult, error) {
	lists := surfaces(in.Config)

	var offences []offence
	byOutcome := false
	for _, f := range in.Diff.Files {
		existing, ok := existingPath(f)
		if !ok {
			continue
		}
		primary, ok := claim(lists, existing)
		if !ok {
			continue
		}
		if primary.mode == domain.ImmutabilityCases {
			// Mode cases judges the outcomes of one run rather than the lines
			// of one file, so the check below answers for all of them at once.
			byOutcome = true
			continue
		}
		if o, broke := judge(f, existing, primary, lists); broke {
			offences = append(offences, o)
		}
	}

	if byOutcome {
		outcomes, err := casesOffences(ctx, in)
		if err != nil {
			return domain.GateResult{}, err
		}
		offences = append(offences, outcomes...)
	}
	return verdict(in.Config.Tests.Immutability, offences), nil
}

// surface is one of the two lists, with the mode that governs it.
type surface struct {
	class string
	// list and modeKey are the config keys, so a report line names the setting
	// a reader has to change rather than the rule in the abstract.
	list    string
	modeKey string
	mode    domain.Immutability
	set     domain.PatternSet
}

// surfaces lists the two classes in the order that settles an edit, and
// tests.fixtures speaks first for a path both lists match.
//
// The two lists overlap by construction rather than by accident. A jest
// snapshot is named after the file it belongs to, so `total.test.js.snap` falls
// under the default pattern `**/*.test.*` as surely as the test itself does.
// Reading it as a test file would leave tests.fixtures_immutability governing
// nothing in the commonest layout in the ecosystem, and section 8 of the spec
// promises the opposite: a human who updated a snapshot gets a report line
// rather than a red cross. The narrower list wins because it is the narrower
// list: someone who enumerated a surface meant that surface.
//
// The order settles an edit and not a removal. Going the other way, a real test
// file under a fixtures path would leave the repository behind the softer of
// the two rules that covered it, and that acquittal is the one invariant 2 puts
// above every convenience. See abandoned.
func surfaces(cfg domain.Config) [2]surface {
	return [2]surface{
		{
			class: "fixture", list: "tests.fixtures", modeKey: "tests.fixtures_immutability",
			mode: cfg.Tests.FixturesImmutability, set: cfg.Tests.Fixtures,
		},
		{
			class: "test file", list: "tests.patterns", modeKey: "tests.immutability",
			mode: cfg.Tests.Immutability, set: cfg.Tests.Patterns,
		},
	}
}

func claim(lists [2]surface, path string) (surface, bool) {
	for _, s := range lists {
		if s.set.Match(path) {
			return s, true
		}
	}
	return surface{}, false
}

// existingPath is the path the file had at the merge base, and whether it had
// one at all. A status nobody expects in a two-tree diff counts as existing:
// every ambiguity resolves toward the check, never away from it.
func existingPath(f domain.FileChange) (string, bool) {
	switch f.Status {
	case domain.FileAdded, domain.FileCopied:
		return "", false
	case domain.FileRenamed:
		return f.OldPath, true
	}
	return f.Path, true
}

// judge applies the rules of every list that claimed the file at the merge
// base. primary is the one that governs an edit; a removal answers to all of
// them.
func judge(f domain.FileChange, existing string, primary surface, lists [2]surface) (offence, bool) {
	if s, gone := abandoned(f, existing, lists); gone {
		return offence{file: existing, mode: s.mode, kind: kindRemoved, detail: removalDetail(f, s)}, true
	}

	o := offence{file: existing, mode: primary.mode}
	switch primary.mode {
	case domain.ImmutabilityStrict, domain.ImmutabilityWarn:
		o.kind = kindEdited
		o.detail = fmt.Sprintf("%s %s, %s is %s", primary.class, verb(f), primary.modeKey, primary.mode)
		return o, true
	case domain.ImmutabilityAppendOnly:
		if detail, broke := shortened(f, primary.class); broke {
			o.kind, o.detail = kindShortened, detail
			return o, true
		}
	}
	return offence{}, false
}

// abandoned names the list a removal answers to: one that claimed the file at
// the merge base and stopped claiming it on head.
//
// Every list that claimed the file gets a say, not the first one alone. The two
// overlap by construction, so a test file that also sits under tests.fixtures
// would otherwise leave the repository behind the softer of the two rules that
// covered it, and deleting a test is the violation the whole tool exists for.
func abandoned(f domain.FileChange, existing string, lists [2]surface) (surface, bool) {
	var softest surface
	found := false
	for _, s := range lists {
		if !s.set.Match(existing) || !left(f, s.set) {
			continue
		}
		if s.mode != domain.ImmutabilityWarn {
			return s, true
		}
		if !found {
			softest, found = s, true
		}
	}
	return softest, found
}

func removalDetail(f domain.FileChange, s surface) string {
	detail := s.class + " " + verb(f)
	if f.Status == domain.FileRenamed {
		detail += ", outside " + s.list
	}
	return detail
}

// left reports whether an existing file stopped belonging to a list. Deletion is
// the plain case; a rename out of the list removes the same insurance while
// leaving every line in the repository, and a type change turns the file into
// something the runner will not collect.
func left(f domain.FileChange, set domain.PatternSet) bool {
	switch f.Status {
	case domain.FileDeleted, domain.FileTypeChange:
		return true
	case domain.FileRenamed:
		return !set.Match(f.Path)
	}
	return false
}

// shortened reports what append-only has to refuse. A binary file counts
// whatever its counters say: git reports no lines for one, so zero deleted
// lines there means nothing was measured rather than nothing was removed, and a
// snapshot image swapped wholesale would walk straight through.
func shortened(f domain.FileChange, class string) (string, bool) {
	switch {
	case f.Binary:
		return class + " replaced, append-only cannot read an addition out of a binary file", true
	case f.Deleted > 0:
		return fmt.Sprintf("%s lost %d %s, append-only allows none",
			class, f.Deleted, plural(f.Deleted, "line")), true
	}
	return "", false
}

// verb says what the diff did to the file. A rename names its destination:
// which end of the move a list caught is invisible from one path.
func verb(f domain.FileChange) string {
	switch f.Status {
	case domain.FileDeleted:
		return "deleted"
	case domain.FileRenamed:
		return "renamed to " + f.Path
	case domain.FileTypeChange:
		return "changed type"
	case domain.FileModified:
		return "modified"
	}
	return "carries status " + string(f.Status)
}

type offenceKind string

const (
	// kindRemoved is a file that left its list: deleted, renamed out, retyped.
	kindRemoved offenceKind = "removed"
	// kindEdited is any edit at all, which strict and warn both count.
	kindEdited offenceKind = "edited"
	// kindShortened is a file that lost lines under append-only.
	kindShortened offenceKind = "shortened"
	// kindRegressed is a case that survived onto head and stopped being green.
	kindRegressed offenceKind = "regressed"
)

// offence is one file, or one case, that broke the rule its mode sets.
type offence struct {
	file string
	// kase is the test case that broke the rule, under mode cases. The other
	// modes judge a file and leave it empty.
	kase   string
	kind   offenceKind
	mode   domain.Immutability
	detail string
}

// verdict turns the offences into the report line. The message opens with the
// mode that produced the strongest one, so a snapshot flagged under warn says
// warn rather than borrowing the mode of a list it never belonged to.
func verdict(tests domain.Immutability, offences []offence) domain.GateResult {
	res := domain.GateResult{Status: domain.StatusPass}
	for _, o := range offences {
		res.Evidence = append(res.Evidence, domain.Evidence{File: o.file, Case: o.kase, Detail: o.detail})
	}

	mode := tests
	if worst, found := strongest(offences); found {
		mode = worst.mode
		res.Status = domain.StatusWarn
		if worst.mode != domain.ImmutabilityWarn {
			res.Status = domain.StatusFail
			res.Remediation = remediationFor(worst.mode)
		}
	}
	res.Message = summarise(offences, mode)
	// A config nobody filled names no mode, and the separator would open the
	// line on its own.
	if mode != "" {
		res.Message = string(mode) + " · " + res.Message
	}
	return res
}

// strongest is the first blocking offence, or the first of any kind when none
// of them blocks.
func strongest(offences []offence) (offence, bool) {
	for _, o := range offences {
		if o.mode != domain.ImmutabilityWarn {
			return o, true
		}
	}
	if len(offences) > 0 {
		return offences[0], true
	}
	return offence{}, false
}

// remediationFor names the move the mode leaves open. Under cases a refusal
// means a case the diff was supposed to keep is gone or red, and the sources
// are what answers for that; the line-based modes ask for the file back.
func remediationFor(mode domain.Immutability) domain.Remediation {
	if mode == domain.ImmutabilityCases {
		return domain.RemediationFixCode
	}
	return domain.RemediationRevertPaths
}

// summarise counts the offences by kind. A clean run says "0 removed": the line
// prints whatever the verdict, and the count is the thing a reviewer wants to
// see at zero. Mode cases counts the second thing it looks for as well.
func summarise(offences []offence, mode domain.Immutability) string {
	counts := make(map[offenceKind]int, 4)
	for _, o := range offences {
		counts[o.kind]++
	}

	parts := make([]string, 0, 4)
	for _, k := range []offenceKind{kindRemoved, kindEdited, kindShortened, kindRegressed} {
		if n := counts[k]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, k))
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, ", ")
	}
	if mode == domain.ImmutabilityCases {
		return "0 removed, 0 regressed"
	}
	return "0 removed"
}
