package gitx_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/gitx"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

// spoofTitle is the one line the base file holds.
const spoofTitle = "# How a patch reads\n"

// spoofDoc adds a worked example to it: patch syntax at column zero, written
// as ordinary content. A document like this one lives in every repository that
// explains diffs, redfirst included.
const spoofDoc = spoofTitle + `
diff --git a/spoof.js b/spoof.js
@@ -1,4 +1,9 @@
--- a/spoof.js
+++ b/spoof.js
-not a deletion
+not an addition
`

// TestGitx_AddedLinesThatLookLikePatchSyntaxStayContent is the detector for the
// way hunks get read. The parser consumes exactly the number of lines the hunk
// header counts, so nothing inside a hunk can pass for a header: a parser that
// scanned for headers instead would take the added `diff --git` for another
// file, and one that trusted a leading plus would report the patch's own
// `+++ b/...` line as something the agent wrote.
func TestGitx_AddedLinesThatLookLikePatchSyntaxStayContent(t *testing.T) {
	t.Parallel()

	r := testkit.NewRepo(t)
	r.Write("docs/patch.md", spoofTitle)
	r.Commit("docs: open the patch guide")

	r.Branch(testkit.FixtureHead)
	r.Write("docs/patch.md", spoofDoc)
	r.Commit("docs: show what a patch looks like")

	got := fileByPath(t, diffFixture(t, r), "docs/patch.md").AddedLines

	body := strings.Split(spoofDoc, "\n")
	want := make([]domain.AddedLine, 0, len(body))
	for i, text := range body[1 : len(body)-1] {
		want = append(want, domain.AddedLine{Number: i + 2, Text: text})
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("added lines = %+v, want %+v", got, want)
	}
}

func TestGitx_NumbersAddedLinesFromTheHeadFile(t *testing.T) {
	t.Parallel()

	base := numbered("keep", 1, 8)
	head := "inserted at the top\n" + numbered("keep", 1, 4) +
		"inserted in the middle\n" + numbered("keep", 5, 8) + "inserted at the end\n"

	r := testkit.NewRepo(t)
	r.Write("src/list.js", base)
	r.Commit("feat: seed the list")

	r.Branch(testkit.FixtureHead)
	r.Write("src/list.js", head)
	r.Commit("feat: extend the list")

	got := fileByPath(t, diffFixture(t, r), "src/list.js").AddedLines
	// Three hunks, and each added line carries the number it has on head.
	want := []domain.AddedLine{
		{Number: 1, Text: "inserted at the top"},
		{Number: 6, Text: "inserted in the middle"},
		{Number: 11, Text: "inserted at the end"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("added lines = %+v, want %+v", got, want)
	}
}

func numbered(prefix string, from, to int) string {
	var b strings.Builder
	for i := from; i <= to; i++ {
		fmt.Fprintf(&b, "%s %d\n", prefix, i)
	}
	return b.String()
}

func TestGitx_MissingTrailingNewlineIsNotAnAddedLine(t *testing.T) {
	t.Parallel()

	r := testkit.NewRepo(t)
	// No newline at the end, so git marks the last line in the patch. The
	// marker sits inside the hunk and counts for neither side.
	r.Write("notes.txt", "one\ntwo")
	r.Commit("docs: start the notes")

	r.Branch(testkit.FixtureHead)
	r.Write("notes.txt", "one\ntwo\nthree\n")
	r.Commit("docs: finish the notes")

	got := fileByPath(t, diffFixture(t, r), "notes.txt").AddedLines
	want := []domain.AddedLine{{Number: 2, Text: "two"}, {Number: 3, Text: "three"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("added lines = %+v, want %+v", got, want)
	}
}

func TestGitx_FilesWithoutAddedLinesCarryNone(t *testing.T) {
	t.Parallel()

	r := testkit.NewRepo(t)
	r.Write("src/total.js", moduleSource)
	r.Write("drop.txt", unrelatedText)
	r.WriteBinary("assets/logo.png", []byte(logoBytes))
	r.Commit("feat: seed the tree")

	r.Branch(testkit.FixtureHead)
	r.Remove("drop.txt")
	r.Rename("src/total.js", "src/sum.js")
	r.WriteBinary("assets/logo.png", []byte(logoBytes+"\x11\x12"))
	r.Commit("refactor: rearrange the tree")

	d := diffFixture(t, r)
	tests := []struct {
		name string
		path string
	}{
		{"a deletion adds nothing", "drop.txt"},
		{"a pure rename adds nothing", "src/sum.js"},
		{"a binary file reports no lines at all", "assets/logo.png"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := fileByPath(t, d, tc.path).AddedLines; got != nil {
				t.Errorf("added lines of %s = %+v, want none", tc.path, got)
			}
		})
	}
}

func TestGitx_HunkHeaderGrammar(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name                  string
		header                string
		start, added, deleted int
	}{
		{"BothSidesCounted", "@@ -12,3 +14,5 @@", 14, 5, 3},
		// A count of one is written as no count at all.
		{"CountsLeftOut", "@@ -12 +14 @@", 14, 1, 1},
		{"OneSideLeftOut", "@@ -12,3 +14 @@", 14, 1, 3},
		{"ANewFile", "@@ -0,0 +1,9 @@", 1, 9, 0},
		{"APureDeletion", "@@ -5,2 +4,0 @@", 4, 0, 2},
		// Git appends the enclosing function, and it may hold anything.
		{"TrailingContext", "@@ -1,4 +1,9 @@ func main() { @@ -1 +1 @@", 1, 9, 4},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			start, added, deleted, err := gitx.ParseHunkHeader(tc.header)
			if err != nil {
				t.Fatalf("parse %q: %v", tc.header, err)
			}
			if start != tc.start || added != tc.added || deleted != tc.deleted {
				t.Errorf("parse %q = start %d, +%d, -%d, want start %d, +%d, -%d",
					tc.header, start, added, deleted, tc.start, tc.added, tc.deleted)
			}
		})
	}
}

