package domain

import (
	"context"
	"slices"
	"time"
)

// Phase is one half of red-green, plus the suite run. The source tree differs
// between phases, so each phase gets a working copy of its own; every run
// inside a phase shares it, because the tree is identical byte for byte and a
// rebuild would change nothing.
type Phase string

const (
	PhaseBase  Phase = "base"
	PhaseHead  Phase = "head"
	PhaseSuite Phase = "suite"
)

// CaseOutcome is what a results file says about one case.
type CaseOutcome string

const (
	CasePass CaseOutcome = "pass"
	CaseFail CaseOutcome = "fail"
	CaseSkip CaseOutcome = "skip"
	// CaseAbsent is a run that never mentioned the case. No results file
	// carries it: the aggregation produces it, because a case that vanished
	// from a run is not a case that passed.
	CaseAbsent CaseOutcome = "absent"
)

// Case is one named test result.
//
// The name is the unit red-green judges by. File granularity leaves open the
// channel the gate exists to close: a case appended to an existing test file,
// with no new file anywhere in the diff.
type Case struct {
	// File is the JUnit classname. TAP names no file and leaves it empty.
	File    string
	Name    string
	Outcome CaseOutcome
}

// Run is one execution of the project's test hook.
type Run struct {
	// Index is the run's number inside its phase, starting at one.
	Index int
	// Took is how long the hook process ran. It is what the report's timings
	// block is built from, and what the core's own share is measured against.
	Took time.Duration
	// Green is the hook having exited zero. What green means is the hook's call.
	Green bool
	// Cases is what the results file named. It stays empty where the harness
	// reports no per-case results, which costs the run a capability rather than
	// failing it.
	Cases []Case
}

// Named reports whether the run carries per-case names.
func (r Run) Named() bool {
	return slices.ContainsFunc(r.Cases, func(c Case) bool { return c.Name != "" })
}

// Failed is every case the run reported as failing.
func (r Run) Failed() []Case {
	var failed []Case
	for _, c := range r.Cases {
		if c.Outcome == CaseFail {
			failed = append(failed, c)
		}
	}
	return failed
}

// Runs is what one probe produced: one entry per execution of the hook.
type Runs []Run

// Took is how long the whole probe spent inside hook processes.
func (r Runs) Took() time.Duration {
	var total time.Duration
	for _, run := range r {
		total += run.Took
	}
	return total
}

// Named reports whether any run named a case. Without names red-green cannot
// tell a case the diff added from one both versions of the file already held.
func (r Runs) Named() bool {
	return slices.ContainsFunc(r, Run.Named)
}

// AllGreen reports whether every run exited zero. An empty set is not green:
// a probe that never ran proves nothing.
func (r Runs) AllGreen() bool {
	return len(r) > 0 && !slices.ContainsFunc(r, func(run Run) bool { return !run.Green })
}

// AnyGreen reports whether at least one run exited zero.
func (r Runs) AnyGreen() bool {
	return slices.ContainsFunc(r, func(run Run) bool { return run.Green })
}

// Names is every case name the runs mention, in first-seen order, so two runs
// on the same input produce the same report.
func (r Runs) Names() []string {
	var names []string
	seen := make(map[string]struct{})
	for _, run := range r {
		for _, c := range run.Cases {
			if _, ok := seen[c.Name]; ok || c.Name == "" {
				continue
			}
			seen[c.Name] = struct{}{}
			names = append(names, c.Name)
		}
	}
	return names
}

// File is the file the runs attribute a case to, empty where the harness named
// none. TAP names no file, and a report line then carries the case alone.
func (r Runs) File(name string) string {
	for _, run := range r {
		for _, c := range run.Cases {
			if c.Name == name && c.File != "" {
				return c.File
			}
		}
	}
	return ""
}

// Outcomes is what every run said about one case, in run order.
func (r Runs) Outcomes(name string) Outcomes {
	out := make(Outcomes, 0, len(r))
	for _, run := range r {
		out = append(out, run.outcome(name))
	}
	return out
}

func (r Run) outcome(name string) CaseOutcome {
	for _, c := range r.Cases {
		if c.Name == name {
			return c.Outcome
		}
	}
	return CaseAbsent
}

// Outcomes is one case seen across the runs of a probe. K-of-K lives here: the
// gates ask whether every run agreed, never whether one of them did.
type Outcomes []CaseOutcome

// All reports whether every run produced want. An empty set does not.
func (o Outcomes) All(want CaseOutcome) bool {
	return len(o) > 0 && !slices.ContainsFunc(o, func(got CaseOutcome) bool { return got != want })
}

// Any reports whether at least one run produced want.
func (o Outcomes) Any(want CaseOutcome) bool { return slices.Contains(o, want) }

// Strings is the outcomes as the report prints them, run by run.
func (o Outcomes) Strings() []string {
	out := make([]string, 0, len(o))
	for _, got := range o {
		out = append(out, string(got))
	}
	return out
}

// Probe is the project's own harness as an executing gate reads it.
//
// Every run it names happens at most once and is shared. Two gates ask for the
// same inventory and the same head run, and a gate that drove the environment
// itself would pay for a working copy twice and would have to know that the
// other gate exists, which is the one thing gates may not know.
//
// The runner fills it while bringing the environment up. It is nil for a run
// without hooks, which the capability model turned into an unavailable line
// before any gate saw it.
type Probe interface {
	// Inventory is the test files of the diff as the base ref has them, run
	// once with nothing overlaid. It is empty where the diff adds test files
	// and modifies none: base holds nothing to inventory.
	Inventory(ctx context.Context) (Runs, error)
	// Overlaid is the base tree carrying the head versions of those files and
	// nothing else, run red_green.probe_runs times. The fix does not travel.
	Overlaid(ctx context.Context) (Runs, error)
	// Head is the same files on head, run red_green.probe_runs times.
	Head(ctx context.Context) (Runs, error)
	// Suite is the whole suite on head, unfiltered, run once.
	Suite(ctx context.Context) (Runs, error)
	// Retry reruns the given files on head suite.retry_failed times. It is
	// empty where the config asks for no retry.
	Retry(ctx context.Context, files []string) (Runs, error)
	// BaseSuite is the whole suite on the merge base. It costs a working copy
	// of its own and runs on one path only: the suite red on head, the failures
	// untouched by the diff, the retry no help.
	BaseSuite(ctx context.Context) (Runs, error)
}
