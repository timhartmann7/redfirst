package cliux

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Preset is the hook template set `init --hooks` writes into .redfirst/.
type Preset string

const (
	PresetNodeUnit     Preset = "node-unit"
	PresetNodePostgres Preset = "node-postgres-docker"
	PresetPythonPytest Preset = "python-pytest"
	PresetGo           Preset = "go"
	PresetBlank        Preset = "blank"
	// PresetAuto names no template. It asks the markers of the repository to
	// pick one, which is what `init --hooks` does when nobody names a preset.
	PresetAuto Preset = "auto"
)

// Presets lists what `--hooks` accepts, in the order the flag help prints them.
func Presets() []Preset {
	return []Preset{PresetNodeUnit, PresetNodePostgres, PresetPythonPytest, PresetGo, PresetBlank, PresetAuto}
}

// stack is a language the presets know how to run tests for.
type stack struct {
	name string
	// preset is what `--hooks auto` writes for a repository of this stack, and
	// what doctor names in the command it prints.
	preset Preset
	// files are the root markers that say the stack is here. Any one of them.
	files []string
	// runner reads the test runner out of those markers, empty where none of
	// them names one.
	runner func(root string) string
}

// stacks is walked in order, so a repository carrying two sets of markers is
// reported the same way twice running.
func stacks() []stack {
	return []stack{
		{name: "node", preset: PresetNodeUnit, files: []string{"package.json"}, runner: nodeRunner},
		{
			name:   "python",
			preset: PresetPythonPytest,
			files:  []string{"pyproject.toml", "setup.py", "setup.cfg", "pytest.ini", "tox.ini"},
			runner: pythonRunner,
		},
		{name: "go", preset: PresetGo, files: []string{"go.mod"}, runner: func(string) string { return "go test" }},
	}
}

// composeFiles are the names docker compose reads by default.
var composeFiles = []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml"}

// Markers is what the files at the root of a repository say about it. It feeds
// two things: the preset `init --hooks` writes without being told which, and
// the `detected` line of `redfirst doctor`.
type Markers struct {
	// Stacks is every language recognised, in the order stacks() lists them.
	// More than one means the repository does not pick its own preset.
	Stacks []string
	// Runner is the test runner of the first stack, empty where the markers
	// name none.
	Runner  string
	Compose bool
}

// Detect reads the markers at the root of a repository.
//
// It reads the working tree rather than a ref: `init` and `doctor` run in the
// repository somebody has in front of them, and the point of both is to answer
// for the files that are there now. Everything that judges still comes from
// base, and nothing here judges.
func Detect(root string) Markers {
	var m Markers
	for _, s := range stacks() {
		if !anyPresent(root, s.files) {
			continue
		}
		if len(m.Stacks) == 0 {
			m.Runner = s.runner(root)
		}
		m.Stacks = append(m.Stacks, s.name)
	}
	m.Compose = anyPresent(root, composeFiles)
	return m
}

// Preset is what `--hooks auto` writes for this repository.
//
// Two stacks refuse rather than pick. A monorepo with a package.json beside a
// go.mod has no answer the markers can settle, and writing the wrong test.sh
// leaves somebody deleting generated files and wondering what the tool read.
func (m Markers) Preset() (Preset, error) {
	if len(m.Stacks) > 1 {
		return "", fmt.Errorf(
			"this repository carries markers for %s; name one with --hooks <preset>",
			strings.Join(m.Stacks, " and "))
	}
	if len(m.Stacks) == 0 {
		return PresetBlank, nil
	}
	for _, s := range stacks() {
		if s.name != m.Stacks[0] {
			continue
		}
		// Compose beside a Node project is the one combination with a preset of
		// its own: the services cost more to bring up than the suite does.
		if s.preset == PresetNodeUnit && m.Compose {
			return PresetNodePostgres, nil
		}
		return s.preset, nil
	}
	return PresetBlank, nil
}

// String is the `detected` line of doctor: the stack, its runner and whether
// the services need bringing up.
func (m Markers) String() string {
	if len(m.Stacks) == 0 {
		return "no stack markers at the repository root"
	}
	detected := strings.Join(m.Stacks, " and ")
	if m.Runner != "" {
		detected += " + " + m.Runner
	} else {
		detected += ", no test runner named by the markers"
	}
	if m.Compose {
		return detected + ", docker-compose"
	}
	return detected + ", no docker-compose"
}

// nodeRunner reads the runner out of package.json. Only the two the presets
// know are recognised: naming a runner we have no template for would put a
// command in the report that leads nowhere.
func nodeRunner(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return ""
	}
	var pkg struct {
		Dependencies    map[string]json.RawMessage `json:"dependencies"`
		DevDependencies map[string]json.RawMessage `json:"devDependencies"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return ""
	}
	for _, name := range []string{"vitest", "jest"} {
		_, dev := pkg.DevDependencies[name]
		_, prod := pkg.Dependencies[name]
		if dev || prod {
			return name
		}
	}
	return ""
}

// pythonRunner names pytest only where a marker says so. A python project on
// unittest exists, and telling its owner we detected pytest would send them to
// a preset that runs a command they do not have.
func pythonRunner(root string) string {
	if present(root, "pytest.ini") {
		return "pytest"
	}
	for _, name := range []string{"pyproject.toml", "setup.cfg", "tox.ini"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err == nil && strings.Contains(string(data), "pytest") {
			return "pytest"
		}
	}
	return ""
}

// anyKind reports whether a repository-relative path is in the working tree,
// file or directory alike: .redfirst/ is a directory and redfirst.toml is not.
func anyKind(root, rel string) bool {
	_, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
	return err == nil
}

func anyPresent(root string, names []string) bool {
	for _, name := range names {
		if present(root, name) {
			return true
		}
	}
	return false
}

// present reports whether a marker file is there. An unreadable root reads as
// no markers: detection suggests, and a suggestion may not stop the command.
func present(root, name string) bool {
	info, err := os.Stat(filepath.Join(root, name))
	if errors.Is(err, fs.ErrNotExist) || err != nil {
		return false
	}
	return !info.IsDir()
}
