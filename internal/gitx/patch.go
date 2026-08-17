package gitx

import (
	"bufio"
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/timhartmann7/redfirst/internal/domain"
)

const (
	fileHeader = "diff --git "
	hunkHeader = "@@ "
	// noNewline is the marker git prints after a line that ends without one.
	// It sits inside a hunk and counts for neither side.
	noNewline = `\`
)

// readAddedLines fills the added lines of every file in the diff.
//
// Two gates judge what the diff wrote rather than how much of it there was, and
// section 12 of the spec keeps them pure functions of (Diff, Config): the lines
// have to arrive with the diff instead of being fetched from a gate.
func (r *Repo) readAddedLines(ctx context.Context, from, to string, files []domain.FileChange) error {
	if len(files) == 0 {
		return nil
	}
	p := &patch{files: files, current: -1}
	if err := r.runLines(ctx, p.parse, patchArgs(from, to)...); err != nil {
		return err
	}
	return p.check()
}

// patchArgs asks for the same file set as diffArgs, with the content of every
// hunk and no context around it.
//
// --no-ext-diff and --no-textconv keep a diff driver from replacing what we
// read: a gate judges the line the agent wrote, not what a filter made of it.
// --no-color does the same for `color.diff = always`, which would otherwise
// wrap every line in escape sequences a regex has no idea about.
func patchArgs(from, to string) []string {
	return []string{
		"--attr-source=" + from,
		"diff", "--unified=0", "--no-color", "--no-ext-diff", "--no-textconv",
		"--find-renames", "--find-copies", from, to,
	}
}

// patch reads one `git diff --unified=0` stream onto the files --name-status
// already produced.
type patch struct {
	files []domain.FileChange
	// current is the file whose hunks are being read, -1 before the first
	// header. Git emits the same files in the same order for every output
	// format, so position pairs the two runs; the alternative is unquoting the
	// path out of the header, which means reimplementing git's own C-style
	// quoting for the sake of a name we already hold.
	current int
}

func (p *patch) parse(s *bufio.Scanner) error {
	for s.Scan() {
		line := s.Text()
		switch {
		case strings.HasPrefix(line, fileHeader):
			p.current++
			if p.current >= len(p.files) {
				return fmt.Errorf("%w: git diff prints more files than --name-status did", domain.ErrInternal)
			}
		case strings.HasPrefix(line, hunkHeader):
			if err := p.readHunk(s, line); err != nil {
				return err
			}
		}
	}
	return nil
}

// readHunk consumes exactly as many lines as the header counts.
//
// Counting rather than reading until the next header is what keeps content out
// of the parse: a diff that adds the line `diff --git a/x b/y` to a file is a
// perfectly ordinary diff, and a parser looking for headers would take that
// added line for the start of another file.
func (p *patch) readHunk(s *bufio.Scanner, header string) error {
	if p.current < 0 {
		return fmt.Errorf("%w: git diff opens with a hunk before any file header", domain.ErrInternal)
	}
	h, err := parseHunkHeader(header)
	if err != nil {
		return err
	}

	f := &p.files[p.current]
	added := 0
	for remaining := h.deleted + h.added; remaining > 0; {
		line, err := next(s, "a line of hunk "+header)
		if err != nil {
			return err
		}
		switch {
		case strings.HasPrefix(line, noNewline):
			continue
		case strings.HasPrefix(line, "+"):
			f.AddedLines = append(f.AddedLines, domain.AddedLine{Number: h.start + added, Text: line[1:]})
			added++
		case strings.HasPrefix(line, "-"):
		default:
			return fmt.Errorf("%w: hunk %q holds a line starting with neither plus nor minus: %q",
				domain.ErrInternal, header, line)
		}
		remaining--
	}
	return nil
}

// hunk is what a hunk header states: where the added lines land on head and how
// many lines of each side follow.
type hunk struct {
	start   int
	added   int
	deleted int
}

// parseHunkHeader reads `@@ -12,3 +14,5 @@`, where either count may be missing
// and means one line when it is.
func parseHunkHeader(line string) (hunk, error) {
	fields := strings.Fields(line)
	if len(fields) < 4 || fields[0] != "@@" || fields[3] != "@@" {
		return hunk{}, fmt.Errorf("%w: unexpected git hunk header %q", domain.ErrInternal, line)
	}
	_, deleted, err := parseRange(fields[1], '-')
	if err != nil {
		return hunk{}, err
	}
	start, added, err := parseRange(fields[2], '+')
	if err != nil {
		return hunk{}, err
	}
	return hunk{start: start, added: added, deleted: deleted}, nil
}

func parseRange(field string, sign byte) (start, count int, err error) {
	if len(field) < 2 || field[0] != sign {
		return 0, 0, fmt.Errorf("%w: hunk range %q does not open with %q", domain.ErrInternal, field, sign)
	}
	body, count := field[1:], 1
	if comma := strings.IndexByte(body, ','); comma >= 0 {
		if count, err = strconv.Atoi(body[comma+1:]); err != nil {
			return 0, 0, fmt.Errorf("%w: hunk range %q: %w", domain.ErrInternal, field, err)
		}
		body = body[:comma]
	}
	if start, err = strconv.Atoi(body); err != nil {
		return 0, 0, fmt.Errorf("%w: hunk range %q: %w", domain.ErrInternal, field, err)
	}
	return start, count, nil
}

// check pairs the patch against the counts --numstat already gave. The two
// commands run separately and line up by position, so a slip would hand a gate
// another file's lines and a refusal would name the wrong path.
func (p *patch) check() error {
	if p.current != len(p.files)-1 {
		return fmt.Errorf("%w: git diff prints %d files, --name-status printed %d",
			domain.ErrInternal, p.current+1, len(p.files))
	}
	for _, f := range p.files {
		if len(f.AddedLines) != f.Added {
			return fmt.Errorf("%w: git diff prints %d added lines for %q, --numstat counted %d",
				domain.ErrInternal, len(f.AddedLines), f.Path, f.Added)
		}
	}
	return nil
}
