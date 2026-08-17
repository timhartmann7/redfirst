package paths

import (
	"context"
	"fmt"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// DiffBudget keeps the number of changed files and lines inside [budget].
//
// Two kinds of file stay out of the count. Files under tests.patterns, because
// new-test-present demands them and charging the agent for satisfying a
// neighbouring gate would leave it no legal diff. Files .gitattributes marks
// linguist-generated at the merge base, because nobody wrote them by hand and
// one regenerated lockfile would eat the whole budget.
type DiffBudget struct{}

// NewDiffBudget builds the gate. The config arrives at construction rather than
// at Run: Requires() has to stay fixed for a whole run.
func NewDiffBudget(domain.Config) *DiffBudget { return &DiffBudget{} }

// ID names the gate in the config, the report and the JSON.
func (g *DiffBudget) ID() domain.GateID { return domain.GateDiffBudget }

// Requires declares what the gate needs before the runner lets it speak.
func (g *DiffBudget) Requires() []domain.Capability {
	return []domain.Capability{domain.CapDiff}
}

// Run counts the diff and compares it against the three limits. The counts
// come from gitx, which took them from the merge base: a two-dot diff would
// charge a long-lived branch for commits that landed on base meanwhile.
func (g *DiffBudget) Run(_ context.Context, in domain.Input) (domain.GateResult, error) {
	budget := in.Config.Budget
	counted := countable(in.Diff, in.Config.Tests.Patterns)

	res := domain.GateResult{
		Status:   domain.StatusPass,
		Message:  usage(counted, budget),
		Evidence: overruns(counted, budget),
	}
	if len(res.Evidence) > 0 {
		res.Status = domain.StatusFail
		res.Remediation = domain.RemediationReduceScope
	}
	return res, nil
}

// countable sums what the budget judges. A binary file counts as a file and
// contributes no lines: git reports no counts for one, and inventing them
// would put the verdict at the mercy of a byte size.
func countable(d domain.Diff, tests domain.PatternSet) domain.Stats {
	var s domain.Stats
	for _, f := range d.Files {
		if f.Generated || tests.Match(f.Path) {
			continue
		}
		s.Files++
		s.Added += f.Added
		s.Deleted += f.Deleted
	}
	return s
}

// usage is the line from section 9 of the spec, printed whatever the verdict:
// a diff at 19 of 20 files is worth seeing before it becomes 21.
func usage(s domain.Stats, b domain.Budget) string {
	return fmt.Sprintf("%d/%d files, +%d/%d, -%d/%d",
		s.Files, b.MaxFiles, s.Added, b.MaxAddedLines, s.Deleted, b.MaxDeletedLines)
}

// overruns names every limit the diff passed. All three at once rather than
// the first: whoever splits the PR should learn the whole size of the problem
// in one run.
func overruns(s domain.Stats, b domain.Budget) []domain.Evidence {
	limits := []struct {
		got   int
		limit int
		unit  string
	}{
		{s.Files, b.MaxFiles, "files"},
		{s.Added, b.MaxAddedLines, "added lines"},
		{s.Deleted, b.MaxDeletedLines, "deleted lines"},
	}

	var ev []domain.Evidence
	for _, l := range limits {
		if l.got > l.limit {
			ev = append(ev, domain.Evidence{
				Detail: fmt.Sprintf("%d %s, the budget allows %d", l.got, l.unit, l.limit),
			})
		}
	}
	return ev
}
