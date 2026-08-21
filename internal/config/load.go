package config

import (
	"context"
	"errors"
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
		// git reports a missing or unfetchable blob only once the stream is
		// running, so a read failure arrives here already carrying ErrHarness.
		// Calling that an invalid config would tell the orchestrator a retry is
		// pointless when the repository, not the file, is what broke.
		if errors.Is(err, domain.ErrHarness) {
			return domain.Config{}, "", fmt.Errorf("reading %s from %s: %w", path, ref, err)
		}
		return domain.Config{}, "", fmt.Errorf("%w: %s in %s: %w", domain.ErrConfig, path, ref, err)
	}
	if err := validate(cfg, md); err != nil {
		return domain.Config{}, "", fmt.Errorf("%s in %s: %w", path, ref, err)
	}
	resolveRedGreenEnabled(&cfg)
	return cfg, "base:" + path, nil
}

// resolveRedGreenEnabled folds the one switch the spec gives red-green of its
// own into the list every gate answers to.
//
// The fold happens here rather than in Config.GateDisabled because false is the
// zero value of the field: reading it at judgement time would let a Config
// nobody loaded switch the gate off in silence. Here the value can only have
// come from a file somebody wrote, and `redfirst explain` prints the result
// through the same list as any other disabled gate.
func resolveRedGreenEnabled(cfg *domain.Config) {
	if cfg.RedGreen.Enabled || cfg.GateDisabled(domain.GateRedGreen) {
		return
	}
	cfg.Gates.Disabled = append(cfg.Gates.Disabled, domain.GateRedGreen)
}

// DowngradeCasesWithoutHooks lowers every cases mode to append-only and names
// the config keys it changed. The caller is the runner, which is the only place
// that knows whether hooks exist; append-only forbids more than cases does, so
// the downgrade moves toward strictness.
//
// tests.fixtures_immutability travels with tests.immutability. Left at cases it
// would take the whole gate to unavailable for want of case names, and a config
// key that switches a check off is the weakening this downgrade exists to
// avoid.
func DowngradeCasesWithoutHooks(cfg *domain.Config) []string {
	keys := []struct {
		name string
		mode *domain.Immutability
	}{
		{"tests.immutability", &cfg.Tests.Immutability},
		{"tests.fixtures_immutability", &cfg.Tests.FixturesImmutability},
	}

	var downgraded []string
	for _, k := range keys {
		if *k.mode != domain.ImmutabilityCases {
			continue
		}
		*k.mode = domain.ImmutabilityAppendOnly
		downgraded = append(downgraded, k.name)
	}
	return downgraded
}
