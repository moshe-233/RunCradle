package integration

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestW2SIGPIPEDispositionPreservesNativeFailure(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	script := filepath.Join(dir, "pipe.sh")
	os.WriteFile(script, []byte("#!/bin/sh\nkill -PIPE $$\nexit 0\n"), 0600)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	direct := exec.CommandContext(ctx, "/bin/sh", script)
	_ = direct.Run()
	if direct.ProcessState == nil || direct.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGPIPE {
		t.Fatalf("direct control did not receive SIGPIPE: %v", direct.ProcessState)
	}
	helper := exec.CommandContext(ctx, executable, "_exec", "--receipt-dir", dir, "--run-id", "pipe", "--node-id", "a", "--", "/bin/sh", script)
	_ = helper.Run()
	if helper.ProcessState == nil || helper.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGPIPE {
		t.Fatalf("helper changed SIGPIPE result: %v", helper.ProcessState)
	}
	raw, _ := json.Marshal(map[string]any{"processes": map[string]any{"a": map[string]any{"entrypoint": []string{"/bin/sh", script}, "availability": map[string]any{"exit_on_end": true}}}})
	file := filepath.Join(dir, "pc.json")
	os.WriteFile(file, raw, 0600)
	native := exec.CommandContext(ctx, engine(t), "--log-file", "/dev/null", "up", "--config", file, "--tui=false", "--no-server", "--disable-dotenv")
	native.Dir = dir
	_ = native.Run()
	if native.ProcessState == nil || native.ProcessState.ExitCode() == 0 {
		t.Fatalf("native control did not fail: %v", native.ProcessState)
	}
	out, code := cliCall(t, "workflow", "run", "--workspace", dir, "--file", "pc.json", "--target", "a", "--process-compose", engine(t))
	if code != 1 || !strings.Contains(string(out), `"exit_code":141`) {
		t.Fatalf("workflow hid real SIGPIPE failure: %d %s", code, out)
	}
}
