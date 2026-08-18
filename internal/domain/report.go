package domain

// Version is the redfirst release. S5 replaces it at link time.
const Version = "0.1.0"

// SchemaVersion is the report contract in schema/report.v1.json. Adding a
// field needs no bump; renaming or removing one does.
const SchemaVersion = 1

// Verdict is the run outcome as a word. The exit code carries the detail.
type Verdict string

const (
	VerdictPass Verdict = "pass"
	VerdictFail Verdict = "fail"
)

// WarningKind groups the report lines that leave the verdict alone.
type WarningKind string

const (
	// WarnGuardedPaths is a diff file under paths.guarded.
	WarnGuardedPaths WarningKind = "guarded-paths"
	// WarnSuppression is a suppression-scan hit.
	WarnSuppression WarningKind = "suppression"
	// WarnFlaky is a failing test the diff did not touch.
	WarnFlaky WarningKind = "flaky"
	// WarnConfig is a rule the run had to adjust, such as an immutability
	// mode downgraded for want of hooks.
	WarnConfig WarningKind = "config"
)

// Warning is a report line that never changes the verdict.
type Warning struct {
	Kind    WarningKind `json:"kind"`
	File    string      `json:"file,omitempty"`
	Line    int         `json:"line,omitempty"`
	Pattern string      `json:"pattern,omitempty"`
	Detail  string      `json:"detail,omitempty"`
}

// Timings are the only part of the report that changes between two runs on the
// same input.
type Timings struct {
	TotalS float64 `json:"total_s"`
}

// Report is the contract with the orchestrator.
type Report struct {
	Schema          int    `json:"schema"`
	RedfirstVersion string `json:"redfirst_version"`
	Tier            int    `json:"tier"`
	// ConfigSource is "defaults" or "base:<path>", so the orchestrator sees
	// whether project rules or built-in ones judged the diff.
	ConfigSource string `json:"config_source"`
	Base         string `json:"base"`
	Head         string `json:"head"`
	EnvMode      string `json:"env_mode,omitempty"`
	ProbeRuns    int    `json:"probe_runs,omitempty"`
	// DiffDigest names the patch under judgement, BaseProbeKey the base probe
	// of red-green. The core computes both and reads neither: the orchestrator
	// deduplicates attempts with the first, and the second exists so that
	// months of logs can answer whether a cache would have paid for itself.
	// BaseProbeKey stays empty where no base probe would run.
	DiffDigest   string       `json:"diff_digest,omitempty"`
	BaseProbeKey string       `json:"base_probe_key,omitempty"`
	Verdict      Verdict      `json:"verdict"`
	ExitCode     int          `json:"exit_code"`
	Diff         Stats        `json:"diff"`
	Tests        Stats        `json:"tests"`
	Gates        []GateResult `json:"gates"`
	Warnings     []Warning    `json:"warnings"`
	Timings      Timings      `json:"timings"`
}
