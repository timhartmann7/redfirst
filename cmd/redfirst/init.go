package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/timhartmann7/redfirst/internal/cliux"
	"github.com/timhartmann7/redfirst/internal/gitx"
)

type initFlags struct {
	repo   string
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

	var written []string
	if f.ci {
		path, err := cliux.InitCI(repo.Dir(), cliux.PinnedRelease())
		if err != nil {
			return err
		}
		written = append(written, path)
	}
	if f.config {
		path, err := cliux.InitConfig(repo.Dir())
		if err != nil {
			return err
		}
		written = append(written, path)
	}

	for _, path := range written {
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

	if err := fs.Parse(args); err != nil {
		return initFlags{}, err
	}
	if !f.ci && !f.config {
		fs.Usage()
		return initFlags{}, errors.New("nothing to generate: pass --ci, --config or both")
	}
	return f, nil
}
