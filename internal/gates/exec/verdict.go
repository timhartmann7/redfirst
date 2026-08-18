package exec

import (
	"fmt"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// The four refusals red-green can reach, spelled out where they are worded
// rather than at their call sites. A failure line says what is wrong, not what
// got checked: "test is green on base" helps, "red-green check failed" does not.
const (
	greenOnBase = "test is green on base, it does not cover the fix"
	flakyOnBase = "flaky test, base probe is not deterministic"
	redOnHead   = "test is red on head, the fix does not make it pass"
	flakyOnHead = "flaky test, head probe is not deterministic"
)

// judgeBase demands the one result that proves an added case catches a bug:
// red in every run, not red in one of them.
//
// This is the single place in the whole system where randomness could produce a
// pass. A fake test that sits green on base would be stamped as a real
// reproduction on one accidental red, and a human would receive it with a green
// verdict. Rerunning until it goes red would bend the result to fit, so we
// demand determinism instead.
func judgeBase(base domain.Runs, added []string) (domain.GateResult, bool) {
	var green, mixed []string
	for _, name := range added {
		outcomes := base.Outcomes(name)
		switch {
		case outcomes.All(domain.CaseFail):
		case !outcomes.Any(domain.CaseFail):
			green = append(green, name)
		default:
			mixed = append(mixed, name)
		}
	}

	if len(green) > 0 {
		return refuse(greenOnBase, domain.RemediationFixTest, baseEvidence(base, green)...), true
	}
	if len(mixed) > 0 {
		return refuse(flakyOnBase, domain.RemediationFixTest, baseEvidence(base, mixed)...), true
	}
	return domain.GateResult{}, false
}

// judgeHead demands the mirror of it: green in every run. A regression test
// that only sometimes passes with the fix in place has no value either, and
// K-of-K cuts it off here as well.
func judgeHead(base, head domain.Runs, added []string) domain.GateResult {
	var red, mixed []string
	for _, name := range added {
		outcomes := head.Outcomes(name)
		switch {
		case outcomes.All(domain.CasePass):
		case !outcomes.Any(domain.CasePass):
			red = append(red, name)
		default:
			mixed = append(mixed, name)
		}
	}

	if len(red) > 0 {
		return refuse(redOnHead, domain.RemediationFixCode, headEvidence(base, head, red)...)
	}
	if len(mixed) > 0 {
		return refuse(flakyOnHead, domain.RemediationFixTest, headEvidence(base, head, mixed)...)
	}
	n := len(added)
	return domain.GateResult{
		Status:  domain.StatusPass,
		Message: fmt.Sprintf("%d new %s red on base, green on head", n, plural(n, "case")),
	}
}

// judgeBaseByFile is judgeBase with the file set as the unit, for a base probe
// that named nothing to judge.
func judgeBaseByFile(base domain.Runs) (domain.GateResult, bool) {
	switch {
	case !base.AnyGreen():
		return domain.GateResult{}, false
	case base.AllGreen():
		return refuse(greenOnBase, domain.RemediationFixTest, fileEvidence(base, nil)...), true
	}
	return refuse(flakyOnBase, domain.RemediationFixTest, fileEvidence(base, nil)...), true
}

// judgeHeadByFile is judgeHead with the file set as the unit.
func judgeHeadByFile(head domain.Runs) domain.GateResult {
	switch {
	case head.AllGreen():
		return domain.GateResult{
			Status:  domain.StatusPass,
			Message: "the added test files are red on base and green on head",
		}
	case !head.AnyGreen():
		return refuse(redOnHead, domain.RemediationFixCode, fileEvidence(nil, head)...)
	}
	return refuse(flakyOnHead, domain.RemediationFixTest, fileEvidence(nil, head)...)
}

// noNewCase is the refusal for a diff that touched test files and left no case
// behind that could catch anything.
func noNewCase() domain.GateResult {
	return refuse("test files changed but no new test case appeared", domain.RemediationAddTest)
}

// namesMissing is the refusal no diff can answer.
//
// The harness on base decides whether results carry case names, and the agent
// cannot reach it. A plain violation would burn the retry budget and escalate a
// task that was valid from the start; a pass would reopen the very channel the
// gate moved to cases in order to close. So the report says a human is needed
// and the run exits 5.
func namesMissing() domain.GateResult {
	return refuse("cannot verify added cases: test results lack per-case names",
		domain.RemediationHumanRequired)
}

func refuse(message string, rem domain.Remediation, evidence ...domain.Evidence) domain.GateResult {
	return domain.GateResult{
		Status:      domain.StatusFail,
		Message:     message,
		Remediation: rem,
		Evidence:    evidence,
	}
}

// baseEvidence names the case rather than the file. In a file holding twenty
// cases, "the file went red" tells a reviewer nothing.
func baseEvidence(base domain.Runs, names []string) []domain.Evidence {
	out := make([]domain.Evidence, 0, len(names))
	for _, name := range names {
		out = append(out, domain.Evidence{
			File: base.File(name), Case: name, BaseRuns: base.Outcomes(name).Strings(),
		})
	}
	return out
}

func headEvidence(base, head domain.Runs, names []string) []domain.Evidence {
	out := make([]domain.Evidence, 0, len(names))
	for _, name := range names {
		out = append(out, domain.Evidence{
			File:     head.File(name),
			Case:     name,
			BaseRuns: base.Outcomes(name).Strings(),
			HeadRuns: head.Outcomes(name).Strings(),
		})
	}
	return out
}

// fileEvidence is what is left to show where no case was named: the run by run
// outcome of the filtered file set.
func fileEvidence(base, head domain.Runs) []domain.Evidence {
	e := domain.Evidence{Detail: "the base probe named no case, judged by file"}
	e.BaseRuns = greenness(base)
	e.HeadRuns = greenness(head)
	return []domain.Evidence{e}
}

func greenness(runs domain.Runs) []string {
	out := make([]string, 0, len(runs))
	for _, run := range runs {
		outcome := domain.CaseFail
		if run.Green {
			outcome = domain.CasePass
		}
		out = append(out, string(outcome))
	}
	return out
}

// missing is every name of got that want does not already hold, in the order
// the runs produced them.
func missing(got, have []string) []string {
	index := make(map[string]struct{}, len(have))
	for _, name := range have {
		index[name] = struct{}{}
	}
	var out []string
	for _, name := range got {
		if _, ok := index[name]; !ok {
			out = append(out, name)
		}
	}
	return out
}
