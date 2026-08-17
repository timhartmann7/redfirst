package gitx_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
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