// TestGitx_MalformedHunkHeaderIsOurBugNotASilentSkip keeps the parser loud. A
// header it cannot read means git prints something this code does not know
// about, and reading on would number added lines against a range nobody parsed.
func TestGitx_MalformedHunkHeaderIsOurBugNotASilentSkip(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		header string
	}{
		{"TooShort", "@@ -1,4 @@"},
		{"NoClosingMarker", "@@ -1,4 +1,9 ##"},
		{"WrongSignOnTheOldSide", "@@ +1,4 +1,9 @@"},
		{"AStartThatIsNotANumber", "@@ -x,4 +1,9 @@"},
		{"ACountThatIsNotANumber", "@@ -1,4 +1,z @@"},
		{"AnEmptyRange", "@@ - +1,9 @@"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, _, _, err := gitx.ParseHunkHeader(tc.header)
			if !errors.Is(err, domain.ErrInternal) {
				t.Errorf("parse %q = %v, want an internal error", tc.header, err)
			}
		})
	}
}

// TestGitx_TypeChangePairsWithItsOneNameStatusRecord is the regression test for
// the way a type change reaches the patch. Git writes the old symlink out as a
// deletion and the new regular file as an addition, under two identical
// `diff --git` headers, while --name-status reports the pair as a single T.
// A parser that counted headers would run one file ahead of itself from there
// on and hand every later gate another file's lines.
func TestGitx_TypeChangePairsWithItsOneNameStatusRecord(t *testing.T) {
	t.Parallel()

	r := testkit.NewRepo(t)
	r.Write("notes.txt", "a note\n")
	symlink(t, r, "notes.txt", "link.txt")
	r.Write("src/total.js", moduleSource)
	r.Commit("feat: seed the tree")

	r.Branch(testkit.FixtureHead)
	if err := os.Remove(filepath.Join(r.Dir, "link.txt")); err != nil {
		t.Fatalf("remove the symlink: %v", err)
	}
	r.Write("link.txt", "now a real file\n")
	r.Write("src/total.js", moduleEdited)
	r.Commit("chore: replace the link with a file")

	d := diffFixture(t, r)
	want := domain.FileChange{
		Path: "link.txt", Status: domain.FileTypeChange, Added: 1, Deleted: 1,
		AddedLines: []domain.AddedLine{{Number: 1, Text: "now a real file"}},
	}
	if got := fileByPath(t, d, "link.txt"); !reflect.DeepEqual(got, want) {
		t.Errorf("change = %+v, want %+v", got, want)
	}
	// The file behind it is where a slipped pairing would show.
	after := fileByPath(t, d, "src/total.js")
	if len(after.AddedLines) != 1 || after.AddedLines[0].Text != "export const rate = 9" {
		t.Errorf("added lines of src/total.js = %+v, want the line it changed", after.AddedLines)
	}
}

