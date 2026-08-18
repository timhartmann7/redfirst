package main

import (
	"context"
	"flag"
	"io"

	"github.com/timhartmann7/redfirst/internal/cliux"
	"github.com/timhartmann7/redfirst/internal/config"
	"github.com/timhartmann7/redfirst/internal/gitx"
)

type explainFlags struct {
	repo string
	base string
	path string
}

// explainRules prints the rule set a verify run would apply. It runs nothing
// and reads nothing but the config on the base ref: you reach for it while
// adopting a repository and while arguing about a verdict.
func explainRules(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	f, err := parseExplainFlags(args, stderr)
	if err != nil {
		return err
	}

	repo, err := gitx.Open(ctx, f.repo)
	if err != nil {
		return err
	}
	// The rules come from base, so this is where they get read. A config on
	// head is the agent's own writing and explaining it would describe rules
	// nothing applies.
	cfg, source, err := config.Load(ctx, repo, f.base, configPath)
	if err != nil {
		return err
	}
	return cliux.Explain(stdout, cfg, source, f.path)
}

func parseExplainFlags(args []string, stderr io.Writer) (explainFlags, error) {
	fs := flag.NewFlagSet("explain", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var f explainFlags
	fs.StringVar(&f.repo, "repo", ".", "path to the git repository")
	fs.StringVar(&f.base, "base", "origin/main", "where to read the config from")
	fs.StringVar(&f.path, "path", "", "print the effective rules for one path instead of the whole set")

	if err := fs.Parse(args); err != nil {
		return explainFlags{}, err
	}
	return f, nil
}
