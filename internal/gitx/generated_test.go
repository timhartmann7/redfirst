package gitx_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/testkit"
)

// baseAttributes uses all three spellings linguist understands, so the parser
// has to tell a value from a flag from a negation.
const baseAttributes = "*.pb.go linguist-generated=true\n" +
	"generated/** linguist-generated\n" +
	"vendored.go -linguist-generated\n"

func TestGitx_MarksLinguistGeneratedFromBaseAttributes(t *testing.T) {
	t.Parallel()

	r := testkit.NewRepo(t)
	r.Write(".gitattributes", baseAttributes)
	r.Write("api.pb.go", "package api\n\nconst Version = 1\n")
	r.Write("generated/data.go", "package generated\n\nvar Rows = 1\n")
	r.Write("vendored.go", "package main\n\nconst Vendored = 1\n")
	r.Write("src/hand.go", "package src\n\nconst Hand = 1\n")
	r.Commit("chore: seed a generated tree")

	r.Branch(testkit.FixtureHead)
	// Head disowns every rule. Invariant 5 says rule lists are read from base
	// only, so this rewrite must change nothing about the marking.
	r.Write(".gitattributes", "* -linguist-generated\n")
	r.Write("api.pb.go", "package api\n\nconst Version = 2\n")
	r.Write("generated/data.go", "package generated\n\nvar Rows = 2\n")
	r.Write("vendored.go", "package main\n\nconst Vendored = 2\n")
	r.Write("src/hand.go", "package src\n\nconst Hand = 2\n")
	r.Commit("feat: regenerate and edit")

	d := diffFixture(t, r)

	tests := []struct {
		name string
		path string
		want bool
	}{
		{"an attribute set to true marks the file", "api.pb.go", true},
		{"a bare attribute marks the file", "generated/data.go", true},
		{"a negated attribute leaves the file alone", "vendored.go", false},
		{"an unmentioned file is not generated", "src/hand.go", false},
		{"the attributes file itself is not generated", ".gitattributes", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := fileByPath(t, d, tc.path).Generated; got != tc.want {
				t.Errorf("generated for %s = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestGitx_MarksGeneratedAcrossChunkedPathLists(t *testing.T) {
	t.Parallel()

	// 400 paths of about 190 bytes are past the argument budget of one
	// check-attr invocation, so the marking has to survive being split. The
	// two kinds alternate along git's ordering, which puts generated files in
	// every chunk including the last one.
	const wideDiffFiles = 400
	pad := strings.Repeat("p", 170)

	r := testkit.NewRepo(t)
	r.Write(".gitattributes", "*-generated.txt linguist-generated\n")
	r.Commit("chore: mark the generated files")

	r.Branch(testkit.FixtureHead)
	for i := range wideDiffFiles {
		kind := "handwritten"
		if i%2 == 0 {
			kind = "generated"
		}
		r.Write(fmt.Sprintf("wide/%s-%03d-%s.txt", pad, i, kind), fmt.Sprintf("row %d\n", i))
	}
	r.Commit("feat: land a wide diff")

	d := diffFixture(t, r)
	if len(d.Files) != wideDiffFiles {
		t.Fatalf("files = %d, want %d", len(d.Files), wideDiffFiles)
	}
	for _, f := range d.Files {
		want := strings.HasSuffix(f.Path, "-generated.txt")
		if f.Generated != want {
			t.Fatalf("generated for %s = %v, want %v", f.Path, f.Generated, want)
		}
	}
}
