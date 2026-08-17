package domain

import "errors"

// The sentinels below carry the exit code contract from section 5 of the spec.
// Wrap them with %w and branch on them with errors.Is; nothing else decides an
// exit code.
var (
	// ErrGateViolation is a gate refusal the agent has a legal move against.
	ErrGateViolation = errors.New("gate violation")
	// ErrHarness is a broken harness or a repository that was already broken.
	ErrHarness = errors.New("harness failure")
	// ErrConfig is a config that exists and is invalid. A missing config is not.
	ErrConfig = errors.New("invalid configuration")
	// ErrInternal is a redfirst bug.
	ErrInternal = errors.New("internal error")
	// ErrHumanRequired is a refusal that survives any diff the agent can write.
	ErrHumanRequired = errors.New("no legal move for the agent")
)

// ExitCode maps an error onto the orchestrator contract. Anything unrecognised
// is our own bug and reports 4 rather than blaming the agent.
func ExitCode(err error) int {
	switch {
	case err == nil:
		return 0
	// human-required outranks everything: one of them anywhere in the report
	// makes a retry pointless, whatever else failed.
	case errors.Is(err, ErrHumanRequired):
		return 5
	case errors.Is(err, ErrConfig):
		return 3
	case errors.Is(err, ErrHarness):
		return 2
	case errors.Is(err, ErrGateViolation):
		return 1
	}
	return 4
}
