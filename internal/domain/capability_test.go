package domain_test

import (
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
)

func TestCapability_EveryMissingReasonNamesTheGap(t *testing.T) {
	t.Parallel()

	cases := []struct {
		cap    domain.Capability
		name   string
		reason string
	}{
		{domain.CapDiff, "diff", "no diff"},
		{domain.CapHooks, "hooks", "no hooks"},
		{domain.CapTestFiles, "test-files", "no test files in diff"},
		{domain.CapCaseNames, "case-names", "no per-case test names"},
		{domain.CapStackTrace, "stack-trace", "no stack trace provided"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			if got := c.cap.String(); got != c.name {
				t.Errorf("String() = %q, want %q", got, c.name)
			}
			if got := c.cap.MissingReason(); got != c.reason {
				t.Errorf("MissingReason() = %q, want %q", got, c.reason)
			}
		})
	}
}

func TestCapability_OnlyHooksAdvertisesTheNextTier(t *testing.T) {
	t.Parallel()

	if got := domain.CapHooks.MissingHint(); !strings.Contains(got, ".redfirst/") {
		t.Errorf("CapHooks.MissingHint() = %q, want the path that removes the N/A line", got)
	}
	for _, c := range []domain.Capability{domain.CapDiff, domain.CapTestFiles, domain.CapCaseNames, domain.CapStackTrace} {
		if got := c.MissingHint(); got != "" {
			t.Errorf("%v.MissingHint() = %q, want empty: the caller cannot act on it", c, got)
		}
	}
}
