package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/timhartmann7/redfirst/internal/config"
	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/gitx"
	"github.com/timhartmann7/redfirst/internal/harness"
	"github.com/timhartmann7/redfirst/internal/report"
	"github.com/timhartmann7/redfirst/internal/runner"
)

type verifyFlags struct {
	base      string
	head      string
	repo      string
	config    string
	gates     string
	format    string
	out       string
	workDir   string
	timeout   time.Duration
	probeRuns int
	noHooks   bool
	freshEnv  bool
}

func verify(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	f, err := parseVerifyFlags(args, stderr)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()

	started := time.Now()
	results, rep, err := judge(ctx, f)
	if err != nil {
		return err
	}
	// Assigned into the block the phases already filled: judge measured the
	// hooks, and this is the wall clock the core's share is subtracted from.
	rep.Timings.TotalS = time.Since(started).Seconds()

	outcome := runner.Outcome(results)
	rep.ExitCode = domain.ExitCode(outcome)
	rep.Verdict = domain.VerdictPass
	if rep.ExitCode != 0 {
		rep.Verdict = domain.VerdictFail
	}

	if err := writeReport(rep, f, stdout); err != nil {
		return err
	}
	return outcome
}

func parseVerifyFlags(args []string, stderr io.Writer) (verifyFlags, error) {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var f verifyFlags
	fs.StringVar(&f.base, "base", "origin/main", "reference branch: the source of config, hooks and rules")
	fs.StringVar(&f.head, "head", "HEAD", "the branch under judgement")
	fs.StringVar(&f.repo, "repo", ".", "path to the git repository")
	fs.StringVar(&f.config, "config", "redfirst.toml", "config path inside the base ref; a missing file is not an error")
	fs.StringVar(&f.gates, "gates", "", "restrict the gate set, comma separated")
	fs.StringVar(&f.format, "report", "text", "report format: text, json or both")
	fs.StringVar(&f.out, "out", "", "write the report to this file instead of stdout")
	fs.StringVar(&f.workDir, "work-dir", "", "where to create the working copies; empty means the system temp")
	fs.DurationVar(&f.timeout, "timeout", 30*time.Minute, "global deadline for the whole run")
	fs.IntVar(&f.probeRuns, "probe-runs", 0, "override red_green.probe_runs; 0 takes the config value")
	fs.BoolVar(&f.noHooks, "no-hooks", false, "force static gates only")
	fs.BoolVar(&f.freshEnv, "fresh-env", false, "forbid service reuse")

	if err := fs.Parse(args); err != nil {
		return verifyFlags{}, err
	}
	switch f.format {
	case "text", "json", "both":
	default:
		return verifyFlags{}, fmt.Errorf("unknown report format %q, want text, json or both", f.format)
	}
	if f.probeRuns < 0 {
		return verifyFlags{}, fmt.Errorf("--probe-runs is %d, a probe cannot run a negative number of times", f.probeRuns)
	}
	return f, nil
}

// judgement is everything the run settles before the first gate speaks: the
// rules, the diff and what the environment offers.
type judgement struct {
	repo   *gitx.Repo
	cfg    domain.Config
	source string
	diff   domain.Diff
	caps   runner.CapSet
	// warnings are the report lines the setup itself produced, such as a mode
	// downgraded for want of hooks.
	warnings []domain.Warning
}

func prepare(ctx context.Context, f verifyFlags) (judgement, error) {
	repo, err := gitx.Open(ctx, f.repo)
	if err != nil {
		return judgement{}, err
	}
	cfg, source, err := config.Load(ctx, repo, f.base, f.config)
	if err != nil {
		return judgement{}, err
	}
	if f.probeRuns > 0 {
		cfg.RedGreen.ProbeRuns = f.probeRuns
	}
	diff, err := repo.Diff(ctx, f.base, f.head)
	if err != nil {
		return judgement{}, err
	}
	// The presence of the hook directory on base is what switches tier 2 on,
	// and the harness owns where it lives.
	hooks, err := repo.Exists(ctx, f.base, harness.HooksDir)
	if err != nil {
		return judgement{}, err
	}

	j := judgement{repo: repo, cfg: cfg, source: source, diff: diff}
	j.caps = runner.Capabilities(
		runner.Env{HooksPresent: hooks, NoHooks: f.noHooks}, diff, runner.HarnessProbe{}, cfg,
	)
	j.warnings = downgradeWarnings(&j.cfg, j.caps)
	return j, nil
}

