package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"time"

	"github.com/timhartmann7/redfirst/internal/cliux"
	"github.com/timhartmann7/redfirst/internal/config"
	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/gitx"
)

type doctorFlags struct {
	repo    string
	base    string
	workDir string
	timeout time.Duration
	noHooks bool
}

// diagnose prints the adoption state and what blocks the next tier.
//
// It is the one command that runs the project's own hooks and judges nothing.
// People drop off because they cannot tell what to fix, not because their stack
// is unsupported, and the three ways of breaking red-green quietly are all
// invisible until something has executed.
func diagnose(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	f, err := parseDoctorFlags(args, stderr)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()

	repo, err := gitx.Open(ctx, f.repo)
	if err != nil {
		return err
	}
	// A resolved commit rather than the ref the flag named: doctor reads the
	// hooks and lays out a working copy, and a branch that moved between the
	// two would have it answer for a tree it never ran.
	base, err := repo.Resolve(ctx, f.base)
	if err != nil {
		return err
	}
	cfg, source, err := config.Load(ctx, repo, base, configPath)
	// A config verify would refuse is the answer somebody came for rather than
	// a reason to stop. The rest of the diagnosis runs against the built-in
	// defaults, which is what the repository would be judged by without the
	// file, and the config row says what the file did wrong.
	configErr := err
	if err != nil && !errors.Is(err, domain.ErrConfig) {
		return err
	}
	if configErr != nil {
		cfg, source = config.Defaults(), config.SourceDefaults
	}

	return cliux.Doctor{
		Repo:         repo,
		BaseRef:      f.base,
		BaseSHA:      base,
		Config:       cfg,
		ConfigSource: source,
		ConfigError:  configErr,
		NoHooks:      f.noHooks,
		WorkDir:      f.workDir,
	}.Report(ctx, stdout)
}

func parseDoctorFlags(args []string, stderr io.Writer) (doctorFlags, error) {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var f doctorFlags
	fs.StringVar(&f.repo, "repo", ".", "path to the git repository")
	fs.StringVar(&f.base, "base", "origin/main", "the ref the config and the hooks are read from")
	fs.StringVar(&f.workDir, "work-dir", "", "where to create the working copy; empty means the system temp")
	fs.DurationVar(&f.timeout, "timeout", 30*time.Minute, "global deadline for the whole diagnosis")
	fs.BoolVar(&f.noHooks, "no-hooks", false, "answer from the repository alone, without running the hooks")

	if err := fs.Parse(args); err != nil {
		return doctorFlags{}, err
	}
	return f, nil
}
