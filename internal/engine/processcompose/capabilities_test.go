package processcompose

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCapabilitiesDoNotClaimAnUnavailableEngine(t *testing.T) {
	dir := t.TempDir()
	for _, path := range []string{filepath.Join(dir, "missing"), filepath.Join(dir, "wrong-version")} {
		if filepath.Base(path) == "wrong-version" {
			os.WriteFile(path, []byte("#!/bin/sh\necho v0.0.0\n"), 0700)
		}
		if _, e := New(path, "/bin/true").Capabilities(context.Background()); e == nil {
			t.Fatal("unavailable engine advertised executable capabilities")
		}
	}
}
