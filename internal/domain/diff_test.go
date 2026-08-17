package domain_test

import (
	"errors"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
)

func TestDomain_FileChangeStatusRoundTrip(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		code       string
		want       domain.FileStatus
		hasOldPath bool
	}{
		{"added", "A", domain.FileAdded, false},
		{"copied", "C", domain.FileCopied, true},
		{"copied with score", "C75", domain.FileCopied, true},
		{"deleted", "D", domain.FileDeleted, false},
		{"modified", "M", domain.FileModified, false},
		{"renamed", "R", domain.FileRenamed, true},
		{"renamed with score", "R100", domain.FileRenamed, true},
		{"type change", "T", domain.FileTypeChange, false},
		{"unmerged", "U", domain.FileUnmerged, false},
		{"unknown", "X", domain.FileUnknown, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got, err := domain.ParseFileStatus(c.code)
			if err != nil {
				t.Fatalf("ParseFileStatus(%q): %v", c.code, err)
			}
			if got != c.want {
				t.Errorf("ParseFileStatus(%q) = %q, want %q", c.code, got, c.want)
			}
			if string(got) != c.code[:1] {
				t.Errorf("round trip of %q gave %q", c.code, string(got))
			}
			if got.HasOldPath() != c.hasOldPath {
				t.Errorf("%q.HasOldPath() = %v, want %v", got, got.HasOldPath(), c.hasOldPath)
			}
		})
	}
}

func TestDomain_ParseFileStatusRejectsUnknownCode(t *testing.T) {
	t.Parallel()

	for _, code := range []string{"", "Z", "42", "AM"} {
		if _, err := domain.ParseFileStatus(code); !errors.Is(err, domain.ErrInternal) {
			t.Errorf("ParseFileStatus(%q) error = %v, want ErrInternal", code, err)
		}
	}
}

func TestDomain_StatsMatchingCountsOnlyMatchingFiles(t *testing.T) {
	t.Parallel()

	diff := domain.Diff{Files: []domain.FileChange{
		{Path: "src/total.ts", Status: domain.FileModified, Added: 10, Deleted: 4},
		{Path: "src/total.test.ts", Status: domain.FileAdded, Added: 31},
		{Path: "logo.png", Status: domain.FileModified, Binary: true},
	}}

	if got, want := diff.Totals(), (domain.Stats{Files: 3, Added: 41, Deleted: 4}); got != want {
		t.Errorf("Totals() = %+v, want %+v", got, want)
	}

	isTest := func(path string) bool { return path == "src/total.test.ts" }
	if got, want := diff.StatsMatching(isTest), (domain.Stats{Files: 1, Added: 31}); got != want {
		t.Errorf("StatsMatching(tests) = %+v, want %+v", got, want)
	}
}

func TestDomain_StatsOnEmptyDiffAreZero(t *testing.T) {
	t.Parallel()

	if got, want := (domain.Diff{}).Totals(), (domain.Stats{}); got != want {
		t.Errorf("Totals() on an empty diff = %+v, want %+v", got, want)
	}
}
