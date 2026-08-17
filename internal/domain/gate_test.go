package domain_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// TestGateResult_WarningsStayOutOfTheGateLine holds the shape of the contract:
// a warning reaches the orchestrator once, from the top-level block, so a gate
// line never repeats it.
func TestGateResult_WarningsStayOutOfTheGateLine(t *testing.T) {
	t.Parallel()

	res := domain.GateResult{
		ID:     domain.GateProtectedPaths,
		Status: domain.StatusPass,
		Warnings: []domain.Warning{
			{Kind: domain.WarnGuardedPaths, File: "pnpm-lock.yaml"},
		},
	}

	encoded, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal gate result: %v", err)
	}
	if strings.Contains(string(encoded), "pnpm-lock.yaml") {
		t.Errorf("a gate line carries its warnings into the report contract: %s", encoded)
	}
}
