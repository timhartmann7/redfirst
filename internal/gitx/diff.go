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
	// checkAttrArgBudget caps the paths handed to one check-attr invocation. A
	// diff of thousands of files would otherwise overflow the argument limit.
	checkAttrArgBudget = 64 << 10
	// linguistGenerated is the attribute that takes a file out of the budget.
	linguistGenerated = "linguist-generated"
	// binaryCount is what numstat prints instead of a line count.
	binaryCount = "-"
)

// Diff is the change under judgement, merge-base to head.
func (r *Repo) Diff(ctx context.Context, base, head string) (domain.Diff, error) {
	baseSHA, err := r.Resolve(ctx, base)
	if err != nil {
		return domain.Diff{}, err
	}
	headSHA, err := r.Resolve(ctx, head)
	if err != nil {
		return domain.Diff{}, err
	}
	// The diff runs from the merge base, never two-dot: commits that landed on
	// base after the fork would otherwise read as reverts the agent wrote.
	mergeBase, err := r.MergeBase(ctx, baseSHA, headSHA)
	if err != nil {
		return domain.Diff{}, err
	}

	var files []domain.FileChange
	err = r.runStream(ctx, func(s *bufio.Scanner) error {
		var perr error
		files, perr = parseNameStatus(s)
		return perr
	}, diffArgs("--name-status", mergeBase, headSHA)...)
	if err != nil {
		return domain.Diff{}, err
	}

	byPath := indexByPath(files)
	err = r.runStream(ctx, func(s *bufio.Scanner) error {
		return parseNumstat(s, byPath)
	}, diffArgs("--numstat", mergeBase, headSHA)...)
	if err != nil {
		return domain.Diff{}, err
	}

	if err := r.markGenerated(ctx, mergeBase, files, byPath); err != nil {
		return domain.Diff{}, err
	}
	return domain.Diff{Base: baseSHA, Head: headSHA, MergeBase: mergeBase, Files: files}, nil
}

// diffArgs keeps the rename and copy flags identical between the two
// invocations, so the two outputs line up file for file.
//
// --attr-source pins .gitattributes to the merge base (git 2.42 and newer).
// Without it git reads attributes from the working tree, which is head: one
// line of `*.py -diff` on the agent's branch makes numstat print "-" for every
// count, and diff-budget then measures a diff the agent zeroed itself.
// Invariant 5 puts rule lists on base, and .gitattributes is one of them.
func diffArgs(format, from, to string) []string {
	return []string{
		"--attr-source=" + from,
		"diff", format, "-z", "--find-renames", "--find-copies", from, to,
	}
}

// indexByPath addresses the changes by their head path, which is unique inside
// one diff. The pointers stay valid because the slice is complete by now.
func indexByPath(files []domain.FileChange) map[string]*domain.FileChange {
	byPath := make(map[string]*domain.FileChange, len(files))
	for i := range files {
		byPath[files[i].Path] = &files[i]
	}
	return byPath
}

// parseNameStatus reads `status NUL path NUL` records. Rename and copy carry a
// second path, and the destination is the one that lands in Path.
func parseNameStatus(s *bufio.Scanner) ([]domain.FileChange, error) {
	var files []domain.FileChange
	for s.Scan() {
		status, err := domain.ParseFileStatus(s.Text())
		if err != nil {
			return nil, err
		}
		path, err := next(s, "the path after status "+string(status))
		if err != nil {
			return nil, err
		}
		change := domain.FileChange{Path: path, Status: status}
		if status.HasOldPath() {
			change.OldPath = path
			if change.Path, err = next(s, "the destination of status "+string(status)); err != nil {
				return nil, err
			}
		}
		files = append(files, change)
	}
	return files, nil
}

