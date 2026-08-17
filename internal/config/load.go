package config

import (
	"context"
	"fmt"
	"io"

	"github.com/BurntSushi/toml"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// FileSource is the slice of git the loader needs. internal/gitx satisfies it.
type FileSource interface {
	Exists(ctx context.Context, ref, path string) (bool, error)
	ShowFile(ctx context.Context, ref, path string) (io.ReadCloser, error)
}

// Load reads the config from the base ref. It returns the effective config and
// its source ("defaults" or "base:<path>"). A missing file is not an error:
// tiers 0 and 1 run without one by design, and only a file that exists and is
// broken is a refusal.
//
// The decoder writes over a value Defaults() has already filled, which is what
// makes the unit of replacement the key rather than the section: a key left
// out keeps its default, a key you set replaces it whole, and lists do not
// merge.
func Load(ctx context.Context, src FileSource, ref, path string) (domain.Config, string, error) {
	cfg := Defaults()

	exists, err := src.Exists(ctx, ref, path)
	if err != nil {
		return domain.Config{}, "", fmt.Errorf("%w: looking for %s in %s: %w", domain.ErrHarness, path, ref, err)
	}
	if !exists {
		return cfg, SourceDefaults, nil
	}

	r, err := src.ShowFile(ctx, ref, path)
	if err != nil {
		return domain.Config{}, "", fmt.Errorf("%w: reading %s from %s: %w", domain.ErrHarness, path, ref, err)
	}
	defer func() { _ = r.Close() }()

	md, err := toml.NewDecoder(r).Decode(&cfg)
	if err != nil {
		return domain.Config{}, "", fmt.Errorf("%w: %s in %s: %w", domain.ErrConfig, path, ref, err)
	}
	if err := validate(cfg, md); err != nil {
		return domain.Config{}, "", fmt.Errorf("%s in %s: %w", path, ref, err)
	}
	return cfg, "base:" + path, nil
}

// DowngradeCasesWithoutHooks lowers tests.immutability from cases to
// append-only and reports whether it did. The caller is the runner, which is
// the only place that knows whether hooks exist; append-only forbids more than
// cases does, so the downgrade moves toward strictness.
func DowngradeCasesWithoutHooks(cfg *domain.Config) bool {
	if cfg.Tests.Immutability != domain.ImmutabilityCases {
		return false
	}
	cfg.Tests.Immutability = domain.ImmutabilityAppendOnly
	return true
}
