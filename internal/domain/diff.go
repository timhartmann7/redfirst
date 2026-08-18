package domain

import (
	"fmt"
	"strings"
)

// FileStatus is the git change status of one file.
type FileStatus string

const (
	FileAdded      FileStatus = "A"
	FileCopied     FileStatus = "C"
	FileDeleted    FileStatus = "D"
	FileModified   FileStatus = "M"
	FileRenamed    FileStatus = "R"
	FileTypeChange FileStatus = "T"
	FileUnmerged   FileStatus = "U"
	FileUnknown    FileStatus = "X"
)

const statusScoreDigits = "0123456789"

// ParseFileStatus reads one --name-status code. Rename and copy carry a
// similarity score ("R100", "C75"); the score does not change the status.
func ParseFileStatus(code string) (FileStatus, error) {
	status := FileStatus(strings.TrimRight(code, statusScoreDigits))
	switch status {
	case FileAdded, FileCopied, FileDeleted, FileModified,
		FileRenamed, FileTypeChange, FileUnmerged, FileUnknown:
		return status, nil
	}
	return "", fmt.Errorf("%w: unknown git file status %q", ErrInternal, code)
}

// HasOldPath reports whether the status carries a source path alongside the
// destination. Rename and copy do; every other status names one file.
func (s FileStatus) HasOldPath() bool {
	return s == FileRenamed || s == FileCopied
}

// AddedLine is one line the diff added, numbered on head.
type AddedLine struct {
	Number int
	Text   string
}

// FileChange is one file in a diff.
type FileChange struct {
	// Path is the path on head. For a deletion it is the path that disappeared.
	Path string
	// OldPath is the source of a rename or a copy, empty otherwise.
	OldPath string
	Status  FileStatus
	Added   int
	Deleted int
	// Binary marks a file git reported as binary. Added and Deleted stay zero.
	Binary bool
	// Generated marks linguist-generated in .gitattributes at the base ref.
	Generated bool
	// Object is the git object id of the file on head, printed by
	// `git diff --raw`: forty zeroes where the file is gone. An object id names
	// content exactly, which is what the digests the report publishes are built
	// from.
	Object string
	// AddedLines is what the diff added, in file order, without the leading
	// plus. Only added lines travel: a gate judges what the diff introduced,
	// and the lines it left alone were never the agent's doing. Empty for a
	// binary file, where git reports no lines at all.
	AddedLines []AddedLine
}

// Diff is the change under judgement: merge-base to head.
type Diff struct {
	Base      string
	Head      string
	MergeBase string
	Files     []FileChange
}

// Stats counts files and lines.
type Stats struct {
	Files   int `json:"files"`
	Added   int `json:"added"`
	Deleted int `json:"deleted"`
}

// Totals counts every file in the diff.
func (d Diff) Totals() Stats {
	return d.StatsMatching(func(string) bool { return true })
}

// StatsMatching counts the files whose head path satisfies match.
func (d Diff) StatsMatching(match func(path string) bool) Stats {
	var s Stats
	for _, f := range d.Files {
		if !match(f.Path) {
			continue
		}
		s.Files++
		s.Added += f.Added
		s.Deleted += f.Deleted
	}
	return s
}
