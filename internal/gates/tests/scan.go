package tests

import (
	"strings"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// excerptRunes caps one line in the report. A minified bundle on a single line
// would otherwise push every other finding off the screen.
const excerptRunes = 80

// hit is one added line a pattern set matched.
type hit struct {
	file    string
	line    int
	text    string
	pattern string
}

// scanAdded walks the added lines of every file include accepts and returns
// what set matched, in diff order. Two gates read added lines, and this is
// where they agree on what one is: only what the diff wrote, never what it left
// alone, and never a line the diff removed.
func scanAdded(d domain.Diff, include func(path string) bool, set domain.RegexSet) []hit {
	var hits []hit
	for _, f := range d.Files {
		if !include(f.Path) {
			continue
		}
		for _, l := range f.AddedLines {
			if pattern, ok := set.Match(l.Text); ok {
				hits = append(hits, hit{file: f.Path, line: l.Number, text: l.Text, pattern: pattern})
			}
		}
	}
	return hits
}

// holdsTestCases reports whether a path is a test file rather than part of the
// surface around one.
//
// The two lists overlap by construction: a jest snapshot is named after the
// file it belongs to, so `total.test.js.snap` falls under the default pattern
// `**/*.test.*` as surely as the test does. tests.fixtures is defined as the
// files that decide outcomes without holding test cases, so it settles the
// overlap here exactly as it does in test-immutability.
func holdsTestCases(cfg domain.Config, path string) bool {
	return cfg.Tests.Patterns.Match(path) && !cfg.Tests.Fixtures.Match(path)
}

// excerpt is the line as the report prints it: trimmed of the indentation the
// column layout supplies anyway, and cut where it stops being readable.
func excerpt(s string) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= excerptRunes {
		return s
	}
	return string(runes[:excerptRunes]) + "…"
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
