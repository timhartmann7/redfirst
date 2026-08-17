package domain_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
)

func TestExitCode_HarnessFailureIsTwoNotOne(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("env-up.sh exited 1: %w", domain.ErrHarness)
	if got := domain.ExitCode(err); got != 2 {
		t.Errorf("ExitCode(harness failure) = %d, want 2: a broken harness is not the agent's fault", got)
	}
}

func TestExitCode_MapsEverySentinelToItsCode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want int
	}{
		{"success", nil, 0},
		{"gate violation", domain.ErrGateViolation, 1},
		{"harness failure", domain.ErrHarness, 2},
		{"invalid config", domain.ErrConfig, 3},
		{"internal error", domain.ErrInternal, 4},
		{"no legal move", domain.ErrHumanRequired, 5},
		{"unrecognised error is ours", errors.New("boom"), 4},
		{"wrapped sentinel", fmt.Errorf("while loading: %w", domain.ErrConfig), 3},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			if got := domain.ExitCode(c.err); got != c.want {
				t.Errorf("ExitCode(%v) = %d, want %d", c.err, got, c.want)
			}
		})
	}
}

func TestExitCode_HumanRequiredOutranksGateViolation(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("%w: %w", domain.ErrGateViolation, domain.ErrHumanRequired)
	if got := domain.ExitCode(err); got != 5 {
		t.Errorf("ExitCode = %d, want 5: a retry is pointless whatever else failed", got)
	}
}
