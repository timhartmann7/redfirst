package domain

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// PatternSet is a compiled list of gitignore-style globs. Compilation happens
// once, when the config loads: the gates match every file of every diff.
type PatternSet struct {
	raw   []string
	globs []globPattern
}

// globPattern follows gitignore: a pattern holding no slash matches the base
// name at any depth, anything else matches the path from the repository root.
type globPattern struct {
	expr     string
	baseOnly bool
}

// NewPatternSet compiles raw patterns. An unparseable pattern is a config
// error, not a pattern that silently matches nothing.
func NewPatternSet(raw []string) (PatternSet, error) {
	set := PatternSet{raw: slices.Clone(raw), globs: make([]globPattern, 0, len(raw))}
	for _, r := range raw {
		g, err := compileGlob(r)
		if err != nil {
			return PatternSet{}, err
		}
		set.globs = append(set.globs, g)
	}
	return set, nil
}

func compileGlob(raw string) (globPattern, error) {
	expr := strings.TrimSuffix(raw, "/")
	if expr == "" {
		return globPattern{}, fmt.Errorf("%w: empty glob pattern", ErrConfig)
	}
	baseOnly := !strings.Contains(expr, "/")
	expr = strings.TrimPrefix(expr, "/")
	if !doublestar.ValidatePattern(expr) {
		return globPattern{}, fmt.Errorf("%w: invalid glob pattern %q", ErrConfig, raw)
	}
	return globPattern{expr: expr, baseOnly: baseOnly}, nil
}

// Match reports whether a repository-relative path falls in the set.
func (p PatternSet) Match(name string) bool {
	_, ok := p.MatchPattern(name)
	return ok
}

// MatchPattern returns the first pattern the path falls under, the way
// RegexSet.Match returns the expression that fired. `redfirst explain` prints
// the rule that decided: a bare "protected: yes" leaves a reader arguing with a
// verdict that points at nothing.
func (p PatternSet) MatchPattern(name string) (string, bool) {
	base := path.Base(name)
	for i, g := range p.globs {
		target := name
		if g.baseOnly {
			target = base
		}
		if ok, err := doublestar.Match(g.expr, target); err == nil && ok {
			return p.raw[i], true
		}
	}
	return "", false
}

// Raw returns the patterns as written. Config validation compares them and
// `redfirst explain` prints them; matching goes through Match.
func (p PatternSet) Raw() []string { return slices.Clone(p.raw) }

// UnmarshalTOML compiles the patterns at decode time, so a bad glob surfaces
// as a config error instead of a gate that quietly matches nothing.
func (p *PatternSet) UnmarshalTOML(v any) error {
	raw, err := tomlStrings(v, "glob pattern list")
	if err != nil {
		return err
	}
	set, err := NewPatternSet(raw)
	if err != nil {
		return err
	}
	*p = set
	return nil
}

// RegexSet is a compiled list of regular expressions, compiled at config load
// for the same reason as PatternSet: the gates run it over every added line.
type RegexSet struct {
	raw   []string
	exprs []*regexp.Regexp
}

// NewRegexSet compiles raw expressions.
func NewRegexSet(raw []string) (RegexSet, error) {
	set := RegexSet{raw: slices.Clone(raw), exprs: make([]*regexp.Regexp, 0, len(raw))}
	for _, r := range raw {
		re, err := regexp.Compile(r)
		if err != nil {
			return RegexSet{}, fmt.Errorf("%w: invalid regular expression %q: %w", ErrConfig, r, err)
		}
		set.exprs = append(set.exprs, re)
	}
	return set, nil
}

// Match returns the first pattern that matches the line. The pattern reaches
// the report, so the reviewer sees which rule fired.
func (r RegexSet) Match(line string) (string, bool) {
	for i, re := range r.exprs {
		if re.MatchString(line) {
			return r.raw[i], true
		}
	}
	return "", false
}

// Raw returns the expressions as written.
func (r RegexSet) Raw() []string { return slices.Clone(r.raw) }

// UnmarshalTOML compiles the expressions at decode time.
func (r *RegexSet) UnmarshalTOML(v any) error {
	raw, err := tomlStrings(v, "regular expression list")
	if err != nil {
		return err
	}
	set, err := NewRegexSet(raw)
	if err != nil {
		return err
	}
	*r = set
	return nil
}

func tomlStrings(v any, what string) ([]string, error) {
	items, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%w: expected a %s, got %T", ErrConfig, what, v)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("%w: expected a %s, got %T inside it", ErrConfig, what, item)
		}
		out = append(out, s)
	}
	return out, nil
}
