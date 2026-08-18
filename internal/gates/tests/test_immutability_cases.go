package tests

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// casesOffences judges outcomes instead of lines.
//
// The set of case names the base ref reported for the diff's test files has to
// sit entirely inside the set of names on head, and every case that carried
// over has to be green there. Renaming a symbol passes, because a rename moves
// no case name; deleting a test or hollowing it out into a stub does not.
//
// The mode exists because strict and append-only forbid legitimate refactoring:
// renaming a function that tests cover needs edits to test files, and neither
// line-based mode leaves the agent a legal diff for it.
func casesOffences(ctx context.Context, in domain.Input) ([]offence, error) {
	if in.Probe == nil {
		return nil, fmt.Errorf("%w: %q judges outcomes and the run brought no environment up",
			domain.ErrInternal, domain.ImmutabilityCases)
	}
	before, err := in.Probe.Inventory(ctx)
	if err != nil {
		return nil, err
	}
	// The same head run red-green reads. The two gates share it: a run of the
	// project's suite costs minutes, and neither may pay for it twice.
	head, err := in.Probe.Head(ctx)
	if err != nil {
		return nil, err
	}

	survived := head.Names()
	var offences []offence
	for _, name := range before.Names() {
		if !slices.Contains(survived, name) {
			offences = append(offences, caseOffence(before.File(name), name,
				kindRemoved, "the case is gone on head"))
			continue
		}
		if !head.Outcomes(name).All(domain.CasePass) {
			offences = append(offences, caseOffence(head.File(name), name,
				kindRegressed, "the case is not green on head: "+
					joinOutcomes(head.Outcomes(name))))
		}
	}
	return offences, nil
}

func caseOffence(file, name string, kind offenceKind, detail string) offence {
	return offence{
		file: file, kase: name, kind: kind,
		mode: domain.ImmutabilityCases, detail: detail,
	}
}

// joinOutcomes prints what every head run said, so a reviewer sees a flake as a
// flake rather than as a plain refusal.
func joinOutcomes(outcomes domain.Outcomes) string {
	return "head runs: " + strings.Join(outcomes.Strings(), ", ")
}
