package exec

import (
	"context"
	"fmt"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// SuiteGreen runs the whole suite on head, once, and answers the only question
// a red suite raises: whose red is it.
//
// The agent answers for what it wrote and does not answer for a flake that sat
// in the repository before it arrived. Without that split the first foreign
// flake gets charged to the agent, the orchestrator spends its retries on it,
// and a human receives a problem that was never the diff's.
type SuiteGreen struct {
	retries int
	// knownFlaky comes from base, so the agent cannot extend it.
	knownFlaky []string
}

// NewSuiteGreen builds the gate. The config arrives at construction rather than
// at Run: Requires() has to stay fixed for a whole run.
func NewSuiteGreen(cfg domain.Config) *SuiteGreen {
	return &SuiteGreen{retries: cfg.Suite.RetryFailed, knownFlaky: cfg.Tests.KnownFlaky}
}

// ID names the gate in the config, the report and the JSON.
func (g *SuiteGreen) ID() domain.GateID { return domain.GateSuiteGreen }

// Requires declares what the gate needs before the runner lets it speak.
func (g *SuiteGreen) Requires() []domain.Capability {
	return []domain.Capability{domain.CapDiff, domain.CapHooks}
}

// Run executes the suite on head and classifies whatever it lost.
func (g *SuiteGreen) Run(ctx context.Context, in domain.Input) (domain.GateResult, error) {
	probe, err := probeOf(in)
	if err != nil {
		return domain.GateResult{}, err
	}
	suite, err := probe.Suite(ctx)
	if err != nil {
		return domain.GateResult{}, err
	}

	all := failures(suite)
	judged := g.judged(all)
	if len(judged) == 0 {
		return g.clean(suite, all)
	}
	retried, err := probe.Retry(ctx, files(judged))
	if err != nil {
		return domain.GateResult{}, err
	}
	return g.classify(ctx, probe, in.Diff, suite, judged, retried)
}

// clean is the verdict where nothing the gate judges failed.
//
// A red run whose every failure the config exempts is what known_flaky is for.
// A red run that named no failure at all is a different thing: nothing built,
// or the runner died before it could report, or the hook lost a step outside
// the cases. The head tree is what is broken there, the agent wrote it, and a
// gate that read the silence as a pass would acquit on an ambiguity.
func (g *SuiteGreen) clean(suite domain.Runs, all []domain.Case) (domain.GateResult, error) {
	if suite.AllGreen() || len(all) > 0 {
		return domain.GateResult{Status: domain.StatusPass, Message: passed(suite, 0)}, nil
	}
	return domain.GateResult{
		Status:      domain.StatusFail,
		Message:     "the suite is red on head and named no failing case",
		Remediation: domain.RemediationFixCode,
	}, nil
}

// classify sorts the failures by origin: the ones the diff wrote block, and the
// ones it did not go to the retry and, failing that, to the suite on base.
func (g *SuiteGreen) classify(
	ctx context.Context, probe domain.Probe, diff domain.Diff,
	suite domain.Runs, failed []domain.Case, retried domain.Runs,
) (domain.GateResult, error) {
	var blocking, inherited []domain.Case
	var warnings []domain.Warning

	for _, c := range failed {
		switch {
		case touched(diff, c.File):
			blocking = append(blocking, c)
		case greenOnRetry(retried, c):
			warnings = append(warnings, domain.Warning{
				Kind: domain.WarnFlaky, File: c.File,
				Detail: "passed on retry, not touched by diff",
			})
		default:
			inherited = append(inherited, c)
		}
	}

	// The suite on base costs a working copy of its own, so it runs on the one
	// path that needs it: nothing the diff wrote failed, and a failure it did
	// not write survived the retry. With a blocking failure in hand the run is
	// refused either way, and the answer would change nothing.
	askedBase := len(blocking) == 0 && len(inherited) > 0
	if askedBase {
		if err := g.blameTheRepository(ctx, probe, inherited); err != nil {
			return domain.GateResult{}, err
		}
	}
	if len(blocking)+len(inherited) == 0 {
		res := domain.GateResult{Status: domain.StatusPass, Message: passed(suite, len(warnings))}
		res.Warnings = warnings
		return res, nil
	}
	return suiteRefusal(suite, blocking, inherited, warnings, askedBase), nil
}

// blameTheRepository runs the suite on base and reports a harness failure where
// the same cases were already red there.
//
// That is the whole reason exit code 2 exists: an orchestrator that charged the
// agent for a repository broken before it arrived would burn its retry budget
// on somebody else's commit.
func (g *SuiteGreen) blameTheRepository(
	ctx context.Context, probe domain.Probe, inherited []domain.Case,
) error {
	base, err := probe.BaseSuite(ctx)
	if err != nil {
		return err
	}
	// A base suite that came back red without naming a case is broken outright,
	// and every failure the diff did not write belongs to it.
	broken := !base.AllGreen() && len(base.Names()) == 0
	for _, c := range inherited {
		// Keyed on the file and the name together: two files carrying one
		// name would otherwise answer for each other, and a failure the
		// repository already had would be charged to the diff.
		if broken || base.OutcomesOf(c.ID()).Any(domain.CaseFail) {
			return fmt.Errorf("%w: %q is red on the base ref too, the repository was broken before the diff",
				domain.ErrHarness, c.Name)
		}
	}
	return nil
}

// failures is every case the suite lost, deduplicated by the pair that names
// one case.
func failures(suite domain.Runs) []domain.Case {
	var failed []domain.Case
	seen := make(map[domain.Case]struct{})
	for _, run := range suite {
		for _, c := range run.Failed() {
			key := domain.Case{File: c.File, Name: c.Name}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			failed = append(failed, c)
		}
	}
	return failed
}

// judged drops the cases tests.known_flaky takes out of the gate entirely.
func (g *SuiteGreen) judged(failed []domain.Case) []domain.Case {
	var out []domain.Case
	for _, c := range failed {
		if !exempt(g.knownFlaky, c) {
			out = append(out, c)
		}
	}
	return out
}

// greenOnRetry reports whether the rerun kept the case green throughout.
//
// A harness that names no case leaves only the file to go by, and there the
// question becomes whether the rerun lost anything in that file at all: an
// unnamed case is not one the outcome lookup could tell from its neighbours.
func greenOnRetry(retried domain.Runs, c domain.Case) bool {
	if len(retried) == 0 {
		return false
	}
	if c.Name != "" {
		return retried.OutcomesOf(c.ID()).All(domain.CasePass)
	}
	for _, run := range retried {
		for _, got := range run.Failed() {
			if got.File == c.File {
				return false
			}
		}
	}
	return true
}

// files is what the retry is filtered to: the files the failures sit in, each
// once. A case the harness attributed to no file leaves nothing to filter by,
// and the retry then covers whatever the others named.
func files(failed []domain.Case) []string {
	var out []string
	for _, c := range failed {
		if c.File != "" && !contains(out, c.File) {
			out = append(out, c.File)
		}
	}
	return out
}

func contains(list []string, want string) bool {
	for _, got := range list {
		if got == want {
			return true
		}
	}
	return false
}

// suiteRefusal is the report line for a suite that lost cases the gate judges.
// askedBase says whether the suite on base actually ran: a line claiming a case
// was green there when nobody looked would be a report inventing a fact.
func suiteRefusal(
	suite domain.Runs, blocking, inherited []domain.Case,
	warnings []domain.Warning, askedBase bool,
) domain.GateResult {
	res := domain.GateResult{
		Status:      domain.StatusFail,
		Remediation: domain.RemediationFixCode,
		Warnings:    warnings,
	}
	n := len(blocking) + len(inherited)
	res.Message = fmt.Sprintf("%d %s red on head, %s", n, plural(n, "test"), passed(suite, len(warnings)))

	for _, c := range blocking {
		res.Evidence = append(res.Evidence, domain.Evidence{
			File: c.File, Case: c.Name, Detail: "touched by the diff",
		})
	}
	detail := "not touched by the diff"
	if askedBase {
		detail += ", green on base"
	}
	for _, c := range inherited {
		res.Evidence = append(res.Evidence, domain.Evidence{File: c.File, Case: c.Name, Detail: detail})
	}
	return res
}

// passed counts what the suite kept, which is the number a reviewer reads first.
func passed(suite domain.Runs, flaky int) string {
	n := 0
	for _, run := range suite {
		for _, c := range run.Cases {
			if c.Outcome == domain.CasePass {
				n++
			}
		}
	}
	line := fmt.Sprintf("%d passed", n)
	if flaky > 0 {
		line += fmt.Sprintf(", %d flaky (untouched)", flaky)
	}
	return line
}
