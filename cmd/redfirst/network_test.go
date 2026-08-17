package main

import (
	"os/exec"
	"strings"
	"testing"
)

// TestRedfirst_LinksNoNetworkPackage holds "zero network calls at runtime" at
// the only place a deterministic test can reach it: the import graph of the
// binary. A package that cannot be linked cannot dial.
func TestRedfirst_LinksNoNetworkPackage(t *testing.T) {
	t.Parallel()

	out, err := exec.CommandContext(t.Context(), "go", "list", "-deps", ".").Output()
	if err != nil {
		t.Fatalf("go list -deps: %v", err)
	}

	forbidden := map[string]struct{}{
		"net":        {},
		"net/http":   {},
		"net/rpc":    {},
		"net/smtp":   {},
		"crypto/tls": {},
	}
	for _, pkg := range strings.Fields(string(out)) {
		if _, bad := forbidden[pkg]; bad {
			t.Errorf("the binary links %s; redfirst makes no network call at runtime", pkg)
		}
	}
}
