package harness

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"unicode"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// tapLineLimit caps one line of a TAP stream. A description never comes close.
const tapLineLimit = 1 << 20

// readResults reads what test.sh wrote to $REDFIRST_RESULTS. The content picks
// the format, not the extension.
//
// A missing or empty file is not an error: a harness that reports no per-case
// results costs the run a capability, and the runner turns that into an
// unavailable gate or, where the diff touched an existing test file, into a
// refusal no diff can answer. That is the spec's own answer to a runner without
// per-case names, so content in a third format reads as no cases rather than as
// a failure. A file that opens as xml is different: it claims to be results,
// and half of one means the runner died mid-write, which is a broken harness.
func readResults(path string) ([]domain.Case, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: reading the results of test.sh: %w", domain.ErrHarness, err)
	}
	defer func() { _ = f.Close() }()

	r := bufio.NewReader(f)
	first, err := firstContent(r)
	if err != nil || first == 0 {
		return nil, err
	}
	if first == '<' {
		return parseJUnit(r)
	}
	return parseTAP(r)
}

// byteOrderMark opens a file some runners write on Windows. It is not
// whitespace and it is not content, and leaving it in place would send a JUnit
// file to the TAP reader, which would find nothing in it.
var byteOrderMark = []byte{0xEF, 0xBB, 0xBF}

// firstContent is the first byte that is not whitespace, or zero at the end of
// an empty file.
func firstContent(r *bufio.Reader) (byte, error) {
	if mark, err := r.Peek(len(byteOrderMark)); err == nil && bytes.Equal(mark, byteOrderMark) {
		if _, err := r.Discard(len(byteOrderMark)); err != nil {
			return 0, fmt.Errorf("%w: reading the results of test.sh: %w", domain.ErrHarness, err)
		}
	}
	for {
		b, err := r.ReadByte()
		if errors.Is(err, io.EOF) {
			return 0, nil
		}
		if err != nil {
			return 0, fmt.Errorf("%w: reading the results of test.sh: %w", domain.ErrHarness, err)
		}
		if !unicode.IsSpace(rune(b)) {
			return b, r.UnreadByte()
		}
	}
}

// junitCase is one <testcase>. A failure, an error or a skip arrives as a child
// element, and only its presence matters here.
type junitCase struct {
	Classname string    `xml:"classname,attr"`
	Name      string    `xml:"name,attr"`
	Failure   *struct{} `xml:"failure"`
	Error     *struct{} `xml:"error"`
	Skipped   *struct{} `xml:"skipped"`
}

// parseJUnit walks the tokens rather than decoding a root element, so a file
// rooted at <testsuites> and one rooted at a bare <testsuite> read the same.
func parseJUnit(r io.Reader) ([]domain.Case, error) {
	dec := xml.NewDecoder(r)

	var cases []domain.Case
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return cases, nil
		}
		if err != nil {
			return nil, fmt.Errorf("%w: test.sh wrote results that are not valid xml: %w",
				domain.ErrHarness, err)
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "testcase" {
			continue
		}

		var c junitCase
		if err := dec.DecodeElement(&c, &start); err != nil {
			return nil, fmt.Errorf("%w: test.sh wrote a testcase we cannot read: %w",
				domain.ErrHarness, err)
		}
		cases = append(cases, domain.Case{File: c.Classname, Name: c.Name, Outcome: c.outcome()})
	}
}

func (c junitCase) outcome() domain.CaseOutcome {
	switch {
	case c.Failure != nil || c.Error != nil:
		return domain.CaseFail
	case c.Skipped != nil:
		return domain.CaseSkip
	}
	return domain.CasePass
}

// parseTAP reads the `ok 1 - description` grammar. Only lines that open at the
// left margin count: an indented block is a subtest, and its cases already
// arrive again as the result of the test that owns them.
func parseTAP(r io.Reader) ([]domain.Case, error) {
	s := bufio.NewScanner(r)
	s.Buffer(nil, tapLineLimit)

	var cases []domain.Case
	for s.Scan() {
		if c, ok := parseTAPLine(s.Text()); ok {
			cases = append(cases, c)
		}
	}
	if err := s.Err(); err != nil {
		return nil, fmt.Errorf("%w: reading the results of test.sh: %w", domain.ErrHarness, err)
	}
	return cases, nil
}

func parseTAPLine(line string) (domain.Case, bool) {
	rest, outcome := "", domain.CasePass
	switch {
	case strings.HasPrefix(line, "not ok"):
		rest, outcome = line[len("not ok"):], domain.CaseFail
	case strings.HasPrefix(line, "ok"):
		rest = line[len("ok"):]
	default:
		return domain.Case{}, false
	}
	// The word has to end there: "okay, moving on" is somebody's comment.
	if rest != "" && !unicode.IsSpace(rune(rest[0])) {
		return domain.Case{}, false
	}

	name, directive := splitTAPDirective(strings.TrimSpace(rest))
	if strings.EqualFold(directive, "skip") {
		outcome = domain.CaseSkip
	}
	return domain.Case{Name: tapName(name), Outcome: outcome}, true
}

// splitTAPDirective cuts the trailing `# SKIP why` off a description and
// returns the directive word on its own.
func splitTAPDirective(rest string) (name, directive string) {
	hash := strings.Index(rest, "#")
	if hash < 0 {
		return rest, ""
	}
	directive = strings.TrimSpace(rest[hash+1:])
	if space := strings.IndexFunc(directive, unicode.IsSpace); space >= 0 {
		directive = directive[:space]
	}
	return strings.TrimSpace(rest[:hash]), directive
}

// tapName drops the test number and the dash the grammar puts before the
// description, which no runner counts as part of the name.
func tapName(rest string) string {
	if space := strings.IndexFunc(rest, unicode.IsSpace); space >= 0 {
		if _, err := strconv.Atoi(rest[:space]); err == nil {
			rest = strings.TrimSpace(rest[space:])
		}
	} else if _, err := strconv.Atoi(rest); err == nil {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(rest, "-"))
}
