// Package cliux holds the commands that set a repository up and say where it
// stands. None of them judges a diff: `init` writes the files a project needs,
// `explain` prints the rule set a verify run would apply, and `doctor` reports
// what works now and what blocks the next tier. Only doctor executes anything,
// and only the project's own hooks.
package cliux

import (
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// gap separates the key column from the values. The width of the column comes
// from the longest key printed, so a rule set with short keys does not pay for
// the longest one the config can hold.
const gap = 2

// row is one line of output: a config key and what it resolved to. A key with
// several values prints one per line under the same column.
type row struct {
	key    string
	values []string
}

// Explain writes the rules a verify run would apply. With path empty it prints
// the whole effective set; with a path it prints what those rules mean for that
// one file. It reads nothing and runs nothing: the caller has already loaded
// the config from the base ref.
func Explain(w io.Writer, cfg domain.Config, source, path string) error {
	rows := ruleRows(cfg)
	if path != "" {
		rows = pathRows(cfg, path)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "redfirst v%s · explain · config=%s\n\n", domain.Version, source)
	writeRows(&b, rows)

	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("write the rules: %w", err)
	}
	return nil
}

func writeRows(b *strings.Builder, rows []row) {
	width := 0
	for _, r := range rows {
		width = max(width, len(r.key))
	}
	for _, r := range rows {
		for i, v := range r.values {
			key := r.key
			if i > 0 {
				key = ""
			}
			b.WriteString(strings.TrimRight(fmt.Sprintf("%-*s%s", width+gap, key, v), " "))
			b.WriteByte('\n')
		}
	}
}

// ruleRows prints the effective config key by key, under the names the config
// file uses: a reader who disagrees with a value has to know what to edit.
func ruleRows(cfg domain.Config) []row {
	rows := []row{
		{key: "budget.max_files", values: []string{strconv.Itoa(cfg.Budget.MaxFiles)}},
		{key: "budget.max_added_lines", values: []string{strconv.Itoa(cfg.Budget.MaxAddedLines)}},
		{key: "budget.max_deleted_lines", values: []string{strconv.Itoa(cfg.Budget.MaxDeletedLines)}},
		list("paths.protected", cfg.Paths.Protected.Raw(), "every path may change"),
		list("paths.guarded", cfg.Paths.Guarded.Raw(), "no path is watched"),
		list("paths.allowed", cfg.Paths.Allowed.Raw(), "the diff may touch any path"),
	}
	rows = append(rows, testRows(cfg)...)
	rows = append(rows, runRows(cfg)...)
	return append(rows, gateRows(cfg)...)
}

func testRows(cfg domain.Config) []row {
	return []row{
		list("tests.patterns", cfg.Tests.Patterns.Raw(), "nothing counts as a test file"),
		list("tests.fixtures", cfg.Tests.Fixtures.Raw(), "nothing counts as test surface"),
		{key: "tests.immutability", values: []string{string(cfg.Tests.Immutability)}},
		{key: "tests.fixtures_immutability", values: []string{string(cfg.Tests.FixturesImmutability)}},
		{key: "tests.require_new", values: []string{strconv.FormatBool(cfg.Tests.RequireNew)}},
		list("tests.forbid_disabled", cfg.Tests.ForbidDisabled.Raw(), "no marker is forbidden"),
		list("tests.known_flaky", cfg.Tests.KnownFlaky, "no test is exempt from suite-green"),
	}
}

func runRows(cfg domain.Config) []row {
	return []row{
		{key: "red_green.enabled", values: []string{strconv.FormatBool(cfg.RedGreen.Enabled)}},
		{key: "red_green.probe_runs", values: []string{strconv.Itoa(cfg.RedGreen.ProbeRuns)}},
		{
			key:    "red_green.compile_failure_on_base",
			values: []string{string(cfg.RedGreen.CompileFailureOnBase)},
		},
		{key: "suite.retry_failed", values: []string{strconv.Itoa(cfg.Suite.RetryFailed)}},
		{key: "suppression.enabled", values: []string{strconv.FormatBool(cfg.Suppression.Enabled)}},
		list("suppression.patterns", cfg.Suppression.Patterns.Raw(), "nothing is scanned for"),
		{key: "scopes.allow_cross_scope", values: []string{strconv.FormatBool(cfg.Scopes.AllowCrossScope)}},
	}
}

