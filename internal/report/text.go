// Package report renders a domain.Report for a human and for the orchestrator.
// Both renderers are pure: they write what the report already holds, compute
// nothing and read nothing.
package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// Column layout of a gate line, taken from the samples in section 9 of the
// spec: the status label owns six columns, the gate id the next twenty-three,
// so every message starts at column 29.
const (
	statusWidth = 6
	idWidth     = 23
	// contIndent lines evidence up under the message column of its gate line.
	contIndent = "      "
	// shaWidth is the abbreviated commit id the header prints.
	shaWidth = 7
)

// Text renders the human report.
func Text(w io.Writer, r domain.Report) error {
	var b strings.Builder

	writeHeader(&b, r)
	writeGates(&b, r.Gates)
	writeWarnings(&b, r.Warnings)
	writeSuppressions(&b, r.Warnings)
	writeVerdict(&b, r)

	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("write text report: %w", err)
	}
	return nil
}

// line drops the padding a short message left behind, so no report line ends
// in whitespace.
func line(b *strings.Builder, s string) {
	b.WriteString(strings.TrimRight(s, " "))
	b.WriteByte('\n')
}

func writeHeader(b *strings.Builder, r domain.Report) {
	head := fmt.Sprintf("redfirst v%s · verify · tier=%d · config=%s",
		r.RedfirstVersion, r.Tier, r.ConfigSource)
	if r.EnvMode != "" {
		head += fmt.Sprintf(" · env_mode=%s · probe_runs=%d", r.EnvMode, r.ProbeRuns)
	}
	line(b, head)

	stats := fmt.Sprintf("base %s  head %s  diff %d files (+%d -%d)",
		short(r.Base), short(r.Head), r.Diff.Files, r.Diff.Added, r.Diff.Deleted)
	if r.Tests != (domain.Stats{}) {
		stats += fmt.Sprintf(", tests %d %s (+%d)",
			r.Tests.Files, plural(r.Tests.Files, "file"), r.Tests.Added)
	}
	line(b, stats)
}

// writeGates opens with the blank line that separates the header from the
// gates. An empty gate list leaves no blank line behind.
func writeGates(b *strings.Builder, gates []domain.GateResult) {
	if len(gates) == 0 {
		return
	}
	b.WriteByte('\n')

	separated := false
	for _, g := range gates {
		// The unavailable block advertises the next tier; a blank line sets it
		// apart from the gates that actually ran.
		if g.Status == domain.StatusUnavailable && !separated {
			b.WriteByte('\n')
			separated = true
		}
		line(b, fmt.Sprintf("%-*s%-*s%s", statusWidth, label(g.Status), idWidth, g.ID, gateMessage(g)))
		// A warning earns its evidence as much as a refusal does. Invariant 9
		// asks the report to say what exactly was flagged and where, and a
		// warning is the one status where the reader decides for themselves:
		// hiding the files leaves them nothing to decide with.
		if g.Status == domain.StatusFail || g.Status == domain.StatusWarn {
			writeEvidence(b, g)
		}
	}
}

// label is the six-column status word. Only unavailable reads differently from
// its domain value, because "UNAVAILABLE" would not fit the column.
func label(s domain.GateStatus) string {
	if s == domain.StatusUnavailable {
		return "N/A"
	}
	return strings.ToUpper(string(s))
}

func gateMessage(g domain.GateResult) string {
	switch g.Status {
	case domain.StatusUnavailable:
		if g.Hint != "" {
			return g.Reason + ", " + g.Hint
		}
		return g.Reason
	case domain.StatusSkip:
		if g.Reason != "" {
			return g.Reason
		}
	}
	return g.Message
}

func writeEvidence(b *strings.Builder, g domain.GateResult) {
	for _, e := range g.Evidence {
		line(b, contIndent+evidenceLine(e))
	}
	if g.Remediation != "" {
		line(b, contIndent+"remediation: "+string(g.Remediation))
	}
}

// evidenceLine names the case when there is one: in a file holding twenty
// cases the path tells a reviewer nothing.
func evidenceLine(e domain.Evidence) string {
	parts := make([]string, 0, 4)
	switch {
	case e.Case != "":
		parts = append(parts, fmt.Sprintf("case %q", e.Case))
	case e.File != "" && e.Line > 0:
		parts = append(parts, fmt.Sprintf("%s:%d", e.File, e.Line))
	case e.File != "":
		parts = append(parts, e.File)
	}
	if len(e.BaseRuns) > 0 {
		parts = append(parts, "base runs: "+strings.Join(e.BaseRuns, ", "))
	}
	if len(e.HeadRuns) > 0 {
		parts = append(parts, "head runs: "+strings.Join(e.HeadRuns, ", "))
	}
	if e.Detail != "" {
		parts = append(parts, e.Detail)
	}
	return strings.Join(parts, "  ")
}

func writeWarnings(b *strings.Builder, ws []domain.Warning) {
	opened := false
	for _, w := range ws {
		if w.Kind == domain.WarnSuppression {
			continue
		}
		if !opened {
			b.WriteByte('\n')
			opened = true
		}
		line(b, fmt.Sprintf("%-*s%-*s%s", statusWidth, "WARN", idWidth, w.Kind, warningDetail(w)))
	}
}

func warningDetail(w domain.Warning) string {
	parts := make([]string, 0, 2)
	if w.File != "" {
		parts = append(parts, w.File)
	}
	if w.Detail != "" {
		parts = append(parts, w.Detail)
	}
	return strings.Join(parts, " ")
}

// writeSuppressions collects the suppression scan into its own block: those
// lines ask a human to look, they do not accuse the diff of anything.
func writeSuppressions(b *strings.Builder, ws []domain.Warning) {
	opened := false
	for _, w := range ws {
		if w.Kind != domain.WarnSuppression {
			continue
		}
		if !opened {
			b.WriteString("\nREVIEW NEEDED\n")
			opened = true
		}
		line(b, fmt.Sprintf("  %s:%d   %s", w.File, w.Line, w.Detail))
	}
}

func writeVerdict(b *strings.Builder, r domain.Report) {
	b.WriteByte('\n')
	if r.Verdict == domain.VerdictPass {
		line(b, fmt.Sprintf("VERDICT: PASS  exit=%d", r.ExitCode))
		return
	}
	n := failedGates(r.Gates)
	line(b, fmt.Sprintf("VERDICT: FAIL (%d %s)  exit=%d", n, plural(n, "gate"), r.ExitCode))
}

func failedGates(gates []domain.GateResult) int {
	n := 0
	for _, g := range gates {
		if g.Status == domain.StatusFail {
			n++
		}
	}
	return n
}

func short(sha string) string {
	if len(sha) > shaWidth {
		return sha[:shaWidth]
	}
	return sha
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
