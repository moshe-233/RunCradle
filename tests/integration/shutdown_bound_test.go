package integration

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestW2DefaultShutdownBudgetReapsManagerAndRetainsCancellation(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "job.sh"), []byte("#!/bin/sh\ntrap '' TERM\necho $$ > node.pid\nexec sleep 30\n"), 0700)
	os.WriteFile(filepath.Join(dir, "pc.yaml"), []byte("processes: {a: {entrypoint: [/bin/sh, job.sh]}}"), 0600)
	wrapper := filepath.Join(dir, "engine")
	os.WriteFile(wrapper, []byte(fmt.Sprintf("#!/bin/sh\necho $$ > '%s'\nexec '%s' \"$@\"\n", filepath.Join(dir, "manager.pid"), engine(t))), 0700)
	args := []string{"workflow", "run", "--workspace", dir, "--file", "pc.yaml", "--target", "a", "--request-id", "long-stop", "--process-compose", wrapper, "--timeout-ms", "20000"}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, w3RunArgs(t, args)...)
	var out bytes.Buffer
	cmd.Stdout = &out
	if e := cmd.Start(); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(5 * time.Second)
	var raw []byte
	var pid int
	for {
		var e error
		raw, e = os.ReadFile(filepath.Join(dir, "node.pid"))
		if e == nil {
			pid, e = strconv.Atoi(strings.TrimSpace(string(raw)))
		}
		if e == nil && pid > 1 {
			break
		}
		if time.Now().After(deadline) {
			cmd.Process.Kill()
			cmd.Wait()
			t.Fatal("node did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	group, e := syscall.Getpgid(pid)
	if e != nil || group <= 1 || group == syscall.Getpgrp() {
		t.Fatal(e)
	}
	defer syscall.Kill(-group, syscall.SIGKILL) // only this test's observed group
	raw, e = os.ReadFile(filepath.Join(dir, "manager.pid"))
	if e != nil {
		t.Fatal(e)
	}
	manager, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	defer syscall.Kill(manager, syscall.SIGKILL) // direct manager fixture only
	start := time.Now()
	if e = cmd.Process.Signal(syscall.SIGTERM); e != nil {
		t.Fatal(e)
	}
	_ = cmd.Wait()
	if ctx.Err() != nil || time.Since(start) > 11*time.Second || cmd.ProcessState.ExitCode() != 143 {
		t.Fatalf("cancel budget: %v %s", cmd.ProcessState, out.String())
	}
	stat, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", manager))
	if e == nil {
		fields := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))
		if len(fields) == 0 || fields[0] != "Z" {
			t.Fatalf("owned manager survived CLI: %s", stat)
		}
	}
	if !strings.Contains(out.String(), `"cleanup":"unknown"`) || !strings.Contains(out.String(), `"residual_path":`) {
		t.Fatalf("lost residual facts: %s", out.String())
	}
	stored, code := cliCall(t, "workflow", "inspect", "--workspace", dir, "--request-id", "long-stop")
	if code != 0 || !strings.Contains(string(stored), `"cancellation_code":143`) || !strings.Contains(string(stored), `"reason":"cancelled"`) {
		t.Fatalf("lost persisted cancellation: %d %s", code, stored)
	}
	retried, code := cliCall(t, args...)
	if code != 143 || !strings.Contains(string(retried), `"cancellation_code":143`) {
		t.Fatalf("retry changed cancellation: %d %s", code, retried)
	}
}
