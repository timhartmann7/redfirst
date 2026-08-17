package domain

// GateID is the one word a gate carries through the config, the report, the
// JSON and its own filename.
type GateID string

const (
	GateProtectedPaths   GateID = "protected-paths"
	GateDiffBudget       GateID = "diff-budget"
	GateTestImmutability GateID = "test-immutability"
	GateNewTestPresent   GateID = "new-test-present"
	GateNoDisabledTests  GateID = "no-disabled-tests"
	GateSuppressionScan  GateID = "suppression-scan"
	GateSuiteGreen       GateID = "suite-green"
	GateRedGreen         GateID = "red-green"
	GateBlastRadius      GateID = "blast-radius"
)

// AllGateIDs lists every declared gate. Config validation rejects a severity
// key or a gates.disabled entry outside this set, and a runner test checks the
// registry against it.
func AllGateIDs() []GateID {
	return []GateID{
		GateProtectedPaths,
		GateDiffBudget,
		GateTestImmutability,
		GateNewTestPresent,
		GateNoDisabledTests,
		GateSuppressionScan,
		GateSuiteGreen,
		GateRedGreen,
		GateBlastRadius,
	}
}

// GateStatus is the outcome of one gate.
type GateStatus string

const (
	// StatusPass means checked, no violations.
	StatusPass GateStatus = "pass"
	// StatusFail is a violation.
	StatusFail GateStatus = "fail"
	// StatusWarn is a note for the reviewer.
	StatusWarn GateStatus = "warn"
	// StatusSkip means a user choice: disabled in config, or cut off by the
	// short-circuit.
	StatusSkip GateStatus = "skip"
	// StatusUnavailable means a missing capability. It always carries a reason.
	StatusUnavailable GateStatus = "unavailable"
)

// Remediation is the machine-readable instruction the orchestrator puts into
// the agent context. A gate declares it statically, by failure type.
type Remediation string

const (
	RemediationRevertPaths   Remediation = "revert-paths"
	RemediationReduceScope   Remediation = "reduce-scope"
	RemediationAddTest       Remediation = "add-test"
	RemediationFixTest       Remediation = "fix-test"
	RemediationFixCode       Remediation = "fix-code"
	RemediationHumanRequired Remediation = "human-required"
)

// Evidence says what exactly was violated and where. A refusal without it
// reads as "check failed", which the report may not do.
type Evidence struct {
	File     string   `json:"file,omitempty"`
	Case     string   `json:"case,omitempty"`
	Line     int      `json:"line,omitempty"`
	BaseRuns []string `json:"base_runs,omitempty"`
	HeadRuns []string `json:"head_runs,omitempty"`
	Detail   string   `json:"detail,omitempty"`
}

// GateResult is one line of the report.
type GateResult struct {
	ID     GateID     `json:"id"`
	Status GateStatus `json:"status"`
	// Message says what is wrong, not what got checked.
	Message string `json:"message,omitempty"`
	// Reason is why an unavailable or skipped gate did not run.
	Reason string `json:"reason,omitempty"`
	// Hint is how to make an unavailable gate available.
	Hint        string      `json:"hint,omitempty"`
	Remediation Remediation `json:"remediation,omitempty"`
	Evidence    []Evidence  `json:"evidence,omitempty"`
	// Warnings are the lines the gate found that leave the verdict alone: a
	// guarded path, a suppression hit. They travel on the result so a gate
	// keeps its whole output in one value, and the report collects them into
	// its own block. Not serialised here: the contract carries them once, at
	// the top level.
	Warnings []Warning `json:"-"`
}

// Input is everything a gate may read. It carries no capability set: the
// runner decides availability before the gate runs.
type Input struct {
	Diff   Diff
	Config Config
}
