package config

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// validate runs every row of the validation table from section 6 of the spec.
// Bad globs, bad regexes and unknown enum values never reach here: the domain
// types reject them while decoding.
func validate(cfg domain.Config, md toml.MetaData) error {
	if err := checkUnknownKeys(md); err != nil {
		return err
	}
	if cfg.Version != 1 {
		return fmt.Errorf("%w: version = %d, this binary reads version = 1", domain.ErrConfig, cfg.Version)
	}
	if len(cfg.Scope) > 0 {
		return fmt.Errorf("%w: scopes are not implemented yet", domain.ErrConfig)
	}
	if err := checkSeverity(cfg.Severity); err != nil {
		return err
	}
	if err := checkDisabled(cfg.Gates.Disabled); err != nil {
		return err
	}
	if err := checkPaths(cfg.Paths); err != nil {
		return err
	}
	if cfg.Tests.Immutability == domain.ImmutabilityWarn {
		return fmt.Errorf("%w: tests.immutability = %q applies to tests.fixtures_immutability only",
			domain.ErrConfig, domain.ImmutabilityWarn)
	}
	// A zero here would make red-green vacuous, and no config may weaken a check.
	if cfg.RedGreen.ProbeRuns < 1 {
		return fmt.Errorf("%w: red_green.probe_runs = %d, a probe has to run at least once",
			domain.ErrConfig, cfg.RedGreen.ProbeRuns)
	}
	return nil
}

// checkUnknownKeys names every offending key at once. A config with
// `protected_paths` instead of `protected` must not turn quietly into an empty
// denylist, and a reader fixing one typo at a time learns that too slowly.
func checkUnknownKeys(md toml.MetaData) error {
	undecoded := md.Undecoded()
	if len(undecoded) == 0 {
		return nil
	}
	keys := make([]string, 0, len(undecoded))
	for _, k := range undecoded {
		keys = append(keys, k.String())
	}
	slices.Sort(keys)
	return fmt.Errorf("%w: unknown key(s): %s", domain.ErrConfig, strings.Join(keys, ", "))
}

// checkSeverity guards the two gates that carry the product's claim, and the
// gate names besides: severity is a map, so the decoder merges it per key and
// a misspelt gate never shows up as an undecoded key.
func checkSeverity(severity map[domain.GateID]domain.Severity) error {
	for _, id := range slices.Sorted(maps.Keys(severity)) {
		if !slices.Contains(domain.AllGateIDs(), id) {
			return fmt.Errorf("%w: severity names an unknown gate %q", domain.ErrConfig, id)
		}
		lowered := severity[id] == domain.SeverityWarn
		if lowered && (id == domain.GateRedGreen || id == domain.GateSuiteGreen) {
			return fmt.Errorf("%w: severity.%s = %q is forbidden, these two gates block or nothing does",
				domain.ErrConfig, id, domain.SeverityWarn)
		}
	}
	return nil
}

func checkDisabled(disabled []domain.GateID) error {
	for _, id := range disabled {
		if !slices.Contains(domain.AllGateIDs(), id) {
			return fmt.Errorf("%w: gates.disabled names an unknown gate %q", domain.ErrConfig, id)
		}
	}
	return nil
}

// checkPaths rejects a pattern that sits in both lists rather than resolving it
// silently in someone's favour.
func checkPaths(paths domain.Paths) error {
	guarded := paths.Guarded.Raw()
	for _, pattern := range paths.Protected.Raw() {
		if slices.Contains(guarded, pattern) {
			return fmt.Errorf("%w: pattern %q sits in both paths.protected and paths.guarded",
				domain.ErrConfig, pattern)
		}
	}
	return nil
}
