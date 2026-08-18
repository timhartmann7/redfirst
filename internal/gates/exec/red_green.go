package exec

import (
	"context"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// RedGreen checks that the added test cases catch the bug the fix was written
// for: red without the fix, green with it, every run of both.
//
// The unit is a case rather than a file. File granularity leaves an open
// channel: the agent appends a fake case to an existing test file, the diff
// holds no new file, and a gate that probes files finds nothing to probe. The
// case names come from someone else's test runner, never from a source parse.
type RedGreen struct {
	compile domain.CompileFailure
}

// NewRedGreen builds the gate. The config arrives at construction rather than
// at Run: Requires() has to stay fixed for a whole run.
func NewRedGreen(cfg domain.Config) *RedGreen {
	return &RedGreen{compile: cfg.RedGreen.CompileFailureOnBase}
}

// ID names the gate in the config, the report and the JSON.
func (g *RedGreen) ID() domain.GateID { return domain.GateRedGreen }

// Requires declares what the gate needs before the runner lets it speak.
//
// CapCaseNames is deliberately not on the list. Missing case names change what
// this gate answers rather than whether it may answer: the spec fixes that
// answer at a refusal with no legal move, and an unavailable line hands back
// exit code 0. Where a diff adds test files and modifies none, the same absence
// costs nothing at all and the gate probes by file.
func (g *RedGreen) Requires() []domain.Capability {
	return []domain.Capability{domain.CapDiff, domain.CapHooks, domain.CapTestFiles}
}

// Run walks the algorithm from section 8 of the spec: inventory the cases base
// already had, overlay the head versions of the test files onto base, and
// demand that what the diff added be red there and green on head.
func (g *RedGreen) Run(ctx context.Context, in domain.Input) (domain.GateResult, error) {
	probe, err := probeOf(in)
	if err != nil {
		return domain.GateResult{}, err
	}
	inventory, err := probe.Inventory(ctx)
	if err != nil {
		return domain.GateResult{}, err
	}
	if len(inventory) > 0 && !inventory.Named() {
		return namesMissing(), nil
	}

	base, err := probe.Overlaid(ctx)
	if err != nil {
		return domain.GateResult{}, err
	}
	if len(base) == 0 {
		// Nothing of the diff's test surface is on head to probe: the diff
		// removed test files and added none.
		return noNewCase(), nil
	}

	before := inventory.Names()
	if added := missing(base.Names(), before); len(added) > 0 {
		return g.byCase(ctx, probe, base, added)
	}
	if base.Named() {
		// The probe named cases and base already had every one of them.
		return noNewCase(), nil
	}
	return g.byFile(ctx, probe, base, before)
}

// byCase is the check the gate exists for: every case the diff added has to be
// red on base in every run and green on head in every run.
func (g *RedGreen) byCase(
	ctx context.Context, probe domain.Probe, base domain.Runs, added []string,
) (domain.GateResult, error) {
	if res, refused := judgeBase(base, added); refused {
		return res, nil
	}
	head, err := probe.Head(ctx)
	if err != nil {
		return domain.GateResult{}, err
	}
	return judgeHead(base, head, added), nil
}

// byFile is what the gate falls back to where the base probe named no case at
// all. Two situations produce that, and neither leaves a case to judge: a new
// test that does not build without the fix, and a harness that reports no
// per-case results for a diff of new test files.
//
// The check degenerates to the file set, and the report has to say so, because
// "the code does not build without the fix" claims less than "the test catches
// the bug".
func (g *RedGreen) byFile(
	ctx context.Context, probe domain.Probe, base domain.Runs, before []string,
) (domain.GateResult, error) {
	if g.compile == domain.CompileFailureFail {
		return refuse("the added tests do not build on base, and the config counts that as a violation",
			domain.RemediationFixTest), nil
	}
	if res, refused := judgeBaseByFile(base); refused {
		return res, nil
	}
	head, err := probe.Head(ctx)
	if err != nil {
		return domain.GateResult{}, err
	}

	// The head run names what the base run could not, and the spec substitutes
	// those names for the added set: a per-case check on head still says more
	// than a file-level one.
	res := judgeHeadByName(base, head, missing(head.Names(), before))
	if g.compile == domain.CompileFailureWeakRed {
		res.Warnings = append(res.Warnings, domain.Warning{
			Kind: domain.WarnConfig,
			Detail: "red-green judged by file: the base probe named no case, so the red on base " +
				"says the tests did not build rather than that an assertion caught the bug",
		})
	}
	return res, nil
}

// judgeHeadByName judges head per case where the head run named any, and by the
// file set where it named none either.
func judgeHeadByName(base, head domain.Runs, added []string) domain.GateResult {
	if len(added) > 0 {
		return judgeHead(base, head, added)
	}
	return judgeHeadByFile(head)
}
