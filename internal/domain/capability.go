package domain

import "fmt"

// Capability is a precondition a gate needs before it can say anything. The
// runner computes the available set from (Env, Diff, HarnessProbe) and marks a
// gate unavailable with a reason; a gate never checks for its own inputs.
type Capability int

const (
	// CapDiff is always available.
	CapDiff Capability = iota
	// CapHooks means base carries .redfirst/ and --no-hooks was not passed.
	CapHooks
	// CapTestFiles means the diff holds files under tests.patterns or fixtures.
	CapTestFiles
	// CapCaseNames means the inventory run returned named test cases.
	CapCaseNames
	// CapStackTrace means the caller passed a stack trace.
	CapStackTrace
)

func (c Capability) String() string {
	switch c {
	case CapDiff:
		return "diff"
	case CapHooks:
		return "hooks"
	case CapTestFiles:
		return "test-files"
	case CapCaseNames:
		return "case-names"
	case CapStackTrace:
		return "stack-trace"
	}
	return fmt.Sprintf("capability(%d)", int(c))
}

// MissingReason is what an unavailable gate line carries when this capability
// is the one that is absent.
func (c Capability) MissingReason() string {
	switch c {
	case CapDiff:
		return "no diff"
	case CapHooks:
		return "no hooks"
	case CapTestFiles:
		return "no test files in diff"
	case CapCaseNames:
		return "no per-case test names"
	case CapStackTrace:
		return "no stack trace provided"
	}
	return "capability " + c.String() + " unavailable"
}

// MissingHint names the way to make the capability appear. An N/A line is the
// one place in the interface where advertising the next tier belongs.
func (c Capability) MissingHint() string {
	if c == CapHooks {
		return "add .redfirst/ to enable"
	}
	return ""
}
