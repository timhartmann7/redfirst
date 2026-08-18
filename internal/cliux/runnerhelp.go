package cliux

// runnerHelp is the command a stack needs to satisfy the hook contract.
//
// Section 4 of the spec puts the reason plainly: people drop off because they
// cannot tell what to fix, not because their stack is unsupported. So every
// finding doctor prints ends in something somebody can paste, and naming the
// fact of a problem is the one thing it may not do.
type runnerHelp struct {
	results string
	filter  string
	rerun   string
}

func helpFor(runner string) runnerHelp {
	const (
		anyResults = "have test.sh write JUnit XML or TAP to $REDFIRST_RESULTS, one entry per case"
		anyFilter  = "pass $REDFIRST_FILTER to the runner, and run everything when it is empty"
		anyRerun   = "the runner has to execute the tests again rather than replay the run before it"
	)
	help := map[string]runnerHelp{
		"vitest": {
			results: `vitest run --reporter=junit --outputFile="$REDFIRST_RESULTS"`,
			filter:  "vitest run $REDFIRST_FILTER",
		},
		"jest": {
			results: `jest --reporters=jest-junit, with JEST_JUNIT_OUTPUT_FILE="$REDFIRST_RESULTS"`,
			filter:  "jest $REDFIRST_FILTER",
		},
		"pytest": {
			results: `pytest --junitxml="$REDFIRST_RESULTS"`,
			filter:  "pytest $REDFIRST_FILTER",
		},
		"go test": {
			results: `gotestsum --junitfile "$REDFIRST_RESULTS" -- ./...`,
			filter:  "gotestsum -- $(for f in $REDFIRST_FILTER; do dirname \"$f\"; done | sort -u)",
			rerun:   "go test -count=1: without it the test cache replays the verdict of the run before",
		},
	}
	h := help[runner]
	if h.results == "" {
		h.results = anyResults
	}
	if h.filter == "" {
		h.filter = anyFilter
	}
	if h.rerun == "" {
		h.rerun = anyRerun
	}
	return h
}
