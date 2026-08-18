package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/timhartmann7/redfirst/internal/audit"
	"github.com/timhartmann7/redfirst/internal/config"
	"github.com/timhartmann7/redfirst/internal/gitx"
)

type auditFlags struct {
	repo   string
	base   string
	last   int
	since  string
	unit   audit.Mode
	format audit.Format
	perPR  bool
}

// auditHistory reads what a repository has already merged and applies the
// static gates to it. It executes nothing, installs nothing and adds no file to
// the repository: this is the tier that exists for recognition.
func auditHistory(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	f, err := parseAuditFlags(args, stderr)
	if err != nil {
		return err
	}

	repo, err := gitx.Open(ctx, f.repo)
	if err != nil {
		return err
	}
	cfg, _, err := config.Load(ctx, repo, f.base, configPath)
	if err != nil {
		return err
	}

	summary, err := audit.Run(ctx, repo, audit.Options{
		Base:   f.base,
		Unit:   f.unit,
		Last:   f.last,
		Since:  f.since,
		Config: cfg,
	})
	if err != nil {
		return err
	}
	return audit.Render(stdout, summary, f.format, f.perPR)
}

func parseAuditFlags(args []string, stderr io.Writer) (auditFlags, error) {
	fs := flag.NewFlagSet("audit", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		f    auditFlags
		unit string
		rep  string
	)
	fs.StringVar(&f.repo, "repo", ".", "path to the git repository")
	fs.StringVar(&f.base, "base", "origin/main", "what things were merged into")
	fs.IntVar(&f.last, "last", 20, "how many recent history units to analyse")
	fs.StringVar(&f.since, "since", "", "analyse everything merged since this date, instead of --last")
	fs.StringVar(&unit, "unit", string(audit.ModeAuto), "what counts as a unit: auto, merge, squash or commit")
	fs.StringVar(&rep, "report", string(audit.FormatText), "report format: text, json or csv")
	fs.BoolVar(&f.perPR, "per-pr", false, "print a breakdown per unit, not only the summary")

	if err := fs.Parse(args); err != nil {
		return auditFlags{}, err
	}

	var err error
	if f.unit, err = audit.ParseMode(unit); err != nil {
		return auditFlags{}, err
	}
	if f.format, err = audit.ParseFormat(rep); err != nil {
		return auditFlags{}, err
	}
	// Zero would reach the walk as "no limit" and audit the whole history,
	// which is not what anyone writing --last 0 asked for.
	if f.last < 1 {
		return auditFlags{}, fmt.Errorf("--last %d, want at least one unit", f.last)
	}
	return f, nil
}
