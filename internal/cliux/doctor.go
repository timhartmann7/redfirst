package cliux

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/timhartmann7/redfirst/internal/config"
	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/harness"
)

// Repo is the slice of git doctor needs. It is declared here, at the consumer;
// internal/gitx satisfies it.
type Repo interface {
	// Dir is the top level of the work tree, where the stack markers are read.
	Dir() string
	// Exists answers for a path in a ref: the config, the hooks and the
	// workflow all have to be on base before they decide anything.
	Exists(ctx context.Context, ref, path string) (bool, error)
	// ShowFile reads one file out of a ref.
	ShowFile(ctx context.Context, ref, path string) (io.ReadCloser, error)
	// Export lays a tree out as a working copy. The harness runs the hooks
	// against it.
	Export(ctx context.Context, ref, dir string) error
}

// Doctor answers what works now, what does not and why.
//
// It is the one command that runs the project's hooks without judging anything.
// Three ways of breaking red-green cost nothing at the moment they are written
// and everything at the moment a verdict depends on them: results without
// per-case names, an ignored $REDFIRST_FILTER and a runner replaying its own
// previous verdict. None of them is visible from the diff, so doctor executes.
type Doctor struct {
	Repo Repo
	// BaseRef is the ref as somebody typed it, which is what the messages name.
	BaseRef string
	// BaseSHA is that ref resolved. The hooks and the working copy come from a
	// commit rather than a branch: a ref that moved mid-run would answer for
	// two different trees.
	BaseSHA string
	Config  domain.Config
	// ConfigSource is what config.Load reported: "defaults" or "base:<path>".
	ConfigSource string
	// ConfigError is what config.Load refused, and nil where it accepted the
	// file. A config verify would answer with exit code 3 is the likeliest
	// reason somebody runs this command, so doctor names it and carries on
	// against the built-in defaults rather than stopping on the one line verify
	// already prints.
	ConfigError error
	// NoHooks keeps the environment down, the way --no-hooks does for verify.
	NoHooks bool
	WorkDir string
}

// Report writes the adoption state and what blocks the next tier.
//
// A finding never changes the exit code. Doctor is read by a person deciding
// what to fix, not by an orchestrator deciding whom to blame, and a command
// that exits non-zero because it found something to say would break the first
// CI job somebody puts it in.
func (d Doctor) Report(ctx context.Context, w io.Writer) error {
	hooks, err := d.locate(ctx, harness.HooksDir)
	if err != nil {
		return err
	}
	config, err := d.locateConfig(ctx)
	if err != nil {
		return err
	}
	workflow, err := d.locate(ctx, WorkflowPath)
	if err != nil {
		return err
	}

	markers := Detect(d.Repo.Dir())
	rows := []row{
		{key: "config", values: []string{config}},
		{key: "hooks", values: []string{d.hooksRow(hooks)}},
		{key: "workflow", values: []string{d.workflowRow(workflow)}},
		{key: "detected", values: []string{markers.String()}},
	}
	if hooks == onBase {
		rows = append(rows, d.harnessRows(ctx, markers)...)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "redfirst v%s · doctor · tier=%d\n\n", domain.Version, tier(hooks, workflow))
	writeRows(&b, indent(rows))
	writeNextTier(&b, tier(hooks, workflow), markers)

	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("write the diagnosis: %w", err)
	}
	return nil
}

// where a file was found. The middle value is the one worth a command of its
// own: everything that judges is read from base, so a hook set that exists only
// in the working tree decides nothing, and the person who just ran `init` has
// no way of seeing that from the filesystem.
type where int

const (
	nowhere where = iota
	inWorkTree
	onBase
)

func (d Doctor) locate(ctx context.Context, path string) (where, error) {
	onBaseRef, err := d.Repo.Exists(ctx, d.BaseSHA, path)
	if err != nil {
		return nowhere, err
	}
	if onBaseRef {
		return onBase, nil
	}
	if anyKind(d.Repo.Dir(), path) {
		return inWorkTree, nil
	}
	return nowhere, nil
}

// locateConfig names the file the rules came from. config.Load has already
// decided that, and repeating the decision here would let the two disagree.
func (d Doctor) locateConfig(ctx context.Context) (string, error) {
	if d.ConfigError != nil {
		return fmt.Sprintf("%s on %s is invalid, and verify answers it with exit code 3: %v",
			ConfigPath, d.BaseRef, d.ConfigError), nil
	}
	if d.ConfigSource != config.SourceDefaults {
		return d.ConfigSource, nil
	}
	found, err := d.locate(ctx, ConfigPath)
	if err != nil {
		return "", err
	}
	if found == inWorkTree {
		return fmt.Sprintf("%s in the working tree, not on %s → "+
			"the rules come from base; commit it and merge it before it applies", ConfigPath, d.BaseRef), nil
	}
	return "not found → using built-in defaults", nil
}

func (d Doctor) hooksRow(found where) string {
	switch found {
	case onBase:
		return fmt.Sprintf("base:%s/ → red-green and suite-green run", harness.HooksDir)
	case inWorkTree:
		return fmt.Sprintf("%s/ in the working tree, not on %s → "+
			"the hooks come from base; commit them and merge them before they run", harness.HooksDir, d.BaseRef)
	}
	return "not found → red-green, suite-green unavailable"
}

func (d Doctor) workflowRow(found where) string {
	switch found {
	case onBase:
		return "base:" + WorkflowPath
	case inWorkTree:
		return fmt.Sprintf("%s in the working tree, not on %s → "+
			"no pull request is judged until it is merged", WorkflowPath, d.BaseRef)
	}
	return "not found → nothing runs redfirst on a pull request"
}

// tier is the adoption state from section 4 of the spec: no files, one
// workflow, or the workflow plus the hooks.
func tier(hooks, workflow where) int {
	switch {
	case hooks == onBase:
		return 2
	case workflow == onBase:
		return 1
	}
	return 0
}

// writeNextTier prints the specific command that moves the repository up one
// step. Tier 2 is the top, and a diagnosis that ended in advice for a tier
// somebody already reached would read as though it had found nothing.
func writeNextTier(b *strings.Builder, current int, markers Markers) {
	switch current {
	case 0:
		b.WriteString("\n  To reach tier 1:\n    redfirst init --ci\n")
	case 1:
		preset, err := markers.Preset()
		if err != nil {
			fmt.Fprintf(b, "\n  To reach tier 2:\n    redfirst init --hooks <preset>\n    %s\n", err)
			return
		}
		fmt.Fprintf(b, "\n  To reach tier 2:\n    redfirst init --hooks %s\n", preset)
	}
}

// indent puts the two spaces of the sample output in section 5 of the spec in
// front of every key, without making the column arithmetic count them.
func indent(rows []row) []row {
	out := make([]row, 0, len(rows))
	for _, r := range rows {
		out = append(out, row{key: "  " + r.key, values: r.values})
	}
	return out
}