// judge runs every gate and assembles the report around the results.
func judge(ctx context.Context, f verifyFlags) (
	results []domain.GateResult, rep domain.Report, err error,
) {
	j, err := prepare(ctx, f)
	if err != nil {
		return nil, domain.Report{}, err
	}
	only, err := parseGateIDs(f.gates)
	if err != nil {
		return nil, domain.Report{}, err
	}
	probeKey, err := baseProbeKey(ctx, j.repo, f.base, j.diff, j.cfg, j.caps)
	if err != nil {
		return nil, domain.Report{}, err
	}

	e := j.environment(f)
	// Teardown always runs, and a deferred close is what makes that true on
	// the panic and the deadline alike.
	defer func() {
		if closeErr := e.close(ctx); err == nil && closeErr != nil {
			results, rep, err = nil, domain.Report{}, closeErr
		}
	}()

	results, err = runner.Run(ctx, j.diff, runner.Options{
		Config: j.cfg, Caps: j.caps, Only: only, Open: e.open,
	})
	if err != nil {
		return nil, domain.Report{}, err
	}

	rep = j.assemble(results)
	rep.DiffDigest = diffDigest(j.diff)
	rep.BaseProbeKey = probeKey
	rep.EnvMode = e.mode()
	rep.Timings = e.timings()
	if rep.EnvMode != "" {
		rep.ProbeRuns = j.cfg.RedGreen.ProbeRuns
	}
	return results, rep, nil
}

func (j judgement) environment(f verifyFlags) *env {
	return &env{
		opts: harness.Options{
			// A resolved commit rather than the ref the flag named: a branch
			// that moved mid-run would hand two phases two rule sets.
			Source: j.repo, Base: j.diff.Base, WorkDir: f.workDir, Fresh: f.freshEnv,
		},
		source: j.repo, diff: j.diff, cfg: j.cfg,
	}
}

// downgradeWarnings lowers the modes that need hooks and says so in the report.
// The downgrade moves toward strictness, and it may not happen quietly.
func downgradeWarnings(cfg *domain.Config, caps runner.CapSet) []domain.Warning {
	if caps.Has(domain.CapHooks) {
		return nil
	}
	var warnings []domain.Warning
	for _, key := range config.DowngradeCasesWithoutHooks(cfg) {
		warnings = append(warnings, domain.Warning{
			Kind: domain.WarnConfig,
			Detail: fmt.Sprintf("%s %q needs hooks, downgraded to %q",
				key, domain.ImmutabilityCases, domain.ImmutabilityAppendOnly),
		})
	}
	return warnings
}

func (j judgement) assemble(results []domain.GateResult) domain.Report {
	tier := 1
	if j.caps.Has(domain.CapHooks) {
		tier = 2
	}
	return domain.Report{
		Schema:          domain.SchemaVersion,
		RedfirstVersion: domain.Version,
		Tier:            tier,
		ConfigSource:    j.source,
		Base:            j.diff.Base,
		Head:            j.diff.Head,
		Diff:            j.diff.Totals(),
		Tests:           j.diff.StatsMatching(j.cfg.IsTestSurface),
		Gates:           results,
		Warnings:        append(j.warnings, gateWarnings(results)...),
	}
}

// gateWarnings lifts the verdict-free lines out of the results, in gate order,
// so two runs on the same input order the report block the same way.
func gateWarnings(results []domain.GateResult) []domain.Warning {
	var ws []domain.Warning
	for _, r := range results {
		ws = append(ws, r.Warnings...)
	}
	return ws
}

func parseGateIDs(list string) ([]domain.GateID, error) {
	if list == "" {
		return nil, nil
	}
	known := domain.AllGateIDs()
	var ids []domain.GateID
	for _, raw := range strings.Split(list, ",") {
		id := domain.GateID(strings.TrimSpace(raw))
		if !slices.Contains(known, id) {
			return nil, fmt.Errorf("unknown gate %q", id)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// writeReport sends the report to --out or to stdout. Those, plus stderr, are
// the only things redfirst writes.
func writeReport(rep domain.Report, f verifyFlags, stdout io.Writer) (err error) {
	w := stdout
	if f.out != "" {
		file, createErr := os.Create(f.out)
		if createErr != nil {
			return fmt.Errorf("create report file: %w", createErr)
		}
		defer func() {
			if closeErr := file.Close(); err == nil && closeErr != nil {
				err = fmt.Errorf("close report file: %w", closeErr)
			}
		}()
		w = file
	}

	if f.format == "text" || f.format == "both" {
		if err := report.Text(w, rep); err != nil {
			return fmt.Errorf("render text report: %w", err)
		}
	}
	if f.format == "json" || f.format == "both" {
		if err := report.JSON(w, rep); err != nil {
			return fmt.Errorf("render json report: %w", err)
		}
	}
	return nil
}
