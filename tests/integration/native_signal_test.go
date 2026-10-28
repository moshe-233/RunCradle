package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestW2SignaledNodeCannotBecomeAllowedNormalExit(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "selfterm.sh"), []byte("#!/bin/sh\nkill -TERM $$\n"), 0600)
	config := `processes:
  source:
    entrypoint: [/bin/sh, selfterm.sh]
    success_exit_codes: [143]
  check:
    command: touch target-ran
    depends_on: {source: {condition: process_completed_successfully}}
    availability: {exit_on_end: true, exit_on_skipped: true}
`
	file := filepath.Join(dir, "pc.yaml")
	os.WriteFile(file, []byte(config), 0600)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	native := exec.CommandContext(ctx, engine(t), "--log-file", "/dev/null", "up", "--config", file, "--tui=false", "--no-server", "--disable-dotenv")
	native.Dir = dir
	_ = native.Run()
	if native.ProcessState == nil || native.ProcessState.ExitCode() == 0 {
		t.Fatal("native signal control unexpectedly passed")
	}
	if _, e := os.Stat(filepath.Join(dir, "target-ran")); !os.IsNotExist(e) {
		t.Fatal("native dependent ran")
	}
	out, code := cliCall(t, "workflow", "run", "--workspace", dir, "--file", "pc.yaml", "--target", "check", "--process-compose", engine(t))
	if code == 0 {
		t.Fatalf("signal became allowed exit143: %s", out)
	}
	if _, e := os.Stat(filepath.Join(dir, "target-ran")); !os.IsNotExist(e) {
		t.Fatal("wrapper changed native signal dependency")
	}
	// A normal exit143 is different and remains explicitly allowed.
	os.WriteFile(filepath.Join(dir, "selfterm.sh"), []byte("#!/bin/sh\nexit 143\n"), 0600)
	out, code = cliCall(t, "workflow", "run", "--workspace", dir, "--file", "pc.yaml", "--target", "check", "--process-compose", engine(t))
	if code != 0 {
		t.Fatalf("normal allowed exit143 was confused with signal: %d %s", code, out)
	}
}
