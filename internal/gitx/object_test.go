package gitx_test

import (
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

// zeroObject is what git prints where a path holds nothing on the side it is
// reporting: a deletion has no object on head.
const zeroObject = "0000000000000000000000000000000000000000"

// TestGitx_CarriesTheHeadObjectID pins what identifies a changed file: the id
// of what it became. The digests the report publishes are built from it, so a
// diff that lost it would hand the orchestrator a value that no longer names
// the patch.
func TestGitx_CarriesTheHeadObjectID(t *testing.T) {
	t.Parallel()

	r := testkit.NewRepo(t)
	r.Write("src/total.js", moduleSource)
	r.Write("docs/guide.md", guideDoc)
	r.WriteBinary("assets/logo.png", []byte(logoBytes))
	r.Commit("feat: seed the tree")

	r.Branch(testkit.FixtureHead)
	r.Write("src/total.js", moduleEdited)
	r.Remove("docs/guide.md")
	r.Write("docs/shipping.md", unrelatedText)
	r.WriteBinary("assets/logo.png", []byte(logoBytes+"\x07\x06"))
	r.Commit("feat: rework the tree")

	d := rawDiffFixture(t, r)

	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "a modified file names what it became on head",
			path: "src/total.js",
			want: r.Git("rev-parse", testkit.FixtureHead+":src/total.js"),
		},
		{
			name: "an added file names its new blob",
			path: "docs/shipping.md",
			want: r.Git("rev-parse", testkit.FixtureHead+":docs/shipping.md"),
		},
		{
			name: "a binary file is identified like any other",
			path: "assets/logo.png",
			want: r.Git("rev-parse", testkit.FixtureHead+":assets/logo.png"),
		},
		{
			name: "a deleted file holds nothing on head",
			path: "docs/guide.md",
			want: zeroObject,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := fileByPath(t, d, tc.path).Object; got != tc.want {
				t.Errorf("object of %s = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

// TestGitx_ObjectIDReadsATreeOutOfTheBaseRef covers the lookup the base probe
// key needs: one id standing for every hook in the directory.
func TestGitx_ObjectIDReadsATreeOutOfTheBaseRef(t *testing.T) {
	t.Parallel()

	r := testkit.HookedFix(t)
	repo := openFixture(t, r)

	got, err := repo.ObjectID(t.Context(), testkit.FixtureBase, ".redfirst")
	if err != nil {
		t.Fatalf("object id of the hook directory: %v", err)
	}
	if want := r.Git("rev-parse", testkit.FixtureBase+":.redfirst"); got != want {
		t.Errorf("object id = %q, want %q", got, want)
	}

	// A hook edited on base changes the id, which is what makes it usable as
	// part of a key: the same id means the same scripts ran.
	r.Checkout(testkit.FixtureBase)
	r.WriteScript(".redfirst/test.sh", "#!/bin/sh\necho rewritten\n")
	r.Commit("chore: rewrite the test hook")

	after, err := repo.ObjectID(t.Context(), testkit.FixtureBase, ".redfirst")
	if err != nil {
		t.Fatalf("object id after the edit: %v", err)
	}
	if after == got {
		t.Error("the hook directory kept its object id after a hook changed")
	}
}

// TestGitx_ObjectIDRefusesAPathThatIsNotThere holds the harness boundary: an
// unreadable object is a broken repository, exit code 2, not an empty answer a
// caller could mistake for a fact.
func TestGitx_ObjectIDRefusesAPathThatIsNotThere(t *testing.T) {
	t.Parallel()

	r := testkit.CleanFix(t)

	_, err := openFixture(t, r).ObjectID(t.Context(), testkit.FixtureBase, ".redfirst")
	if err == nil {
		t.Fatal("a missing path returned an object id")
	}
	if domain.ExitCode(err) != 2 {
		t.Errorf("exit code %d for %v, want 2", domain.ExitCode(err), err)
	}
	if !strings.Contains(err.Error(), ".redfirst") {
		t.Errorf("error %q does not name the path it could not read", err)
	}
}
