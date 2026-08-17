package testkit

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// updateGolden is the -update flag from the spec. Golden files change through
// it and never by hand.
var updateGolden = flag.Bool("update", false, "rewrite golden files instead of comparing against them")

// Golden compares got against testdata/golden/<name> of the calling package.
func Golden(t *testing.T, name string, got []byte) {
	t.Helper()

	path := filepath.Join("testdata", "golden", name)
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create golden directory: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run `go test -update` to create it)", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("golden %s does not match\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}
