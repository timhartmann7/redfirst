package domain_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
)

func TestPatternSet_MatchFollowsGitignoreAnchoring(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		pattern string
		path    string
		want    bool
	}{
		{"no slash matches at root", "Dockerfile*", "Dockerfile", true},
		{"no slash matches at any depth", "Dockerfile*", "deploy/Dockerfile.web", true},
		{"no slash matches base name only", "package-lock.json", "web/package-lock.json", true},
		{"slash anchors to the root", "src/lib/**", "src/lib/auth/token.ts", true},
		{"slash does not match deeper root", "src/lib/**", "web/src/lib/auth/token.ts", false},
		{"leading slash anchors too", "/redfirst.toml", "redfirst.toml", true},
		{"double star prefix matches root", "**/*.snap", "total.snap", true},
		{"double star prefix matches nested", "**/*.snap", "src/__snapshots__/total.snap", true},
		{"directory pattern matches contents", ".redfirst/**", ".redfirst/test.sh", true},
		{"directory pattern matches nested contents", ".redfirst/**", ".redfirst/payments/test.sh", true},
		// A trailing /** also matches the directory itself. gitignore says
		// otherwise, but a git diff only ever names files.
		{"directory pattern matches the directory too", ".redfirst/**", ".redfirst", true},
		{"trailing slash is a directory", "**/migrations/", "db/migrations", true},
		{"non-match", "src/**", "docs/readme.md", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			set, err := domain.NewPatternSet([]string{c.pattern})
			if err != nil {
				t.Fatalf("NewPatternSet(%q): %v", c.pattern, err)
			}
			if got := set.Match(c.path); got != c.want {
				t.Errorf("%q.Match(%q) = %v, want %v", c.pattern, c.path, got, c.want)
			}
		})
	}
}

func TestPatternSet_EmptySetMatchesNothing(t *testing.T) {
	t.Parallel()

	set, err := domain.NewPatternSet(nil)
	if err != nil {
		t.Fatalf("NewPatternSet(nil): %v", err)
	}
	if set.Match("anything.go") {
		t.Error("an empty set matched")
	}
	if got := set.Raw(); len(got) != 0 {
		t.Errorf("Raw() = %v, want empty", got)
	}
}

func TestPatternSet_InvalidGlobIsConfigError(t *testing.T) {
	t.Parallel()

	for _, pattern := range []string{"", "src/[", "/"} {
		if _, err := domain.NewPatternSet([]string{pattern}); !errors.Is(err, domain.ErrConfig) {
			t.Errorf("NewPatternSet(%q) error = %v, want ErrConfig", pattern, err)
		}
	}
}

func TestPatternSet_RawKeepsPatternsAsWritten(t *testing.T) {
	t.Parallel()

	raw := []string{"CLAUDE.md", ".claude/**"}
	set, err := domain.NewPatternSet(raw)
	if err != nil {
		t.Fatalf("NewPatternSet: %v", err)
	}
	got := set.Raw()
	if !slices.Equal(got, raw) {
		t.Fatalf("Raw() = %v, want %v", got, raw)
	}
	got[0] = "mutated"
	if set.Raw()[0] != "CLAUDE.md" {
		t.Error("Raw() handed out the internal slice")
	}
}

func TestRegexSet_MatchReturnsTheFiringPattern(t *testing.T) {
	t.Parallel()

	set, err := domain.NewRegexSet([]string{`@ts-ignore`, `catch\s*\(\s*\w*\s*\)\s*\{\s*\}`})
	if err != nil {
		t.Fatalf("NewRegexSet: %v", err)
	}

	pattern, ok := set.Match("  } catch (e) {}")
	if !ok {
		t.Fatal("empty catch did not match")
	}
	if want := `catch\s*\(\s*\w*\s*\)\s*\{\s*\}`; pattern != want {
		t.Errorf("Match returned %q, want %q", pattern, want)
	}

	if _, ok := set.Match("const total = sum(items)"); ok {
		t.Error("a clean line matched")
	}
}

func TestRegexSet_InvalidExpressionIsConfigError(t *testing.T) {
	t.Parallel()

	if _, err := domain.NewRegexSet([]string{"("}); !errors.Is(err, domain.ErrConfig) {
		t.Errorf("NewRegexSet error = %v, want ErrConfig", err)
	}
}

func TestRegexSet_RawKeepsExpressionsAsWritten(t *testing.T) {
	t.Parallel()

	raw := []string{`\.skip\(`}
	set, err := domain.NewRegexSet(raw)
	if err != nil {
		t.Fatalf("NewRegexSet: %v", err)
	}
	if !slices.Equal(set.Raw(), raw) {
		t.Errorf("Raw() = %v, want %v", set.Raw(), raw)
	}
}

// TestPatternSet_MatchPatternNamesTheGlobThatDecided covers what `redfirst
// explain` prints: the rule a path fell under, not the bare fact that it did.
func TestPatternSet_MatchPatternNamesTheGlobThatDecided(t *testing.T) {
	t.Parallel()

	set, err := domain.NewPatternSet([]string{"CLAUDE.md", "src/**", ".github/workflows/*.yml"})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "a name pattern matches at any depth", path: "docs/CLAUDE.md", want: "CLAUDE.md"},
		{name: "the first matching pattern wins", path: "src/lib/total.ts", want: "src/**"},
		{name: "a rooted pattern names itself", path: ".github/workflows/ci.yml", want: ".github/workflows/*.yml"},
		{name: "a path outside every list names nothing", path: "README.md", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := set.MatchPattern(tc.path)
			if got != tc.want {
				t.Errorf("MatchPattern(%q) = %q, want %q", tc.path, got, tc.want)
			}
			if ok != (tc.want != "") {
				t.Errorf("MatchPattern(%q) reported %v for pattern %q", tc.path, ok, got)
			}
			if set.Match(tc.path) != ok {
				t.Errorf("Match(%q) disagrees with MatchPattern", tc.path)
			}
		})
	}
}
