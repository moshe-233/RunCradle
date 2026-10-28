package integration

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestW2SignalReplayDoesNotExecuteExportedShellFunctions(t *testing.T) {
	t.Setenv("BASH_FUNC_kill%%", "() { return 0; }")
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// The business program raises TERM without invoking any shell function.
	cmd := exec.CommandContext(ctx, executable, "_exec", "--receipt-dir", dir, "--run-id", "signal-env", "--node-id", "a", "--", "python3", "-c", "import os,signal; os.kill(os.getpid(), signal.SIGTERM)")
	_ = cmd.Run()
	if cmd.ProcessState == nil || cmd.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGTERM {
		t.Fatalf("exported function changed replayed wait status: %v", cmd.ProcessState)
	}
}
