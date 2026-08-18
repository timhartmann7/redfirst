package runner

import (
	"context"
	"fmt"
	"slices"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// Options are the per-run choices the CLI passes in.
type Options struct {
	Config domain.Config
	Caps   CapSet
	Only   []domain.GateID // --gates; empty means every gate
	// Open brings the project's environment up and hands back the probe the
	// executing gates read it through. The runner calls it at most once, right
	// before the first gate that needs hooks, and never once a static gate has
	// refused: that lazy call is what the short-circuit is worth. Nil where the
	// run has no hooks.
	Open func(ctx context.Context) (domain.Probe, error)
}

// Run executes every registered gate in order and returns one result per gate,
// in registry order.
func Run(ctx context.Context, diff domain.Diff, opts Options) ([]domain.GateResult, error) {
	return runEntries(ctx, Registry(), diff, opts)
}

func runEntries(
	ctx context.Context, entries []Entry, diff domain.Diff, opts Options,
) ([]domain.GateResult, error) {
	ex := execution{
		opts: opts,
		in:   domain.Input{Diff: diff, Config: opts.Config},
		caps: opts.Caps,
	}
	results := make([]domain.GateResult, 0, len(entries))
	for _, e := range entries {
		res, err := ex.step(ctx, e)
		if err != nil {
			return nil, err
		}
		results = append(results, res)
	}
	return results, nil
}

// execution is the state of one walk over the registry. It lives for the length
// of a single call, so the runner keeps no state between runs.
type execution struct {
	opts Options
	in   domain.Input
	// caps grows during the walk: CapCaseNames arrives with the inventory run,
	// which happens ahead of the first gate that reads it and not before.
	caps CapSet
	// failed is the first gate that refused. It arms the short-circuit.
	failed domain.GateID
	// opened records that the environment was brought up, so a second gate
	// asking for it does not pay for a second one.
	opened bool
}

func (ex *execution) step(ctx context.Context, e Entry) (domain.GateResult, error) {
	res, err := ex.decide(ctx, e.ID, e.New(ex.opts.Config))
	if err != nil {
		// A gate that cannot finish leaves the run without a verdict: a broken
		// harness must not read as an acquittal.
		return domain.GateResult{}, fmt.Errorf("gate %s: %w", e.ID, err)
	}
	// The registry, not the gate, owns the identity of a report line.
	res.ID = e.ID
	res = applySeverity(res, ex.opts.Config)
	if res.Status == domain.StatusFail && ex.failed == "" {
		ex.failed = e.ID
	}
	return res, nil
}

func (ex *execution) decide(ctx context.Context, id domain.GateID, gate Gate) (domain.GateResult, error) {
	if len(ex.opts.Only) > 0 && !slices.Contains(ex.opts.Only, id) {
		return skipped("not selected by --gates"), nil
	}
	if ex.opts.Config.GateDisabled(id) {
		return skipped("disabled in config"), nil
	}
	requires := gate.Requires()
	if slices.Contains(requires, domain.CapHooks) && ex.caps.Has(domain.CapHooks) {
		// The short-circuit: static gates cost milliseconds, the environment
		// costs minutes, and a diff already refused earns neither.
		if ex.failed != "" {
			return skipped(fmt.Sprintf("short-circuit: %s failed", ex.failed)), nil
		}
		if err := ex.bringUp(ctx); err != nil {
			return domain.GateResult{}, err
		}
	}
	if c, missing := firstMissing(requires, ex.caps); missing {
		return domain.GateResult{
			Status: domain.StatusUnavailable,
			Reason: c.MissingReason(),
			Hint:   c.MissingHint(),
		}, nil
	}
	return gate.Run(ctx, ex.in)
}

// bringUp starts the environment and performs the inventory run.
//
// The inventory is what CapCaseNames comes from, and the spec puts it ahead of
// any tier 2 gate for that reason: a gate never runs with a capability nobody
// has settled. A diff that modifies no existing test file has no inventory to
// run and no existing case to lose, so the capability holds there with nothing
// to show for it.
func (ex *execution) bringUp(ctx context.Context) error {
	if ex.opened || ex.opts.Open == nil {
		return nil
	}
	ex.opened = true

	probe, err := ex.opts.Open(ctx)
	if err != nil {
		return err
	}
	ex.in.Probe = probe

	inventory, err := probe.Inventory(ctx)
	if err != nil {
		return err
	}
	if len(inventory) == 0 || inventory.Named() {
		ex.caps |= NewCapSet(domain.CapCaseNames)
	}
	return nil
}

// applySeverity downgrades a refusal the config declared non-blocking. The
// remediation goes with it: only a fail carries one.
//
// A human-required refusal survives the downgrade. It means no diff can fix
// this, so turning it into a report line would hand back exit code 0 for a run
// that has no legal move, and invariant 2 forbids resolving that to a pass.
func applySeverity(res domain.GateResult, cfg domain.Config) domain.GateResult {
	if res.Remediation == domain.RemediationHumanRequired {
		return res
	}
	if res.Status != domain.StatusFail || cfg.SeverityOf(res.ID) != domain.SeverityWarn {
		return res
	}
	res.Status = domain.StatusWarn
	res.Remediation = ""
	return res
}

func firstMissing(caps []domain.Capability, have CapSet) (domain.Capability, bool) {
	for _, c := range caps {
		if !have.Has(c) {
			return c, true
		}
	}
	return domain.CapDiff, false
}

func skipped(reason string) domain.GateResult {
	return domain.GateResult{Status: domain.StatusSkip, Reason: reason}
}

// Outcome turns the results into the run's error. One human-required anywhere
// outranks everything else: a retry against it is pointless whatever the rest
// of the report says.
func Outcome(results []domain.GateResult) error {
	violated := false
	for _, r := range results {
		if r.Remediation == domain.RemediationHumanRequired {
			return domain.ErrHumanRequired
		}
		if r.Status == domain.StatusFail {
			violated = true
		}
	}
	if violated {
		return domain.ErrGateViolation
	}
	return nil
}
