// Package paths holds the gates that judge which files a diff touched.
package paths

import (
	"context"
	"fmt"
	"strings"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// ProtectedPaths refuses a diff that touches paths.protected, and, when
// paths.allowed is not empty, one that steps outside it. Files under
// paths.guarded reach the report and leave the verdict where it was.
type ProtectedPaths struct{}

// NewProtectedPaths builds the gate. The config arrives at construction rather
// than at Run: Requires() has to stay fixed for a whole run.
func NewProtectedPaths(domain.Config) *ProtectedPaths { return &ProtectedPaths{} }

// ID names the gate in the config, the report and the JSON.
func (g *ProtectedPaths) ID() domain.GateID { return domain.GateProtectedPaths }

// Requires declares what the gate needs before the runner lets it speak.
func (g *ProtectedPaths) Requires() []domain.Capability {
	return []domain.Capability{domain.CapDiff}
}

// rule is the list a path broke. It reaches the report, so a refusal that
// mixes the two says which one each file broke.
type rule string

const (
	ruleProtected rule = "under paths.protected"
	ruleOutside   rule = "outside paths.allowed"
)

// Run judges every path the diff touched. A rename touches two of them and
// both get judged: moving a protected file out of its path changes the rules
// for future agents exactly as much as editing it in place does.
func (g *ProtectedPaths) Run(_ context.Context, in domain.Input) (domain.GateResult, error) {
	p := in.Config.Paths
	// paths.allowed is off unless someone filled it, and an empty list would
	// otherwise refuse every file in the diff.
	restricted := len(p.Allowed.Raw()) > 0

	var (
		res       domain.GateResult
		protected int
		outside   int
	)
	for _, f := range in.Diff.Files {
		broken, path, ok := offence(f, p, restricted)
		if !ok {
			res.Warnings = appendGuarded(res.Warnings, f, p)
			continue
		}
		if broken == ruleProtected {
			protected++
		} else {
			outside++
		}
		res.Evidence = append(res.Evidence, domain.Evidence{
			File:   path,
			Detail: describe(f, path) + ", " + string(broken),
		})
	}

	if protected+outside == 0 {
		res.Status = domain.StatusPass
		return res, nil
	}
	res.Status = domain.StatusFail
	res.Message = message(protected, outside)
	res.Remediation = domain.RemediationRevertPaths
	return res, nil
}

// offence returns the first rule a file broke and the path that broke it.
// paths.protected outranks paths.allowed, so a path caught by both keeps
// naming the stronger rule whatever the diff looks like.
func offence(f domain.FileChange, p domain.Paths, restricted bool) (rule, string, bool) {
	if path, ok := matched(p.Protected, f); ok {
		return ruleProtected, path, true
	}
	if !restricted {
		return "", "", false
	}
	// Both ends of a rename have to sit inside the list: a file moved out of
	// it left the allowed area just as surely as one moved into it entered.
	if !p.Allowed.Match(f.Path) {
		return ruleOutside, f.Path, true
	}
	if f.OldPath != "" && !p.Allowed.Match(f.OldPath) {
		return ruleOutside, f.OldPath, true
	}
	return "", "", false
}

// appendGuarded adds the reviewer's line for a file under paths.guarded. Only
// files that broke no rule reach here: a path the gate already refused earns
// no second line asking a human to look at it.
func appendGuarded(ws []domain.Warning, f domain.FileChange, p domain.Paths) []domain.Warning {
	path, ok := matched(p.Guarded, f)
	if !ok {
		return ws
	}
	return append(ws, domain.Warning{
		Kind: domain.WarnGuardedPaths,
		File: path,
		// Only a rename or a copy earns more than the path: which of its two
		// ends the list caught is invisible from one file name.
		Detail: otherEnd(f, path),
	})
}

// matched reports the path of the file that falls in the set, the head path
// first so a rename inside the set names where the file lives now.
func matched(set domain.PatternSet, f domain.FileChange) (string, bool) {
	if set.Match(f.Path) {
		return f.Path, true
	}
	if f.OldPath != "" && set.Match(f.OldPath) {
		return f.OldPath, true
	}
	return "", false
}

// otherEnd describes a rename or a copy and stays silent about anything else.
func otherEnd(f domain.FileChange, path string) string {
	if !f.Status.HasOldPath() {
		return ""
	}
	return describe(f, path)
}

// describe says what the file did to this path. A rename names its other end:
// "renamed to" and "renamed from" tell a reviewer which side was caught.
func describe(f domain.FileChange, path string) string {
	switch f.Status {
	case domain.FileRenamed, domain.FileCopied:
		verb := "renamed"
		if f.Status == domain.FileCopied {
			verb = "copied"
		}
		if path == f.OldPath {
			return verb + " to " + f.Path
		}
		return verb + " from " + f.OldPath
	case domain.FileAdded:
		return "added"
	case domain.FileDeleted:
		return "deleted"
	case domain.FileModified:
		return "modified"
	}
	return "status " + string(f.Status)
}

func message(protected, outside int) string {
	parts := make([]string, 0, 2)
	if protected > 0 {
		parts = append(parts, fmt.Sprintf("%d %s %s", protected, files(protected), ruleProtected))
	}
	if outside > 0 {
		parts = append(parts, fmt.Sprintf("%d %s %s", outside, files(outside), ruleOutside))
	}
	return strings.Join(parts, ", ")
}

func files(n int) string {
	if n == 1 {
		return "file"
	}
	return "files"
}
