package gitx_test

import (
	"reflect"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

const (
	guideDoc      = "# guide\n\nstep one\nstep two\nstep three\nstep four\n"
	guideDocEdit  = "# guide\n\nstep one\nstep two\nstep three\nstep five\n"
	moduleSource  = "export const rate = 1\nexport const tax = 2\nexport const fee = 3\n"
	moduleEdited  = "export const rate = 9\nexport const tax = 2\nexport const fee = 3\n"
	unrelatedText = "Shipping notes\n\nCarrier pickup happens twice a day.\n"
	logoBytes     = "\x89PNG\x00\x01\x02\x03\x04\x05"
	iconBytes     = "GIF89a\x00\xff\xfe\x10\x20\x30\x40"
)

func TestGitx_ParsesRenameWithBothPaths(t *testing.T) {
	t.Parallel()

	r := testkit.NewRepo(t)
	r.Write("src/total.js", moduleSource)
	r.Write("docs/guide.md", guideDoc)
	r.Commit("feat: seed the tree")

	r.Branch(testkit.FixtureHead)
	r.Rename("src/total.js", "src/sum.js")
	r.Rename("docs/guide.md", "docs/manual.md")
	r.Write("docs/manual.md", guideDocEdit)
	r.Commit("refactor: move the modules")

	d := diffFixture(t, r)

	tests := []struct {
		name string
		want domain.FileChange
	}{
		{
			name: "a pure rename carries source and destination",
			want: domain.FileChange{
				Path: "src/sum.js", OldPath: "src/total.js", Status: domain.FileRenamed,
			},
		},
		{
			name: "a rename with an edit keeps its line counts",
			want: domain.FileChange{
				Path: "docs/manual.md", OldPath: "docs/guide.md",
				Status: domain.FileRenamed, Added: 1, Deleted: 1,
				AddedLines: []domain.AddedLine{{Number: 6, Text: "step five"}},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := fileByPath(t, d, tc.want.Path); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("change = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestGitx_ParsesCopyWithSourcePath(t *testing.T) {
	t.Parallel()

	r := testkit.NewRepo(t)
	r.Write("src/total.js", moduleSource)
	r.Commit("feat: add the total")

	r.Branch(testkit.FixtureHead)
	// The source has to change for git to look for copies at all; the new file
	// is the base version of it, byte for byte.
	r.Write("src/total.js", moduleEdited)
	r.Write("src/subtotal.js", moduleSource)
	r.Commit("refactor: fork the module")

	got := fileByPath(t, diffFixture(t, r), "src/subtotal.js")
	want := domain.FileChange{Path: "src/subtotal.js", OldPath: "src/total.js", Status: domain.FileCopied}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("change = %+v, want %+v", got, want)
	}
}

func TestGitx_HandlesBinaryFile(t *testing.T) {
	t.Parallel()

	r := testkit.NewRepo(t)
	r.WriteBinary("assets/logo.png", []byte(logoBytes))
	r.Commit("feat: add the logo")

	r.Branch(testkit.FixtureHead)
	r.WriteBinary("assets/logo.png", []byte(logoBytes+"\x07\x06"))
	r.WriteBinary("assets/icon.gif", []byte(iconBytes))
	r.Commit("feat: redraw the assets")

	d := diffFixture(t, r)

	tests := []struct {
		name string
		want domain.FileChange
	}{
		{
			name: "a modified binary reports no line counts",
			want: domain.FileChange{Path: "assets/logo.png", Status: domain.FileModified, Binary: true},
		},
		{
			name: "an added binary reports no line counts",
			want: domain.FileChange{Path: "assets/icon.gif", Status: domain.FileAdded, Binary: true},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := fileByPath(t, d, tc.want.Path); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("change = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestGitx_EmptyDiff(t *testing.T) {
	t.Parallel()

	t.Run("the same ref on both sides holds no files", func(t *testing.T) {
		t.Parallel()

		r := twoBranches(t)
		d, err := openFixture(t, r).Diff(t.Context(), testkit.FixtureBase, testkit.FixtureBase)
		if err != nil {
			t.Fatalf("diff: %v", err)
		}
		if len(d.Files) != 0 {
			t.Errorf("files = %v, want none", headPaths(d))
		}
		if d.Base != d.Head || d.Head != d.MergeBase {
			t.Errorf("base %s, head %s and merge base %s should be one commit", d.Base, d.Head, d.MergeBase)
		}
	})

	t.Run("an empty commit on head holds no files", func(t *testing.T) {
		t.Parallel()

		r := testkit.NewRepo(t)
		r.Write("src/total.js", moduleSource)
		base := r.Commit("feat: seed the tree")
		r.Branch(testkit.FixtureHead)
		head := r.Commit("chore: change nothing")

		d := diffFixture(t, r)
		if len(d.Files) != 0 {
			t.Errorf("files = %v, want none", headPaths(d))
		}
		if d.Base != base || d.Head != head || d.MergeBase != base {
			t.Errorf("diff = %s..%s from %s, want %s..%s from %s",
				d.Base, d.Head, d.MergeBase, base, head, base)
		}
	})
}

func TestGitx_ParsesDeletion(t *testing.T) {
	t.Parallel()

	r := testkit.NewRepo(t)
	r.Write("docs/guide.md", guideDoc)
	r.Write("src/total.js", moduleSource)
	r.Commit("feat: seed the tree")

	r.Branch(testkit.FixtureHead)
	r.Remove("docs/guide.md")
	r.Commit("docs: drop the guide")

	got := fileByPath(t, diffFixture(t, r), "docs/guide.md")
	want := domain.FileChange{Path: "docs/guide.md", Status: domain.FileDeleted, Deleted: 6}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("change = %+v, want %+v", got, want)
	}
}

func TestGitx_CountsAddedAndDeletedLines(t *testing.T) {
	t.Parallel()

	r := testkit.NewRepo(t)
	r.Write("src/total.js", moduleSource)
	r.Commit("feat: seed the tree")

	r.Branch(testkit.FixtureHead)
	r.Write("src/total.js", moduleEdited)
	r.Write("docs/shipping.md", unrelatedText)
	r.Commit("fix: correct the rate")

	d := diffFixture(t, r)

	tests := []struct {
		name string
		want domain.FileChange
	}{
		{
			name: "one edited line counts once on each side",
			want: domain.FileChange{
				Path: "src/total.js", Status: domain.FileModified, Added: 1, Deleted: 1,
				AddedLines: []domain.AddedLine{{Number: 1, Text: "export const rate = 9"}},
			},
		},
		{
			name: "a new file counts every line as added",
			want: domain.FileChange{
				Path: "docs/shipping.md", Status: domain.FileAdded, Added: 3,
				AddedLines: []domain.AddedLine{
					{Number: 1, Text: "Shipping notes"},
					{Number: 2, Text: ""},
					{Number: 3, Text: "Carrier pickup happens twice a day."},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := fileByPath(t, d, tc.want.Path); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("change = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestGitx_KeepsGitFileOrder(t *testing.T) {
	t.Parallel()

	r := testkit.NewRepo(t)
	r.Write("app/main.go", "package main\n\nfunc main() {\n\tprintln(1)\n}\n")
	r.Write("docs/old.md", guideDoc)
	r.Write("drop.txt", unrelatedText)
	r.WriteBinary("assets/logo.png", []byte(logoBytes))
	r.Commit("feat: seed the tree")

	r.Branch(testkit.FixtureHead)
	r.Write("app/main.go", "package main\n\nfunc main() {\n\tprintln(2)\n}\n")
	r.Rename("docs/old.md", "docs/new.md")
	r.Remove("drop.txt")
	r.WriteBinary("assets/logo.png", []byte(logoBytes+"\x11"))
	r.Commit("refactor: rearrange the tree")

	got := diffFixture(t, r).Files
	// Git orders by destination path, and the order reaches the report as it
	// came: sorting here would make two runs disagree with git itself.
	want := []domain.FileChange{
		{
			Path: "app/main.go", Status: domain.FileModified, Added: 1, Deleted: 1,
			AddedLines: []domain.AddedLine{{Number: 4, Text: "\tprintln(2)"}},
		},
		{Path: "assets/logo.png", Status: domain.FileModified, Binary: true},
		{Path: "docs/new.md", OldPath: "docs/old.md", Status: domain.FileRenamed},
		{Path: "drop.txt", Status: domain.FileDeleted, Deleted: 3},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("files = %+v, want %+v", got, want)
	}
}

func TestGitx_DiffStartsFromMergeBase(t *testing.T) {
	t.Parallel()

	r := testkit.NewRepo(t)
	r.Write("src/total.js", moduleSource)
	fork := r.Commit("feat: seed the tree")

	r.Branch(testkit.FixtureHead)
	r.Write("src/total.js", moduleEdited)
	head := r.Commit("fix: correct the rate")

	// Base moves on while the agent works. A two-dot diff would read this file
	// as a deletion the agent wrote.
	r.Checkout(testkit.FixtureBase)
	r.Write("docs/shipping.md", unrelatedText)
	base := r.Commit("docs: describe shipping")

	d := diffFixture(t, r)

	if d.Base != base || d.Head != head || d.MergeBase != fork {
		t.Errorf("diff = %s..%s from %s, want %s..%s from %s",
			d.Base, d.Head, d.MergeBase, base, head, fork)
	}
	want := []domain.FileChange{
		{
			Path: "src/total.js", Status: domain.FileModified, Added: 1, Deleted: 1,
			AddedLines: []domain.AddedLine{{Number: 1, Text: "export const rate = 9"}},
		},
	}
	if !reflect.DeepEqual(d.Files, want) {
		t.Errorf("files = %+v, want only the branch change %+v", d.Files, want)
	}
}
