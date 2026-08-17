package config_test

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/config"
	"github.com/timhartmann7/redfirst/internal/domain"
)

const configPath = "redfirst.toml"

// fakeSource stands in for internal/gitx: this package needs bytes from a ref,
// not a repository.
type fakeSource struct {
	body      string
	present   bool
	existsErr error
	showErr   error
}

func (f fakeSource) Exists(_ context.Context, _, _ string) (bool, error) {
	return f.present, f.existsErr
}

func (f fakeSource) ShowFile(_ context.Context, _, _ string) (io.ReadCloser, error) {
	if f.showErr != nil {
		return nil, f.showErr
	}
	return io.NopCloser(strings.NewReader(f.body)), nil
}

func withConfig(body string) fakeSource {
	return fakeSource{body: body, present: true}
}

// loadOK loads a config that must be accepted and checks it came from base.
func loadOK(t *testing.T, body string) domain.Config {
	t.Helper()
	cfg, source, err := config.Load(t.Context(), withConfig(body), "main", configPath)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	if want := "base:" + configPath; source != want {
		t.Fatalf("source = %q, want %q", source, want)
	}
	return cfg
}

// loadRejects asserts the refusal lands on exit code 3, not merely that some
// error came back, and that the message names each want.
func loadRejects(t *testing.T, body string, want ...string) {
	t.Helper()
	_, source, err := config.Load(t.Context(), withConfig(body), "main", configPath)
	if err == nil {
		t.Fatalf("Load accepted the config, source = %q", source)
	}
	if got := domain.ExitCode(err); got != 3 {
		t.Fatalf("exit code = %d, want 3; error: %v", got, err)
	}
	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error %q does not name %q", err, w)
		}
	}
}

func TestConfig_MissingFileUsesDefaults(t *testing.T) {
	t.Parallel()

	cfg, source, err := config.Load(t.Context(), fakeSource{}, "main", configPath)
	if err != nil {
		t.Fatalf("a missing config is a user choice, not an error: %v", err)
	}
	if source != config.SourceDefaults {
		t.Errorf("source = %q, want %q", source, config.SourceDefaults)
	}
	want := config.Defaults()
	if cfg.Budget != want.Budget {
		t.Errorf("budget = %+v, want %+v", cfg.Budget, want.Budget)
	}
	if !slices.Equal(cfg.Paths.Protected.Raw(), want.Paths.Protected.Raw()) {
		t.Errorf("protected = %v, want %v", cfg.Paths.Protected.Raw(), want.Paths.Protected.Raw())
	}
	if cfg.Tests.Immutability != want.Tests.Immutability {
		t.Errorf("immutability = %q, want %q", cfg.Tests.Immutability, want.Tests.Immutability)
	}
}

func TestConfig_EmptyFileKeepsDefaultsAndNamesTheBaseFile(t *testing.T) {
	t.Parallel()

	cfg := loadOK(t, "")
	want := config.Defaults()
	if cfg.Budget != want.Budget {
		t.Errorf("budget = %+v, want %+v", cfg.Budget, want.Budget)
	}
	if !slices.Equal(cfg.Tests.Patterns.Raw(), want.Tests.Patterns.Raw()) {
		t.Errorf("tests.patterns = %v, want %v", cfg.Tests.Patterns.Raw(), want.Tests.Patterns.Raw())
	}
}

func TestConfig_KeyReplacesNeighbourKeyKeepsDefault(t *testing.T) {
	t.Parallel()

	// The spec's own example: declaring `allowed` must not zero out `protected`.
	cfg := loadOK(t, "[paths]\nallowed = [\"src/**\"]\n")

	if got, want := cfg.Paths.Allowed.Raw(), []string{"src/**"}; !slices.Equal(got, want) {
		t.Errorf("allowed = %v, want %v", got, want)
	}
	if got, want := cfg.Paths.Protected.Raw(), config.Defaults().Paths.Protected.Raw(); !slices.Equal(got, want) {
		t.Errorf("protected = %v, want the default %v", got, want)
	}
	if got, want := cfg.Paths.Guarded.Raw(), config.Defaults().Paths.Guarded.Raw(); !slices.Equal(got, want) {
		t.Errorf("guarded = %v, want the default %v", got, want)
	}
}

