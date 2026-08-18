package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

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
	target, err := repoRelative(repo.Dir(), f.path)
	if err != nil {
		return err
	}
	return cliux.Explain(stdout, cfg, source, target)
}

// repoRelative turns what somebody typed into the path the rules are written
// against. Rules name repository-relative paths, while a shell completes
// "./.claude/settings.json" and a paste carries the whole absolute one. Either
// spelling falls outside every rooted glob, and the answer would come out wrong
// rather than missing: the command exists to settle arguments.
func repoRelative(root, typed string) (string, error) {
	if typed == "" {
		return "", nil
	}
	target := typed
	switch prefix := workingPrefix(root); {
	case filepath.IsAbs(target):
		rel, err := filepath.Rel(resolved(root), resolved(target))
		if err != nil {
			return "", fmt.Errorf("cannot place %s inside %s: %w", typed, root, err)
		}
		target = rel
	case prefix != "":
		// A relative path reads one way to the shell that completed it and
		// another to the rules, which name paths from the top level. Both
		// readings exist, and a rooted glob like tests/** matches one and misses
		// the other, so neither gets answered: a wrong answer is the one thing a
		// command for settling arguments may not produce.
		clean := path.Clean(filepath.ToSlash(typed))
		return "", fmt.Errorf(
			"%s is ambiguous from %s: it names either %s or %s from the top level. "+
				"Pass an absolute path, or run at the top level",
			typed, prefix, path.Join(prefix, clean), clean)
	}
	target = path.Clean(filepath.ToSlash(target))
	if target == ".." || strings.HasPrefix(target, "../") {
		return "", fmt.Errorf("%s lies outside the repository at %s; name a path inside it", typed, root)
	}
	return target, nil
}

// workingPrefix is where the shell stands inside the work tree, empty when it
// stands at the top of it or outside it altogether. Outside covers the ordinary
// remote case, `explain --repo ../other`, where a relative path can only mean
// what the rules mean.
func workingPrefix(root string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	rel, err := filepath.Rel(resolved(root), resolved(cwd))
	if err != nil {
		return ""
	}
	prefix := filepath.ToSlash(rel)
	if prefix == "." || prefix == ".." || strings.HasPrefix(prefix, "../") {
		return ""
	}
	return prefix
}

// resolved follows the symlinks of a path that exists. A temporary directory on
// macOS is reached through one, and the same file then has two absolute
// spellings that no string comparison pairs up.
func resolved(p string) string {
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	return p
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
