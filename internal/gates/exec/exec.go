// Package exec holds the gates that run someone else's test harness.
package exec

import (
	"fmt"
	"slices"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// probeOf is the environment the runner brought up.
//
// A gate that declared CapHooks and still reached Run without a probe means the
// runner granted a capability nobody has. That is our bug rather than the
// diff's, and it may not read as an acquittal.
func probeOf(in domain.Input) (domain.Probe, error) {
	if in.Probe == nil {
		return nil, fmt.Errorf("%w: the gate needs hooks and the run brought no environment up",
			domain.ErrInternal)
	}
	return in.Probe, nil
}

// touched reports whether the diff wrote the file a case belongs to.
//
// A case the harness attributed to no file counts as touched. The gate exists
// to separate what the agent wrote from what it inherited, and a case that
// names neither leaves that question open: every ambiguity resolves toward the
// check, never away from it.
func touched(diff domain.Diff, file string) bool {
	if file == "" {
		return true
	}
	return slices.ContainsFunc(diff.Files, func(f domain.FileChange) bool {
		return f.Path == file || f.OldPath == file
	})
}

// exempt reports whether the config took the case out of the gate entirely.
// The list comes from base, so the agent cannot extend it; it matches a case
// by name and by file alike, because a project names whichever it knows.
func exempt(knownFlaky []string, c domain.Case) bool {
	return slices.Contains(knownFlaky, c.Name) ||
		(c.File != "" && slices.Contains(knownFlaky, c.File))
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
