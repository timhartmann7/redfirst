package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/timhartmann7/redfirst/internal/cliux"
	"github.com/timhartmann7/redfirst/internal/gitx"
	"github.com/timhartmann7/redfirst/internal/harness"
)

type initFlags struct {
	repo   string
	hooks  string
	ci     bool
	config bool
}

// initRepo generates the files a repository needs to be judged: the workflow
// that runs redfirst, and the rule set it runs under. Both come out of what the
// binary already carries, so the command reaches no network and two runs of it
// write the same bytes.
func initRepo(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	f, err := parseInitFlags(args, stderr)
	if err != nil {
		return err
	}
	// The generated paths are repository-relative, and gitx anchors at the top
	// level: run from a subdirectory, the workflow still lands where a runner
	// looks for it.
	repo, err := gitx.Open(ctx, f.repo)
	if err != nil {
		return err
	}

	// Reported as each file lands rather than at the end. A refusal on the
	// third generator leaves the first two on disk either way, and a run that
	// went quiet about them would send somebody looking for a repository that
	// is no longer the one they started with.
	if f.ci {
		path, err := cliux.InitCI(repo.Dir(), cliux.PinnedRelease())
		if err != nil {
			return err
		}
		if err := announce(stdout, path); err != nil {
			return err
		}
	}
	if f.config {
		path, err := cliux.InitConfig(repo.Dir())
		if err != nil {
			return err
		}
		if err := announce(stdout, path); err != nil {
			return err
		}
	}
	if f.hooks != "" {
		return initHooks(stdout, repo.Dir(), cliux.Preset(f.hooks))
	}
	return nil
}

// initHooks writes a hook set and names the preset it wrote: somebody who named
// none has to see the one they got.
func initHooks(stdout io.Writer, root string, p cliux.Preset) error {
	preset, paths, err := cliux.InitHooks(root, p)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "preset %s\n", preset); err != nil {
		return err
	}
	return announce(stdout, paths...)
}

// announce names what a generator wrote.
func announce(stdout io.Writer, paths ...string) error {
	for _, path := range paths {
		if _, err := fmt.Fprintf(stdout, "wrote %s\n", path); err != nil {
			return err
		}
	}
	return nil
}

func parseInitFlags(args []string, stderr io.Writer) (initFlags, error) {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var f initFlags
	fs.StringVar(&f.repo, "repo", ".", "path to the git repository")
	fs.BoolVar(&f.ci, "ci", false, "generate "+cliux.WorkflowPath+" with a pinned version and SHA256")
	fs.BoolVar(&f.config, "config", false, "generate "+cliux.ConfigPath+" from the built-in defaults, with comments")
	fs.StringVar(&f.hooks, "hooks", "",
		"drop hook templates into "+harness.HooksDir+"/: "+cliux.PresetNames())

	if err := fs.Parse(args); err != nil {
		return initFlags{}, err
	}
	if !f.ci && !f.config && f.hooks == "" {
		fs.Usage()
		return initFlags{}, errors.New("nothing to generate: pass --ci, --config, --hooks or several")
	}
	return f, nil
}
