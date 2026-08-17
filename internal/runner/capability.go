package runner

import "github.com/timhartmann7/redfirst/internal/domain"

// Env is what the run environment offers. The runner turns it into
// capabilities; a gate never sees it.
type Env struct {
	HooksPresent bool // base carries .redfirst/
	NoHooks      bool // --no-hooks was passed
	StackTrace   bool // the caller passed a stack trace
}

// HarnessProbe carries the inventory run's outcome. S4 fills it.
type HarnessProbe struct {
	CaseNames bool
}

// CapSet is the set of capabilities available to this run.
type CapSet uint

// NewCapSet builds a set holding exactly the capabilities given.
func NewCapSet(caps ...domain.Capability) CapSet {
	var s CapSet
	for _, c := range caps {
		s |= 1 << uint(c)
	}
	return s
}

// Has reports whether the capability is available to this run.
func (s CapSet) Has(c domain.Capability) bool { return s&(1<<uint(c)) != 0 }

// Capabilities computes the available set from (Env, Diff, HarnessProbe).
// Three of the five depend on the run input rather than on the environment, so
// deriving the set from the environment alone would push the missing checks
// back into the gates, which is what the capability model exists to prevent.
func Capabilities(env Env, diff domain.Diff, probe HarnessProbe, cfg domain.Config) CapSet {
	caps := []domain.Capability{domain.CapDiff}
	if env.HooksPresent && !env.NoHooks {
		caps = append(caps, domain.CapHooks)
	}
	if touchesTestSurface(diff, cfg) {
		caps = append(caps, domain.CapTestFiles)
	}
	if probe.CaseNames {
		caps = append(caps, domain.CapCaseNames)
	}
	if env.StackTrace {
		caps = append(caps, domain.CapStackTrace)
	}
	return NewCapSet(caps...)
}

// touchesTestSurface looks at the source path of a rename too: a test moved out
// of the surface changes test outcomes just as much as one moved into it.
func touchesTestSurface(diff domain.Diff, cfg domain.Config) bool {
	for _, f := range diff.Files {
		if cfg.IsTestSurface(f.Path) {
			return true
		}
		if f.OldPath != "" && cfg.IsTestSurface(f.OldPath) {
			return true
		}
	}
	return false
}