// gateRows says what each gate does to a verdict. A severity a config never
// mentioned still prints: the default is what a reader is arguing with.
func gateRows(cfg domain.Config) []row {
	var disabled []string
	rows := make([]row, 0, len(domain.AllGateIDs())+1)
	for _, id := range domain.AllGateIDs() {
		if cfg.GateDisabled(id) {
			disabled = append(disabled, string(id))
			continue
		}
		rows = append(rows, row{key: "severity." + string(id), values: []string{string(cfg.SeverityOf(id))}})
	}
	return append(rows, list("gates.disabled", disabled, "every gate runs"))
}

// list renders a list of values, naming what an empty one means. "(empty)" on
// its own would leave the reader to guess whether that opens a rule or closes
// it.
func list(key string, values []string, whenEmpty string) row {
	if len(values) == 0 {
		return row{key: key, values: []string{"(empty) · " + whenEmpty}}
	}
	return row{key: key, values: slices.Clone(values)}
}

// pathRows answers what a reader brings to `explain --path`: which rule decided
// this one file, and what that rule costs. Every row names the glob that
// matched, so an argument about a verdict has something to point at.
func pathRows(cfg domain.Config, path string) []row {
	return []row{
		{key: "path", values: []string{path}},
		membership("paths.protected", cfg.Paths.Protected, path, "an edit here blocks the diff"),
		membership("paths.guarded", cfg.Paths.Guarded, path, "an edit here reaches the report, not the verdict"),
		allowedRow(cfg, path),
		membership("tests.patterns", cfg.Tests.Patterns, path,
			"tests.immutability = "+string(cfg.Tests.Immutability)+" applies"),
		membership("tests.fixtures", cfg.Tests.Fixtures, path,
			"tests.fixtures_immutability = "+string(cfg.Tests.FixturesImmutability)+" applies"),
		budgetRow(cfg, path),
		disabledRow(cfg, path),
	}
}

// membership renders one list: the glob that matched and what matching costs.
func membership(key string, set domain.PatternSet, path, cost string) row {
	glob, ok := set.MatchPattern(path)
	if !ok {
		return row{key: key, values: []string{"no"}}
	}
	return row{key: key, values: []string{"yes · " + glob + " · " + cost}}
}

// allowedRow reads the other way round from the rest: an empty list allows
// every path, and a non-empty one blocks whatever it leaves out.
func allowedRow(cfg domain.Config, path string) row {
	const key = "paths.allowed"
	if len(cfg.Paths.Allowed.Raw()) == 0 {
		return row{key: key, values: []string{"(empty) · the diff may touch any path"}}
	}
	if glob, ok := cfg.Paths.Allowed.MatchPattern(path); ok {
		return row{key: key, values: []string{"yes · " + glob}}
	}
	return row{key: key, values: []string{"no · the diff may touch allowed paths only"}}
}

func budgetRow(cfg domain.Config, path string) row {
	const key = "diff-budget"
	if cfg.Tests.Patterns.Match(path) {
		return row{key: key, values: []string{"not counted · a test file never counts against the budget"}}
	}
	return row{key: key, values: []string{"counted · unless .gitattributes calls it linguist-generated"}}
}

// disabledRow covers no-disabled-tests, the one gate that reads the added lines
// of a path rather than its name alone.
func disabledRow(cfg domain.Config, path string) row {
	const key = "no-disabled-tests"
	if !cfg.Tests.Patterns.Match(path) {
		return row{key: key, values: []string{"not scanned · not a test file"}}
	}
	return row{key: key, values: []string{"added lines scanned for " + strings.Join(cfg.Tests.ForbidDisabled.Raw(), ", ")}}
}