func TestConfig_KeyReplacesScalarNeighbourSectionsKeepDefaults(t *testing.T) {
	t.Parallel()

	cfg := loadOK(t, "[budget]\nmax_files = 8\n")
	want := config.Defaults()

	if cfg.Budget.MaxFiles != 8 {
		t.Errorf("max_files = %d, want 8", cfg.Budget.MaxFiles)
	}
	if cfg.Budget.MaxAddedLines != want.Budget.MaxAddedLines {
		t.Errorf("max_added_lines = %d, want the default %d", cfg.Budget.MaxAddedLines, want.Budget.MaxAddedLines)
	}
	if cfg.RedGreen.ProbeRuns != want.RedGreen.ProbeRuns {
		t.Errorf("probe_runs = %d, want the default %d", cfg.RedGreen.ProbeRuns, want.RedGreen.ProbeRuns)
	}
	if cfg.Suite.RetryFailed != want.Suite.RetryFailed {
		t.Errorf("retry_failed = %d, want the default %d", cfg.Suite.RetryFailed, want.Suite.RetryFailed)
	}
}

func TestConfig_ListsNeverMerge(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		got  func(domain.Config) []string
		want []string
	}{
		{
			name: "protected replaces the built-in list",
			body: "[paths]\nprotected = [\"src/lib/server/**\"]\n",
			got:  func(c domain.Config) []string { return c.Paths.Protected.Raw() },
			want: []string{"src/lib/server/**"},
		},
		{
			name: "an empty protected list is the only way to an empty denylist",
			body: "[paths]\nprotected = []\n",
			got:  func(c domain.Config) []string { return c.Paths.Protected.Raw() },
			want: nil,
		},
		{
			name: "tests.patterns replaces the preset list",
			body: "[tests]\npatterns = [\"**/*.test.ts\"]\n",
			got:  func(c domain.Config) []string { return c.Tests.Patterns.Raw() },
			want: []string{"**/*.test.ts"},
		},
		{
			name: "suppression.patterns replaces the preset list",
			body: "[suppression]\npatterns = [\"@ts-ignore\"]\n",
			got:  func(c domain.Config) []string { return c.Suppression.Patterns.Raw() },
			want: []string{"@ts-ignore"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.got(loadOK(t, tc.body)); !slices.Equal(got, tc.want) {
				t.Errorf("list = %v, want %v", got, tc.want)
			}
		})
	}
}

// The severity map is the one place where per-key merging is the point: each
// entry is a key, and the unit of replacement is the key.
func TestConfig_SeverityMergesPerGate(t *testing.T) {
	t.Parallel()

	cfg := loadOK(t, "[severity]\nprotected-paths = \"fail\"\n")

	if got := cfg.SeverityOf(domain.GateDiffBudget); got != domain.SeverityWarn {
		t.Errorf("diff-budget severity = %q, want the default %q", got, domain.SeverityWarn)
	}
	if got := cfg.SeverityOf(domain.GateProtectedPaths); got != domain.SeverityFail {
		t.Errorf("protected-paths severity = %q, want %q", got, domain.SeverityFail)
	}
}

func TestConfig_UnknownKeyIsExit3(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "a typo does not become an empty denylist",
			body: "[paths]\nprotected_paths = [\"src/**\"]\n",
			want: []string{"paths.protected_paths"},
		},
		{
			name: "an unknown top-level key",
			body: "strict = true\n",
			want: []string{"strict"},
		},
		{
			name: "every offending key is named at once",
			body: "[tests]\nimmutable = \"strict\"\nrequire_test = true\n",
			want: []string{"tests.immutable", "tests.require_test"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			loadRejects(t, tc.body, tc.want...)
		})
	}
}

func TestConfig_RejectsInvalidTOML(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
	}{
		{name: "unterminated table header", body: "[budget\nmax_files = 8\n"},
		{name: "value without a key", body: "= 8\n"},
		{name: "binary content", body: "\x00\x01\x02redfirst\xff\xfe"},
		{name: "wrong type for a known key", body: "[budget]\nmax_files = \"eight\"\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			loadRejects(t, tc.body)
		})
	}
}

func TestConfig_GitFailureIsExit2(t *testing.T) {
	t.Parallel()

	broken := errors.New("git exploded")
	cases := []struct {
		name string
		src  fakeSource
	}{
		{name: "existence check fails", src: fakeSource{existsErr: broken}},
		{name: "reading the file fails", src: fakeSource{present: true, showErr: broken}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := config.Load(t.Context(), tc.src, "main", configPath)
			if got := domain.ExitCode(err); got != 2 {
				t.Fatalf("exit code = %d, want 2; error: %v", got, err)
			}
			if !errors.Is(err, broken) {
				t.Errorf("error %v does not wrap the underlying git failure", err)
			}
		})
	}
}
