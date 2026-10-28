package workflow

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestWorkspaceReadBoundaries(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ok.yaml"), []byte("processes: {}"), 0600)
	os.WriteFile(filepath.Join(outside, "secret"), []byte("secret"), 0600)
	os.Symlink(filepath.Join(outside, "secret"), filepath.Join(dir, "escape.yaml"))
	syscall.Mkfifo(filepath.Join(dir, "pipe.yaml"), 0600)
	for _, name := range []string{"../outside", filepath.Join(outside, "secret"), "escape.yaml", "pipe.yaml", "."} {
		if _, err := Load(dir, name, ""); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	d, err := Load(dir, "ok.yaml", "")
	if err != nil || !d.Valid() {
		t.Fatalf("failed regular input: %v %+v", err, d.Issues)
	}
}
func TestLoadRevisionIncludesPathsAndOptionalUI(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.yaml", "b.yaml"} {
		os.WriteFile(filepath.Join(dir, name), []byte("processes: {}"), 0600)
	}
	a, _ := Load(dir, "a.yaml", "")
	b, _ := Load(dir, "b.yaml", "")
	if a.Revision == b.Revision {
		t.Fatal("path not in revision")
	}
	os.WriteFile(filepath.Join(dir, "empty.json"), nil, 0600)
	d, err := Load(dir, "a.yaml", "empty.json")
	if err != nil || d.Valid() {
		t.Fatal("empty explicit UI treated as absent")
	}
}
