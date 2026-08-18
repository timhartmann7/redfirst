package gitx

import (
	"bufio"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// Commit is one entry of a first-parent walk: what `redfirst audit` needs to
// tell a unit of history from a commit inside one.
type Commit struct {
	SHA string
	// Parents are the parents in git's own order, so Parents[0] is the branch
	// the merge landed on and Parents[1] is the branch that was merged.
	Parents []string
	Subject string
	// Date is the committer date, which is when the unit reached base. The
	// author date can be weeks older and says nothing about the merge order.
	Date time.Time
}

// Merged reports whether the commit joined two histories.
func (c Commit) Merged() bool { return len(c.Parents) > 1 }

// Walk narrows a history walk. A zero Walk asks for everything.
type Walk struct {
	// Limit caps how many commits come back. Zero means no cap.
	Limit int
	// Since is a git date expression; an empty one reaches the root commit.
	Since string
	// Merges keeps the commits that joined two histories and drops the rest.
	Merges bool
}

// logFormat is one commit as four fields. Under -z git terminates every field
// and every commit with a NUL, so the whole stream reads as one flat record
// sequence, and a subject holding a newline or a quote still arrives whole.
const logFormat = "%H%x00%P%x00%cI%x00%s"

// FirstParent walks the history of ref along first parents, newest first.
//
// First parent and nothing else: the second parent of a merge holds the commits
// of the branch that was merged, and counting those as units of history would
// count one pull request as many.
func (r *Repo) FirstParent(ctx context.Context, ref string, w Walk) ([]Commit, error) {
	var commits []Commit
	err := r.runStream(ctx, func(s *bufio.Scanner) error {
		var perr error
		commits, perr = parseCommits(s)
		return perr
	}, walkArgs(ref, w)...)
	if err != nil {
		return nil, fmt.Errorf("cannot walk the history of %q: %w", ref, err)
	}
	return commits, nil
}

func walkArgs(ref string, w Walk) []string {
	args := []string{"log", "-z", "--first-parent", "--format=" + logFormat}
	if w.Merges {
		args = append(args, "--merges")
	}
	if w.Limit > 0 {
		args = append(args, "--max-count="+strconv.Itoa(w.Limit))
	}
	if w.Since != "" {
		args = append(args, "--since="+w.Since)
	}
	// The trailing -- ends the revisions. --end-of-options stops flag parsing
	// and settles nothing about the other ambiguity: a repository holding both
	// a branch named release and a directory named release/ would otherwise
	// answer an audit with "ambiguous argument".
	return append(args, "--end-of-options", ref, "--")
}

func parseCommits(s *bufio.Scanner) ([]Commit, error) {
	var commits []Commit
	for s.Scan() {
		c := Commit{SHA: s.Text()}
		parents, err := next(s, "the parents of commit "+c.SHA)
		if err != nil {
			return nil, err
		}
		// The root commit has none, and its field arrives empty.
		c.Parents = strings.Fields(parents)

		date, err := next(s, "the committer date of commit "+c.SHA)
		if err != nil {
			return nil, err
		}
		if c.Date, err = time.Parse(time.RFC3339, date); err != nil {
			return nil, fmt.Errorf("%w: committer date %q of commit %s: %w",
				domain.ErrInternal, date, c.SHA, err)
		}
		if c.Subject, err = next(s, "the subject of commit "+c.SHA); err != nil {
			return nil, err
		}
		commits = append(commits, c)
	}
	return commits, nil
}
