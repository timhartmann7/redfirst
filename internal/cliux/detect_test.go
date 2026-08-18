package cliux_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/timhartmann7/redfirst/internal/cliux"
)

// TestDetect_ReadsTheStackAndItsRunnerOffTheRootMarkers covers the `detected`
// line of doctor. It has one job: turn a repository into the preset name that
// goes into the command somebody pastes.
func TestDetect_ReadsTheStackAndItsRunnerOffTheRootMarkers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		markers map[string]string
		want    string
	}{
		{
			name:    "node names its runner in package.json",
			markers: map[string]string{"package.json": `{"devDependencies":{"vitest":"^2.0.0"}}`},
			want:    "node + vitest, no docker-compose",
		},
		{
			name:    "a dependency counts as much as a devDependency",
			markers: map[string]string{"package.json": `{"dependencies":{"jest":"^29.0.0"}}`},
			want:    "node + jest, no docker-compose",
		},
		{
			name:    "a runner we have no template for goes unnamed",
			markers: map[string]string{"package.json": `{"devDependencies":{"mocha":"^10.0.0"}}`},
			want:    "node, no test runner named by the markers, no docker-compose",
		},
		{
			name:    "package.json that will not parse still names the stack",
			markers: map[string]string{"package.json": "{ this is not json"},
			want:    "node, no test runner named by the markers, no docker-compose",
		},
		{
			name:    "compose beside a stack",
			markers: map[string]string{"package.json": "{}", "docker-compose.yaml": "services:\n"},
			want:    "node, no test runner named by the markers, docker-compose",
		},
		{
			name:    "pytest named by its own config file",
			markers: map[string]string{"pytest.ini": "[pytest]\n"},
			want:    "python + pytest, no docker-compose",
		},
		{
			name:    "pytest named inside pyproject.toml",
			markers: map[string]string{"pyproject.toml": "[tool.pytest.ini_options]\n"},
			want:    "python + pytest, no docker-compose",
		},
		{
			name:    "a python project on unittest is not sold pytest",
			markers: map[string]string{"setup.py": "from setuptools import setup\n"},
			want:    "python, no test runner named by the markers, no docker-compose",
		},
		{
			name:    "go",
			markers: map[string]string{"go.mod": "module example.com/x\n"},
			want:    "go + go test, no docker-compose",
		},
		{
			name:    "two stacks are both reported",
			markers: map[string]string{"package.json": "{}", "go.mod": "module example.com/x\n"},
			want:    "node and go, no test runner named by the markers, no docker-compose",
		},
		{
			name:    "nothing recognised",
			markers: map[string]string{"Makefile": "all:\n"},
			want:    "no stack markers at the repository root",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			for name, body := range tc.markers {
				if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
					t.Fatalf("write %s: %v", name, err)
				}
			}

			if got := cliux.Detect(root).String(); got != tc.want {
				t.Errorf("detected %q, want %q", got, tc.want)
			}
		})
	}
}

// TestDetect_ADirectoryNamedLikeAMarkerIsNotOne is the boundary a stat call
// walks into: a directory called package.json is not a package.json.
func TestDetect_ADirectoryNamedLikeAMarkerIsNotOne(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "package.json"), 0o755); err != nil {
		t.Fatalf("create the directory: %v", err)
	}

	if got := cliux.Detect(root).String(); got != "no stack markers at the repository root" {
		t.Errorf("detected %q from a directory", got)
	}
}

// TestDetect_AnUnreadableRootNamesNoStack keeps a suggestion from stopping the
// command that asked for it.
func TestDetect_AnUnreadableRootNamesNoStack(t *testing.T) {
	t.Parallel()

	got := cliux.Detect(filepath.Join(t.TempDir(), "no-such-directory"))

	if len(got.Stacks) != 0 || got.Compose {
		t.Errorf("markers = %+v, want nothing found", got)
	}
	if preset, err := got.Preset(); err != nil || preset != cliux.PresetBlank {
		t.Errorf("preset = %q (%v), want %q", preset, err, cliux.PresetBlank)
	}
}