// TestGitx_ChangesWithoutSourceLinesPairUp covers the other two shapes that
// print unlike an ordinary edit: a mode change writes a header and no hunk at
// all, and a submodule writes a pointer line that is neither source nor text.
func TestGitx_ChangesWithoutSourceLinesPairUp(t *testing.T) {
	t.Parallel()

	r := testkit.NewRepo(t)
	r.Write("script.sh", "echo one\n")
	stageGitlink(t, r, "vendor/dep", "0000000000000000000000000000000000000001", "feat: seed the tree")

	r.Branch(testkit.FixtureHead)
	if err := os.Chmod(filepath.Join(r.Dir, "script.sh"), 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	stageGitlink(t, r, "vendor/dep", "0000000000000000000000000000000000000002", "chore: move the tree on")

	d := diffFixture(t, r)
	tests := []struct {
		name string
		want domain.FileChange
	}{
		{
			name: "a mode change carries no line at all",
			want: domain.FileChange{Path: "script.sh", Status: domain.FileModified},
		},
		{
			name: "a submodule pointer counts as one line either way",
			want: domain.FileChange{
				Path: "vendor/dep", Status: domain.FileModified, Added: 1, Deleted: 1,
				AddedLines: []domain.AddedLine{
					{Number: 1, Text: "Subproject commit 0000000000000000000000000000000000000002"},
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

func symlink(t *testing.T, r *testkit.Repo, target, name string) {
	t.Helper()

	if err := os.Symlink(target, filepath.Join(r.Dir, name)); err != nil {
		t.Fatalf("symlink %s -> %s: %v", name, target, err)
	}
}

// stageGitlink commits the worktree together with a submodule pointer. A
// gitlink needs no submodule on disk, and it has to go in after `git add -A`,
// which would otherwise stage the absence of the directory as a deletion.
func stageGitlink(t *testing.T, r *testkit.Repo, path, sha, message string) {
	t.Helper()

	r.Git("add", "-A")
	r.Git("update-index", "--add", "--cacheinfo", "160000,"+sha+","+path)
	r.Git("commit", "--quiet", "-m", message)
}

// hostileConfig is what a repository, a CI runner or a developer's ~/.gitconfig
// may carry. Every entry changes the shape of patch output, and redfirst runs
// git in someone else's repository: none of them may change what it reads.
func hostileConfig() [][2]string {
	return [][2]string{
		{"diff.interHunkContext", "6"},
		{"diff.submodule", "log"},
		{"diff.context", "7"},
		{"diff.noprefix", "true"},
		{"diff.mnemonicPrefix", "true"},
		{"diff.suppressBlankEmpty", "true"},
		{"color.diff", "always"},
	}
}

// TestGitx_AmbientDiffConfigCannotChangeWhatRedfirstReads runs the same diff
// twice, once against a repository carrying every setting that decides how a
// patch prints. Two of them used to be enough to end a run at exit code 4, and
// one of those two could instead file a submodule's lines under another path
// and pass every check on the way out.
func TestGitx_AmbientDiffConfigCannotChangeWhatRedfirstReads(t *testing.T) {
	t.Parallel()

	r := testkit.NewRepo(t)
	r.Write("src/list.js", numbered("keep", 1, 6))
	stageGitlink(t, r, "vendor/dep", "0000000000000000000000000000000000000001", "feat: seed the tree")

	r.Branch(testkit.FixtureHead)
	// Two changes far enough apart to sit in separate hunks, which is what
	// diff.interHunkContext fuses.
	r.Write("src/list.js", "changed first\n"+numbered("keep", 2, 5)+"changed last\n")
	stageGitlink(t, r, "vendor/dep", "0000000000000000000000000000000000000002", "feat: move the tree on")

	want := diffFixture(t, r)
	if len(want.Files) != 2 {
		t.Fatalf("the fixture holds %+v, want the source file and the submodule", want.Files)
	}

	for _, kv := range hostileConfig() {
		r.Git("config", kv[0], kv[1])
	}

	if got := diffFixture(t, r); !reflect.DeepEqual(got, want) {
		t.Errorf("the diff changed with the git config\n got: %+v\nwant: %+v", got.Files, want.Files)
	}
}
