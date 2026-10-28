package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	pc "runcradle/internal/engine/processcompose"
	"runcradle/internal/workflow"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Break caught: returning the wrapper/engine code instead of durably recording
// the actual child's exit, including a child that exits before observation.
func TestW2DirectFastExitReceipt(t *testing.T) {
	for _, code := range []int{0, 7, 8} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Chmod(dir, 0700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "_exec", "--receipt-dir", dir, "--run-id", "test-run", "--node-id", "fast", "--", "/bin/sh", "-c", fmt.Sprintf("exit %d", code))
			_ = cmd.Run()
			if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != code {
				st, _ := os.Stat(dir)
				entries, _ := os.ReadDir(dir)
				t.Fatalf("actual helper exit: %v, want %d; dir=%v entries=%v", cmd.ProcessState, code, st, entries)
			}
			files, err := filepath.Glob(filepath.Join(dir, "*.json"))
			if err != nil || len(files) != 1 {
				t.Fatalf("durable direct receipt missing: %v %v", files, err)
			}
			var receipt struct {
				RunID      string `json:"run_id"`
				NodeID     string `json:"node_id"`
				Invocation string `json:"incarnation"`
				Final      bool   `json:"final"`
				Started    bool   `json:"started"`
				ExitCode   *int   `json:"exit_code"`
			}
			raw, err := os.ReadFile(files[0])
			if err != nil || json.Unmarshal(raw, &receipt) != nil || !receipt.Final || !receipt.Started || receipt.RunID != "test-run" || receipt.NodeID != "fast" || receipt.Invocation == "" || receipt.ExitCode == nil || *receipt.ExitCode != code {
				t.Fatalf("wrong direct receipt: %s %v", raw, err)
			}
		})
	}
}

// Native control and wrapped input differ only in the executable bridge.
func TestW2RealEarlyExitAndArguments(t *testing.T) {
	binary := engine(t)
	for _, code := range []int{0, 7, 8} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Chmod(dir, 0700); err != nil {
				t.Fatal(err)
			}
			args := []string{"/bin/sh", "-c", fmt.Sprintf("exit %d", code)}
			config := map[string]any{"version": "0.5", "processes": map[string]any{"a": map[string]any{"entrypoint": args, "availability": map[string]any{"exit_on_end": true}}}}
			raw, _ := json.Marshal(config)
			file := filepath.Join(dir, "compose.yaml")
			os.WriteFile(file, raw, 0600)
			d, err := workflow.Load(dir, "compose.yaml", "")
			if err != nil {
				t.Fatal(err)
			}
			raw, err = pc.Prepare(d, executable, "gate", dir)
			if err != nil {
				t.Fatal(err)
			}
			os.WriteFile(file, raw, 0600)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binary, "up", "-f", file, "-t=false", "--no-server", "--no-watch", "--disable-dotenv", "--log-file", "/dev/null")
			cmd.Dir = dir
			cmd.Env = []string{"PATH=/usr/bin:/bin", "TERM=dumb", "XDG_CONFIG_HOME=" + dir}
			cmd.WaitDelay = 100 * time.Millisecond
			_ = cmd.Run()
			if ctx.Err() != nil {
				t.Fatal("native early exit hung")
			}
			entries, _ := filepath.Glob(filepath.Join(dir, "*.json"))
			if len(entries) != 1 {
				t.Fatalf("missing receipt after upstream exit %v", cmd.ProcessState)
			}
			var r struct {
				Final bool `json:"final"`
				Exit  *int `json:"exit_code"`
			}
			b, _ := os.ReadFile(entries[0])
			json.Unmarshal(b, &r)
			if !r.Final || r.Exit == nil || *r.Exit != code {
				t.Fatalf("actual exit lost: %s", b)
			}
		})
	}
}

func TestW2HelperPreservesArgumentsCwdAndClosedStdin(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	for _, wrapped := range []bool{false, true} {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		args := []string{"/usr/bin/python3", "-c", "import json,os,sys; print(json.dumps([os.getcwd(),sys.argv[1:],sys.stdin.read()]))", "literal spaces", "'quoted'", "dollar$sign", ""}
		if wrapped {
			args = append([]string{executable, "_exec", "--receipt-dir", dir, "--run-id", "parity", "--node-id", "a", "--"}, args...)
		}
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.Output()
		cancel()
		if err != nil {
			t.Fatalf("wrapped=%v: %v %s", wrapped, err, out)
		}
		want, _ := json.Marshal([]any{dir, []string{"literal spaces", "'quoted'", "dollar$sign", ""}, ""})
		var normalized any
		if json.Unmarshal(out, &normalized) != nil {
			t.Fatalf("invalid child output: %s", out)
		}
		got, _ := json.Marshal(normalized)
		if !bytes.Equal(got, want) {
			t.Fatalf("parity: %s != %s", got, want)
		}
	}
}

func TestW2HelperGroupSignalIsRecorded(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	marker := filepath.Join(dir, "ready")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "_exec", "--receipt-dir", dir, "--run-id", "signal", "--node-id", "a", "--", "/bin/sh", "-c", "printf ready > '"+marker+"'; exec sleep 30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill() }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, e := os.Stat(marker); e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child not ready")
		}
		time.Sleep(time.Millisecond * 5)
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if cmd.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGTERM {
		t.Fatalf("signal not preserved: %v", cmd.ProcessState)
	}
	entries, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(entries) != 1 {
		t.Fatal("receipt missing")
	}
	raw, _ := os.ReadFile(entries[0])
	if !strings.Contains(string(raw), `"exit_code":143`) {
		t.Fatalf("signal receipt lost: %s", raw)
	}
}

func TestW2ReceiptFailureCannotChangeNativeChildExit(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	defer os.Chmod(dir, 0700)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "_exec", "--receipt-dir", dir, "--run-id", "io-failure", "--node-id", "a", "--", "/usr/bin/python3", "-c", "import os,sys; os.chmod(sys.argv[1],0o500); sys.exit(7)", dir)
	_ = cmd.Run()
	if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 7 {
		t.Fatalf("receipt IO replaced actual child result: %v", cmd.ProcessState)
	}
}
