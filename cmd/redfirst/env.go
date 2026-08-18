package main

import (
	"context"

	"github.com/timhartmann7/redfirst/internal/domain"
	"github.com/timhartmann7/redfirst/internal/harness"
	"github.com/timhartmann7/redfirst/internal/runner"
)

// env opens the project's own harness on demand.
//
// The runner asks for it immediately before the first gate that needs hooks,
// so a diff a static gate already refused never pays for a container. close
// tears down whatever was opened and does nothing where nothing was, which is
// what lets the caller reach it through a plain defer.
type env struct {
	opts    harness.Options
	source  runner.Source
	diff    domain.Diff
	cfg     domain.Config
	session *harness.Session
	probe   *runner.Probe
}

func (e *env) open(ctx context.Context) (domain.Probe, error) {
	s, err := harness.Open(ctx, e.opts)
	if err != nil {
		return nil, err
	}
	e.session = s
	e.probe = runner.NewProbe(s, e.source, e.diff, e.cfg)
	return e.probe, nil
}

// timings is what the run spent inside the project's hooks, and zero where no
// environment came up. The core's own share is what total_s has left over.
func (e *env) timings() domain.Timings {
	if e.probe == nil {
		return domain.Timings{}
	}
	return e.probe.Timings()
}

func (e *env) close(ctx context.Context) error {
	if e.session == nil {
		return nil
	}
	return e.session.Close(ctx)
}

// mode is how the session treated the services, and empty where none came up.
// It reaches the first line of the report: somebody digging into a strange
// refusal has to see it at once rather than guess.
func (e *env) mode() string {
	if e.session == nil {
		return ""
	}
	return string(e.session.Mode())
}
