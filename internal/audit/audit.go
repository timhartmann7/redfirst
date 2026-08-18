// Package audit applies the static gates to the history a repository has
// already merged. It is tier 0: it reads, it prints, and it brings no
// environment up even where the base ref carries hooks.
package audit

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/timhartmann7/redfirst/internal/config"
	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/gitx"
	"github.com/timhartmann7/redfirst/internal/runner"
)

// Options are the choices the CLI passes in.
type Options struct {
	// Base is the ref things were merged into, spelled as the caller wrote it:
	// the header names it, so a resolved sha would say less.
	Base string
	// Unit is what counts as one piece of history. ModeAuto asks for detection.
	Unit Mode
	// Last caps the number of units. Since replaces it when set.
	Last int
	// Since is a git date expression, the alternative to Last.
	Since string
	// Config is the rule set, already loaded from Base.
	Config domain.Config
}

// Finding is one category and the number of units it fired on.
type Finding struct {
	Category string `json:"category"`
	Units    int    `json:"units"`
}

// Unit is one piece of history and what the gates found in it.
type Unit struct {
	Commit string `json:"commit"`
	// PR is the pull request number a squashed subject carries, empty in the
	// other two modes.
	PR      string `json:"pr,omitempty"`
	Subject string `json:"subject"`
	Date    string `json:"date"`
	// Diff counts every file. Tests counts the part of it that decides test
	// outcomes, which diff-budget leaves out of its own count.
	Diff     domain.Stats `json:"diff"`
	Tests    domain.Stats `json:"tests"`
	Findings []string     `json:"findings"`
}

// Summary is one audit run.
type Summary struct {
	Base string `json:"base"`
	Unit Mode   `json:"unit"`
	// Caveat is what the mode costs in accuracy. It reaches the header rather
	// than a footnote: a reader comparing two repositories has to see it.
	Caveat string `json:"caveat"`
	// Units counts what was audited, whether or not PerUnit reaches the report.
	Units int `json:"units"`
	// Days is how much history the units cover, oldest to newest.
	Days int `json:"days"`
	// Notes are the rules the run had to adjust before it could judge anything.
	Notes    []string  `json:"notes,omitempty"`
	Findings []Finding `json:"findings"`
	PerUnit  []Unit    `json:"per_unit,omitempty"`
}

// Run walks the history of the base ref and applies the static gates to every
// unit it finds.
func Run(ctx context.Context, repo *gitx.Repo, opts Options) (Summary, error) {
	mode, err := resolveMode(ctx, repo, opts)
	if err != nil {
		return Summary{}, err
	}
	commits, err := walk(ctx, repo, mode, opts)
	if err != nil {
		return Summary{}, err
	}

	cfg := opts.Config
	// A mode that judges outcomes has no outcomes here: audit runs nothing, so
	// it holds no case names, and the downgrade toward the stricter line-based
	// mode is the same one a verify run without hooks performs.
	notes := downgradeNotes(config.DowngradeCasesWithoutHooks(&cfg))

	units := make([]Unit, 0, len(commits))
	for _, c := range commits {
		u, err := judge(ctx, repo, mode, c, cfg)
		if err != nil {
			return Summary{}, err
		}
		units = append(units, u)
	}

	return Summary{
		Base:     opts.Base,
		Unit:     mode,
		Caveat:   mode.caveat(),
		Units:    len(units),
		Days:     span(commits),
		Notes:    notes,
		Findings: summarise(units),
		PerUnit:  units,
	}, nil
}

// judge runs every gate over the diff of one unit.
func judge(
	ctx context.Context, repo *gitx.Repo, mode Mode, c gitx.Commit, cfg domain.Config,
) (Unit, error) {
	diff, err := unitDiff(ctx, repo, mode, c)
	if err != nil {
		return Unit{}, err
	}

	// The zero Env is the point of this command: no hooks, whatever the base
	// ref carries, so the gates that need an environment stay unavailable and
	// nothing gets started.
	caps := runner.Capabilities(runner.Env{}, diff, runner.HarnessProbe{}, cfg)
	results, err := runner.Run(ctx, diff, runner.Options{Config: cfg, Caps: caps})
	if err != nil {
		return Unit{}, fmt.Errorf("judging %s: %w", c.SHA, err)
	}

	return Unit{
		Commit:   c.SHA,
		PR:       pullRequestNumber(mode, c.Subject),
		Subject:  c.Subject,
		Date:     c.Date.Format(time.DateOnly),
		Diff:     diff.Totals(),
		Tests:    diff.StatsMatching(cfg.IsTestSurface),
		Findings: categories(results),
	}, nil
}

// categories names what one unit tripped, in execution order and once each.
//
// The entries are the lines the report itself prints: a gate that refused or
// warned, and a category of its own for every warning a gate raised beside its
// verdict. Suppression is the exception the report already makes, for the same
// reason: those lines restate what suppression-scan just said, while a guarded
// path is a fact about the files that gate deliberately let through, and a
// refusal over some other file may not swallow it.
func categories(results []domain.GateResult) []string {
	var out []string
	seen := make(map[string]bool, len(results))
	add := func(c string) {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}

	for _, r := range results {
		if r.Status == domain.StatusFail || r.Status == domain.StatusWarn {
			add(string(r.ID))
		}
		for _, w := range r.Warnings {
			if w.Kind != domain.WarnSuppression {
				add(string(w.Kind))
			}
		}
	}
	return out
}

// summarise counts the units every category fired on. The gates print in execution
// order and anything else in the order it first appeared, so two runs over one
// repository print the same lines in the same order.
func summarise(units []Unit) []Finding {
	counts := make(map[string]int)
	var beyondGates []string
	for _, u := range units {
		for _, c := range u.Findings {
			if counts[c] == 0 && !isGate(c) {
				beyondGates = append(beyondGates, c)
			}
			counts[c]++
		}
	}

	findings := make([]Finding, 0, len(counts))
	for _, id := range domain.AllGateIDs() {
		if n := counts[string(id)]; n > 0 {
			findings = append(findings, Finding{Category: string(id), Units: n})
		}
	}
	for _, c := range beyondGates {
		findings = append(findings, Finding{Category: c, Units: counts[c]})
	}
	return findings
}

func isGate(category string) bool {
	return slices.Contains(domain.AllGateIDs(), domain.GateID(category))
}

// downgradeNotes phrases the config keys the run had to lower.
func downgradeNotes(keys []string) []string {
	notes := make([]string, 0, len(keys))
	for _, k := range keys {
		notes = append(notes, fmt.Sprintf("%s %q needs hooks, audited as %q",
			k, domain.ImmutabilityCases, domain.ImmutabilityAppendOnly))
	}
	if len(notes) == 0 {
		return nil
	}
	return notes
}