// parseNumstat reads `added TAB deleted TAB path NUL`. A rename or a copy
// leaves the path field empty and follows with source and destination.
func parseNumstat(s *bufio.Scanner, byPath map[string]*domain.FileChange) error {
	for s.Scan() {
		record := s.Text()
		fields := strings.SplitN(record, "\t", 3)
		if len(fields) != 3 {
			return fmt.Errorf("%w: unexpected git numstat record %q", domain.ErrInternal, record)
		}
		path, err := numstatPath(s, fields[2])
		if err != nil {
			return err
		}
		change, ok := byPath[path]
		if !ok {
			return fmt.Errorf("%w: git numstat reports %q, which --name-status did not",
				domain.ErrInternal, path)
		}
		if err := applyCounts(change, fields[0], fields[1]); err != nil {
			return err
		}
	}
	return nil
}

// numstatPath resolves the head path of one numstat record, pulling the extra
// pair of records a rename or a copy appends.
func numstatPath(s *bufio.Scanner, inline string) (string, error) {
	if inline != "" {
		return inline, nil
	}
	if _, err := next(s, "the source path of a numstat rename"); err != nil {
		return "", err
	}
	return next(s, "the destination path of a numstat rename")
}

// applyCounts fills the line counters. Git prints "-" on both sides of a
// binary file: the counters stay at zero and the flag carries the fact.
func applyCounts(change *domain.FileChange, added, deleted string) error {
	if added == binaryCount && deleted == binaryCount {
		change.Binary = true
		return nil
	}
	a, err := strconv.Atoi(added)
	if err != nil {
		return fmt.Errorf("%w: numstat added count %q for %q: %w",
			domain.ErrInternal, added, change.Path, err)
	}
	d, err := strconv.Atoi(deleted)
	if err != nil {
		return fmt.Errorf("%w: numstat deleted count %q for %q: %w",
			domain.ErrInternal, deleted, change.Path, err)
	}
	change.Added, change.Deleted = a, d
	return nil
}

// markGenerated flags the files that .gitattributes at the merge base calls
// linguist-generated. It belongs here because the gates stay pure functions of
// (Diff, Config) and make no git calls of their own. An empty diff runs no
// invocation at all.
func (r *Repo) markGenerated(
	ctx context.Context, tree string, files []domain.FileChange, byPath map[string]*domain.FileChange,
) error {
	parse := func(s *bufio.Scanner) error { return parseCheckAttr(s, byPath) }
	for _, chunk := range chunkPaths(files) {
		args := append([]string{"check-attr", "--source=" + tree, "-z", linguistGenerated, "--"}, chunk...)
		if err := r.runStream(ctx, parse, args...); err != nil {
			return err
		}
	}
	return nil
}

// chunkPaths cuts the head paths into argument lists no operating system will
// refuse, keeping git's order so two runs pass identical arguments.
func chunkPaths(files []domain.FileChange) [][]string {
	var (
		chunks  [][]string
		current []string
		size    int
	)
	for _, f := range files {
		if size+len(f.Path)+1 > checkAttrArgBudget && len(current) > 0 {
			chunks = append(chunks, current)
			current, size = nil, 0
		}
		current = append(current, f.Path)
		size += len(f.Path) + 1
	}
	if len(current) > 0 {
		chunks = append(chunks, current)
	}
	return chunks
}

// parseCheckAttr reads `path NUL attribute NUL value NUL` triples. Linguist
// counts both a bare `linguist-generated` and `linguist-generated=true` as the
// mark, so both values do here.
func parseCheckAttr(s *bufio.Scanner, byPath map[string]*domain.FileChange) error {
	for s.Scan() {
		path := s.Text()
		if _, err := next(s, "the attribute name from check-attr"); err != nil {
			return err
		}
		value, err := next(s, "the attribute value from check-attr")
		if err != nil {
			return err
		}
		change, ok := byPath[path]
		if !ok {
			return fmt.Errorf("%w: git check-attr reports %q, absent from the diff",
				domain.ErrInternal, path)
		}
		change.Generated = value == "set" || value == "true"
	}
	return nil
}
