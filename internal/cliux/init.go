package cliux

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Where the generated files go. Both paths are repository-relative: the caller
// passes the top level of the work tree.
const (
	// WorkflowPath is the one file tier 1 adds to a repository.
	WorkflowPath = ".github/workflows/redfirst.yml"
	// ConfigPath is the optional rule set, read from the base ref.
	ConfigPath = "redfirst.toml"
)

// Release is what a generated workflow installs: a version and the SHA256 of
// the linux/amd64 asset published under it.
type Release struct {
	Version string
	SHA256  string
}

// InitCI writes the workflow that runs redfirst on every pull request.
//
// It refuses a release it cannot pin. A workflow without a checksum still runs,
// which is why the refusal has to be here: the pin is what stops the binary
// under judgement from being swapped, and generating a weaker file quietly
// would take that away from whoever asked for the stronger one.
func InitCI(root string, rel Release) (string, error) {
	if rel.Version == "" || rel.SHA256 == "" {
		return "", errors.New(
			"this build knows no published release to pin; run `init --ci` from a released binary")
	}
	return write(root, WorkflowPath, workflowYAML(rel))
}

// InitConfig writes the strict rule set for agent branches. The built-in
// defaults stay wider on purpose: they judge human pull requests, where a gate
// that fires on an ordinary diff gets the tool deleted rather than the diff
// fixed.
func InitConfig(root string) (string, error) {
	return write(root, ConfigPath, configTOML())
}

// write puts one generated file in place and never over an existing one. A
// repository that already carries a workflow had a reason for it, and reading
// what is there beats guessing what it was.
func write(root, rel string, content string) (string, error) {
	full := filepath.Join(root, filepath.FromSlash(rel))

	switch _, err := os.Stat(full); {
	case err == nil:
		return "", fmt.Errorf("%s exists already; move it aside to generate a new one", rel)
	case !errors.Is(err, fs.ErrNotExist):
		return "", fmt.Errorf("look for %s: %w", rel, err)
	}

	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", fmt.Errorf("create the directory for %s: %w", rel, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", rel, err)
	}
	return rel, nil
}

// tomlList renders the items of a list, one per line, as the generated config
// prints them.
func tomlList(values []string) string {
	var b strings.Builder
	for _, v := range values {
		fmt.Fprintf(&b, "  %s,\n", tomlString(v))
	}
	return strings.TrimRight(b.String(), "\n")
}

// tomlString quotes a value the way TOML reads it back. The expressions in
// tests.forbid_disabled carry backslashes, and a generated config that fails to
// parse would be worse than no config at all.
func tomlString(v string) string {
	escaped := strings.ReplaceAll(v, `\`, `\\`)
	return `"` + strings.ReplaceAll(escaped, `"`, `\"`) + `"`
}
