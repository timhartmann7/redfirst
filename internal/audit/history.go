package audit

import (
	"context"
	"fmt"
	"regexp"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/gitx"
)

// Mode is what counts as one unit of history. Reconstructing merged pull
// requests without a forge API depends on how the repository merges, and v1
// makes no such call: it would break "zero network at runtime" and demand a
// token at the step we sell as one command with nothing installed.
type Mode string

const (
	// ModeAuto asks for detection. It is a request, never a result.
	ModeAuto Mode = "auto"
	// ModeMerge takes the merge commit, which is the exact pull request.
	ModeMerge Mode = "merge"
	// ModeSquash takes a first-parent commit and reads a number off its subject.
	ModeSquash Mode = "squash"
	// ModeCommit takes a first-parent commit and has no number to read.
	ModeCommit Mode = "commit"
)

// sampleSize is how many first-parent commits the detection looks at.
const sampleSize = 50

// prNumber is the trace a squash merge leaves behind. Compiled once, as a
// constant of this package: it runs over every subject of the sample.
var prNumber = regexp.MustCompile(`\(#(\d+)\)`)

// ParseMode reads the --unit flag.
func ParseMode(s string) (Mode, error) {
	switch m := Mode(s); m {
	case ModeAuto, ModeMerge, ModeSquash, ModeCommit:
		return m, nil
	}
	return "", fmt.Errorf("unknown unit mode %q, want auto, merge, squash or commit", s)
}

// Noun names the unit in the header, singular: the count decides the plural.
func (m Mode) Noun() string {
	switch m {
	case ModeMerge:
		return "merged PR"
	case ModeSquash:
		return "squashed commit"
	}
	return "commit"
}

// Caveat is what the mode costs in accuracy. Mode merge gives exact pull
// request boundaries and the other two do not: a pull request that landed as
// three commits counts three times, and the diff-budget statistics come out
// understated. The header says so rather than keeping quiet.
func (m Mode) Caveat() string {
	if m == ModeMerge {
		return "a merge commit is one pull request"
	}
	return "a PR merged as several commits counts several times"
}

func resolveMode(ctx context.Context, repo *gitx.Repo, opts Options) (Mode, error) {
	switch opts.Unit {
	case ModeAuto:
		return detect(ctx, repo, opts.Base)
	case ModeMerge, ModeSquash, ModeCommit:
		return opts.Unit, nil
	}
	return "", fmt.Errorf("%w: unknown unit mode %q", domain.ErrInternal, opts.Unit)
}

// detect picks the mode from the shape of the recent history. You cannot choose
// the mode you get: someone else's repository settings decide it.
func detect(ctx context.Context, repo *gitx.Repo, base string) (Mode, error) {
	sample, err := repo.FirstParent(ctx, base, gitx.Walk{Limit: sampleSize})
	if err != nil {
		return "", err
	}

	merges, numbered := 0, 0
	for _, c := range sample {
		if c.Merged() {
			merges++
		}
		if prNumber.MatchString(c.Subject) {
			numbered++
		}
	}
	switch {
	case merges*2 > len(sample):
		return ModeMerge, nil
	case numbered*2 > len(sample):
		return ModeSquash, nil
	}
	return ModeCommit, nil
}

// walk collects the units to audit, newest first.
func walk(ctx context.Context, repo *gitx.Repo, mode Mode, opts Options) ([]gitx.Commit, error) {
	w := gitx.Walk{Limit: opts.Last, Since: opts.Since, Merges: mode == ModeMerge}
	// --since is the alternative to --last, not a filter on top of it.
	if opts.Since != "" {
		w.Limit = 0
	}

	commits, err := repo.FirstParent(ctx, opts.Base, w)
	if err != nil {
		return nil, err
	}
	return withoutRoot(commits), nil
}

// withoutRoot drops the commit that started the repository. It is the whole
// tree arriving at once rather than a unit of merged history, and counting it
// would put every file of the project into one unit's diff. A first-parent walk
// reaches the root last, so everything before it stands.
func withoutRoot(commits []gitx.Commit) []gitx.Commit {
	for i, c := range commits {
		if len(c.Parents) == 0 {
			return commits[:i]
		}
	}
	return commits
}

// unitDiff is the change one unit brought onto base.
//
// A merge is measured from the point its branch forked, never two-dot from the
// tip: commits that landed on base while the branch was open would otherwise
// read as reverts the branch wrote. The other two modes measure a commit
// against its first parent, which is the same thing for a linear history.
func unitDiff(ctx context.Context, repo *gitx.Repo, mode Mode, c gitx.Commit) (domain.Diff, error) {
	if mode == ModeMerge {
		return repo.Diff(ctx, c.Parents[0], c.Parents[1])
	}
	return repo.Diff(ctx, c.SHA+"^", c.SHA)
}

// pullRequestNumber is the number a squashed subject carries. Squash and commit
// differ in this alone; their accuracy is identical.
func pullRequestNumber(mode Mode, subject string) string {
	if mode != ModeSquash {
		return ""
	}
	if m := prNumber.FindStringSubmatch(subject); m != nil {
		return "#" + m[1]
	}
	return ""
}

// span is how many days the units cover, oldest to newest. Taken from the
// commits rather than from the clock: two runs over one repository print the
// same header.
func span(units []gitx.Commit) int {
	if len(units) < 2 {
		return 0
	}
	newest, oldest := units[0].Date, units[len(units)-1].Date
	return int(newest.Sub(oldest).Hours() / 24)
}
