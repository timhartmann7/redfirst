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
	base    string
	head    string
	repo    string
	config  string
	gates   string
	format  string
	out     string
	timeout time.Duration
	noHooks bool
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
	fs.DurationVar(&f.timeout, "timeout", 30*time.Minute, "global deadline for the whole run")
	fs.BoolVar(&f.noHooks, "no-hooks", false, "force static gates only")

	if err := fs.Parse(args); err != nil {
		return verifyFlags{}, err
	}
	switch f.format {
	case "text", "json", "both":
	default:
		return verifyFlags{}, fmt.Errorf("unknown report format %q, want text, json or both", f.format)
	}
	return f, nil
}

// judge runs every gate and assembles the report around the results.
func judge(ctx context.Context, f verifyFlags) ([]domain.GateResult, domain.Report, error) {
	repo, err := gitx.Open(ctx, f.repo)
	if err != nil {
		return nil, domain.Report{}, err
	}
	cfg, source, err := config.Load(ctx, repo, f.base, f.config)
	if err != nil {
		return nil, domain.Report{}, err
	}
	diff, err := repo.Diff(ctx, f.base, f.head)
	if err != nil {
		return nil, domain.Report{}, err
	}
	// The presence of the hook directory on base is what switches tier 2 on,
	// and the harness owns where it lives.
	hooks, err := repo.Exists(ctx, f.base, harness.HooksDir)
	if err != nil {
		return nil, domain.Report{}, err
	}

	env := runner.Env{HooksPresent: hooks, NoHooks: f.noHooks}
	caps := runner.Capabilities(env, diff, runner.HarnessProbe{}, cfg)
	warnings := downgradeWarnings(&cfg, caps)

	only, err := parseGateIDs(f.gates)
	if err != nil {
		return nil, domain.Report{}, err
	}
	probeKey, err := baseProbeKey(ctx, repo, f.base, diff, cfg, caps)
	if err != nil {
		return nil, domain.Report{}, err
	}
	results, err := runner.Run(ctx, diff, runner.Options{Config: cfg, Caps: caps, Only: only})
	if err != nil {
		return nil, domain.Report{}, err
	}

	rep := assemble(diff, cfg, source, caps, results, warnings)
	rep.DiffDigest = diffDigest(diff)
	rep.BaseProbeKey = probeKey
	return results, rep, nil
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

func assemble(
	diff domain.Diff, cfg domain.Config, source string,
	caps runner.CapSet, results []domain.GateResult, warnings []domain.Warning,
) domain.Report {
	tier := 1
	if caps.Has(domain.CapHooks) {
		tier = 2
	}
	return domain.Report{
		Schema:          domain.SchemaVersion,
		RedfirstVersion: domain.Version,
		Tier:            tier,
		ConfigSource:    source,
		Base:            diff.Base,
		Head:            diff.Head,
		Diff:            diff.Totals(),
		Tests:           diff.StatsMatching(cfg.IsTestSurface),
		Gates:           results,
		Warnings:        append(warnings, gateWarnings(results)...),
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
