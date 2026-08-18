package audit

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// Format is how the summary reaches the reader.
type Format string

const (
	FormatText Format = "text"
	FormatJSON Format = "json"
	FormatCSV  Format = "csv"
)

// subjectRunes caps the subject column. A commit message written as a paragraph
// would otherwise push the findings off the screen.
const subjectRunes = 48

// ParseFormat reads the --report flag.
func ParseFormat(s string) (Format, error) {
	switch f := Format(s); f {
	case FormatText, FormatJSON, FormatCSV:
		return f, nil
	}
	return "", fmt.Errorf("unknown report format %q, want text, json or csv", s)
}

// Render writes the summary. perUnit adds the breakdown --per-pr asks for;
// without it only the counts reach the reader.
func Render(w io.Writer, s Summary, f Format, perUnit bool) error {
	switch f {
	case FormatText:
		return renderText(w, s, perUnit)
	case FormatJSON:
		return renderJSON(w, s, perUnit)
	case FormatCSV:
		return renderCSV(w, s, perUnit)
	}
	return fmt.Errorf("%w: unknown report format %q", domain.ErrInternal, f)
}

func renderText(w io.Writer, s Summary, perUnit bool) error {
	var b strings.Builder

	line(&b, fmt.Sprintf("Audited %d %s on %s (last %d %s)",
		s.Units, plural(s.Units, s.Unit.noun()), s.Base, s.Days, plural(s.Days, "day")))
	line(&b, fmt.Sprintf("  unit=%s · %s", s.Unit, s.Caveat))
	for _, n := range s.Notes {
		line(&b, "  note: "+n)
	}

	writeCounts(&b, s.Findings)
	if perUnit {
		writePerUnit(&b, s.PerUnit)
	}

	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("write audit report: %w", err)
	}
	return nil
}

// writeCounts prints how many units every category fired on, right-aligned so the
// numbers read as a column.
func writeCounts(b *strings.Builder, findings []Finding) {
	b.WriteByte('\n')
	if len(findings) == 0 {
		line(b, "  no findings")
		return
	}

	width := 1
	for _, f := range findings {
		width = max(width, len(strconv.Itoa(f.Units)))
	}
	for _, f := range findings {
		line(b, fmt.Sprintf("  %*d  %s", width, f.Units, f.Category))
	}
}

// writePerUnit prints one line per unit, its columns aligned across the block
// so the findings read down the page.
func writePerUnit(b *strings.Builder, units []Unit) {
	stats := make([]string, len(units))
	found := make([]string, len(units))
	statsWidth, foundWidth := 0, 0
	for i, u := range units {
		stats[i] = fmt.Sprintf("%d %s (+%d -%d)",
			u.Diff.Files, plural(u.Diff.Files, "file"), u.Diff.Added, u.Diff.Deleted)
		found[i] = strings.Join(u.Findings, ", ")
		statsWidth = max(statsWidth, len(stats[i]))
		foundWidth = max(foundWidth, len(found[i]))
	}

	b.WriteString("\nPER UNIT\n")
	for i, u := range units {
		line(b, fmt.Sprintf("  %s  %s  %-*s  %-*s  %s",
			short(u.Commit), u.Date, statsWidth, stats[i], foundWidth, found[i], truncate(u.Subject)))
	}
}

func renderJSON(w io.Writer, s Summary, perUnit bool) error {
	if !perUnit {
		s.PerUnit = nil
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("%w: marshal audit summary: %w", domain.ErrInternal, err)
	}
	if _, err := w.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("write audit report: %w", err)
	}
	return nil
}

// renderCSV prints the counts, or one row per unit under --per-pr. The second
// shape is what sets a budget: the distribution of your own history, with the
// upper quartile as the number to put in the config.
func renderCSV(w io.Writer, s Summary, perUnit bool) error {
	rows := countRows(s.Findings)
	if perUnit {
		rows = unitRows(s.PerUnit)
	}

	cw := csv.NewWriter(w)
	if err := cw.WriteAll(rows); err != nil {
		return fmt.Errorf("write audit report: %w", err)
	}
	return nil
}

func countRows(findings []Finding) [][]string {
	rows := make([][]string, 0, len(findings)+1)
	rows = append(rows, []string{"category", "units"})
	for _, f := range findings {
		rows = append(rows, []string{f.Category, strconv.Itoa(f.Units)})
	}
	return rows
}

func unitRows(units []Unit) [][]string {
	rows := make([][]string, 0, len(units)+1)
	rows = append(rows, []string{
		"commit", "pr", "date", "files", "added", "deleted",
		"test_files", "test_added", "findings", "subject",
	})
	for _, u := range units {
		rows = append(rows, []string{
			u.Commit, u.PR, u.Date,
			strconv.Itoa(u.Diff.Files), strconv.Itoa(u.Diff.Added), strconv.Itoa(u.Diff.Deleted),
			strconv.Itoa(u.Tests.Files), strconv.Itoa(u.Tests.Added),
			strings.Join(u.Findings, " "), u.Subject,
		})
	}
	return rows
}

// truncate cuts the subject where it stops being a label. The pull request
// number needs no column of its own here: the mode that supplies one read it
// off this very subject.
func truncate(s string) string {
	runes := []rune(s)
	if len(runes) <= subjectRunes {
		return s
	}
	return string(runes[:subjectRunes]) + "…"
}

// line drops the padding a short column left behind, so no report line ends in
// whitespace.
func line(b *strings.Builder, s string) {
	b.WriteString(strings.TrimRight(s, " "))
	b.WriteByte('\n')
}

func short(sha string) string {
	const shaWidth = 7
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
