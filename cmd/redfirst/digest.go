package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/harness"
	"github.com/timhartmann7/redfirst/internal/runner"
)

// Each digest carries the purpose it was computed for, so a value that
// identifies a patch can never equal one that identifies a probe.
const (
	diffPurpose      = "redfirst/diff-digest/1"
	baseProbePurpose = "redfirst/base-probe-key/1"
)

// objectSource is the slice of git the base probe key needs. It is declared at
// the consumer; internal/gitx satisfies it.
type objectSource interface {
	ObjectID(ctx context.Context, ref, path string) (string, error)
}

// diffDigest names the patch under judgement, for an orchestrator that wants to
// tell two attempts apart. redfirst never reads it.
//
// What a diff is: a commit to start from, and for every path what the diff did
// to it and what it became. A git object id names content exactly, so two
// digests agree only where the changed files agree byte for byte. Line counts
// and added lines would say the same thing less precisely: a patch that deletes
// a different line while adding the same one carries the same counts and the
// same added lines, and two distinct attempts would hash alike.
func diffDigest(diff domain.Diff) string {
	return digest(diffPurpose, append([]string{diff.MergeBase}, changeFields(diff.Files)...)...)
}

// baseProbeKey names the base probe of red-green: the commit the probe runs on,
// the hooks that run it, and the head content of the test files overlaid onto
// it. Nothing else decides the outcome the key stands for.
//
// It stays empty where no base probe would run at all. A key for a probe that
// never happened would look like a cache entry nobody may have.
func baseProbeKey(
	ctx context.Context, src objectSource, base string,
	diff domain.Diff, cfg domain.Config, caps runner.CapSet,
) (string, error) {
	if !caps.Has(domain.CapHooks) || !caps.Has(domain.CapTestFiles) {
		return "", nil
	}
	hooks, err := src.ObjectID(ctx, base, harness.HooksDir)
	if err != nil {
		return "", err
	}

	fields := []string{diff.MergeBase, hooks}
	fields = append(fields, changeFields(testSurface(diff, cfg))...)
	return digest(baseProbePurpose, fields...), nil
}

// testSurface is the file set the probe overlays: everything in the diff that
// decides a test outcome. A rename counts under both of its paths, the same way
// the capability does, so the key covers every file that made the probe happen.
func testSurface(diff domain.Diff, cfg domain.Config) []domain.FileChange {
	var files []domain.FileChange
	for _, f := range diff.Files {
		if cfg.IsTestSurface(f.Path) || (f.OldPath != "" && cfg.IsTestSurface(f.OldPath)) {
			files = append(files, f)
		}
	}
	return files
}

// changeFields turns the changes into the fields a digest is built from, sorted
// by head path so the value does not depend on the order git printed them in. A
// head path is unique inside one diff, which makes the order total.
func changeFields(files []domain.FileChange) []string {
	sorted := slices.SortedFunc(slices.Values(files), func(a, b domain.FileChange) int {
		return strings.Compare(a.Path, b.Path)
	})
	fields := make([]string, 0, len(sorted)*4)
	for _, f := range sorted {
		fields = append(fields, string(f.Status), f.OldPath, f.Path, f.Object)
	}
	return fields
}

// digest hashes NUL-terminated fields. NUL is the one byte a path cannot hold,
// so no arrangement of paths can produce the byte sequence of another.
func digest(purpose string, fields ...string) string {
	var b strings.Builder
	b.WriteString(purpose)
	b.WriteByte(0)
	for _, f := range fields {
		b.WriteString(f)
		b.WriteByte(0)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return "sha256:" + hex.EncodeToString(sum[:])
}
