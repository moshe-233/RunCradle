package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func cliCall(t *testing.T, args ...string) ([]byte, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, w3RunArgs(t, args)...)
	out, e := cmd.Output()
	code := 0
	if e != nil {
		if cmd.ProcessState == nil {
			t.Fatal(e)
		}
		code = cmd.ProcessState.ExitCode()
	}
	if ctx.Err() != nil {
		t.Fatal("CLI deadline exceeded")
	}
	if len(out) > 4096 || !json.Valid(out) {
		t.Fatalf("unbounded/invalid result: %d bytes, %s", len(out), out)
	}
	return out, code
}

func TestW2DeadlineAndIgnoredTermRemainBounded(t *testing.T) {
	for _, ms := range []string{"1", "2500"} {
		t.Run(ms, func(t *testing.T) {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "pc.yaml"), []byte("processes: {a: {command: \"trap '' TERM; exec sleep 30\", shutdown: {timeout_seconds: 1}}}"), 0600)
			out, code := cliCall(t, "workflow", "run", "--workspace", dir, "--file", "pc.yaml", "--target", "a", "--process-compose", engine(t), "--timeout-ms", ms)
			if code != 124 {
				t.Fatalf("deadline mislabeled %d %s", code, out)
			}
		})
	}
}
func TestW2ForeignListenerUntouched(t *testing.T) {
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	dir := t.TempDir()
	script := fmt.Sprintf("import socket,sys\ns=socket.socket()\ntry: s.bind(('127.0.0.1',%d))\nexcept OSError: sys.exit(8)\nsys.exit(0)", port)
	raw, _ := json.Marshal(map[string]any{"processes": map[string]any{"a": map[string]any{"entrypoint": []string{"python3", "-c", script}, "availability": map[string]any{"exit_on_end": true}}}})
	os.WriteFile(filepath.Join(dir, "pc.json"), raw, 0600)
	out, code := cliCall(t, "workflow", "run", "--workspace", dir, "--file", "pc.json", "--target", "a", "--process-compose", engine(t))
	if code != 1 {
		t.Fatalf("bind failure lost %d %s", code, out)
	}
	go func() {
		c, e := listener.Accept()
		if e == nil {
			c.Write([]byte("alive"))
			c.Close()
		}
	}()
	c, e := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if e != nil {
		t.Fatal("foreign listener was stopped", e)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(time.Second))
	b, e := io.ReadAll(c)
	if e != nil || string(b) != "alive" {
		t.Fatalf("foreign listener altered %q %v", b, e)
	}
}
func TestW2ClosedAndBlockedOutputKeepStoredTaskResult(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(fmt.Sprint(blocked), func(t *testing.T) {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "pc.yaml"), []byte("processes: {a: {command: 'touch ready; sleep 0.3; exit 7'}}"), 0600)
			r, w, e := os.Pipe()
			if e != nil {
				t.Fatal(e)
			}
			defer r.Close()
			defer w.Close()
			if blocked {
				conn, _ := w.SyscallConn()
				var fillErr error
				conn.Control(func(fd uintptr) {
					flags, e := unix.FcntlInt(fd, unix.F_GETFL, 0)
					if e != nil {
						fillErr = e
						return
					}
					if _, e = unix.FcntlInt(fd, unix.F_SETFL, flags|unix.O_NONBLOCK); e != nil {
						fillErr = e
						return
					}
					for {
						_, e = unix.Write(int(fd), make([]byte, 4096))
						if e == unix.EAGAIN {
							break
						}
						if e != nil {
							fillErr = e
							break
						}
					}
				})
				if fillErr != nil {
					t.Fatal(fillErr)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, w3RunArgs(t, []string{"workflow", "run", "--workspace", dir, "--file", "pc.yaml", "--target", "a", "--request-id", "delivery", "--process-compose", engine(t), "--timeout-ms", "5000"})...)
			cmd.Stdout = w
			if e = cmd.Start(); e != nil {
				t.Fatal(e)
			}
			deadline := time.Now().Add(5 * time.Second)
			for {
				if _, e = os.Stat(filepath.Join(dir, "ready")); e == nil {
					break
				}
				if time.Now().After(deadline) {
					cmd.Process.Kill()
					cmd.Wait()
					t.Fatal("node not started")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !blocked {
				r.Close()
			}
			_ = cmd.Wait()
			if cmd.ProcessState.ExitCode() != 125 || ctx.Err() != nil {
				t.Fatalf("delivery not bounded: %v", cmd.ProcessState)
			}
			out, code := cliCall(t, "workflow", "inspect", "--workspace", dir, "--request-id", "delivery", "--node", "a")
			if code != 0 || !strings.Contains(string(out), `"exit_code":7`) {
				t.Fatalf("delivery erased real exit %d %s", code, out)
			}
		})
	}
}

func TestW2ActualCancellationAndRetryKeepSignalResult(t *testing.T) {
	for _, signal := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(signal.String(), func(t *testing.T) {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "pc.yaml"), []byte("processes: {a: {command: 'touch ready; exec sleep 30', shutdown: {timeout_seconds: 1}}}"), 0600)
			args := []string{"workflow", "run", "--workspace", dir, "--file", "pc.yaml", "--target", "a", "--request-id", "cancel", "--process-compose", engine(t), "--timeout-ms", "10000"}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, w3RunArgs(t, args)...)
			var out bytes.Buffer
			cmd.Stdout = &out
			if e := cmd.Start(); e != nil {
				t.Fatal(e)
			}
			deadline := time.Now().Add(5 * time.Second)
			for {
				if _, e := os.Stat(filepath.Join(dir, "ready")); e == nil {
					break
				}
				if time.Now().After(deadline) {
					cmd.Process.Kill()
					cmd.Wait()
					t.Fatalf("node never ready: %s", out.String())
				}
				time.Sleep(10 * time.Millisecond)
			}
			if e := cmd.Process.Signal(signal); e != nil {
				t.Fatal(e)
			}
			_ = cmd.Wait()
			if cmd.ProcessState.ExitCode() != 128+int(signal) {
				t.Fatalf("cancel code: %v %s", cmd.ProcessState, out.String())
			}
			_, again := cliCall(t, args...)
			if again != 128+int(signal) {
				t.Fatalf("retry lost recorded cancellation: %d", again)
			}
		})
	}
}
func TestW2CorruptHistoricalNodeDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "pc.yaml"), []byte("processes: {a: {entrypoint: [/bin/true], availability: {exit_on_end: true}}}"), 0600)
	out, code := cliCall(t, "workflow", "run", "--workspace", dir, "--file", "pc.yaml", "--target", "a", "--process-compose", engine(t))
	if code != 0 {
		t.Fatalf("fixture run %d %s", code, out)
	}
	var summary map[string]any
	json.Unmarshal(out, &summary)
	id := summary["run_id"].(string)
	path := filepath.Join(dir, ".runcradle/runs", id, "snapshot.json")
	raw, _ := os.ReadFile(path)
	var record map[string]any
	json.Unmarshal(raw, &record)
	snapshot := record["snapshot"].(map[string]any)
	snapshot["nodes"].([]any)[0].(map[string]any)["name"] = "unknown-node"
	raw, _ = json.Marshal(record)
	os.WriteFile(path, raw, 0600)
	_, code = cliCall(t, "workflow", "inspect", "--workspace", dir, "--run-id", id, "--node", "unknown-node")
	if code != 125 {
		t.Fatalf("corrupt node accepted: %d", code)
	}
}
func TestW2ActualRunAndOfflineHistory(t *testing.T) {
	for _, code := range []int{0, 7, 8} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			dir := t.TempDir()
			config := fmt.Sprintf("processes:\n  prepare:\n    entrypoint: [/bin/true]\n  check:\n    command: python3 -c \"open('count','a').write('x'); exit(%d)\"\n    depends_on: {prepare: {condition: process_completed_successfully}}\n    availability: {exit_on_end: true}\n", code)
			os.WriteFile(filepath.Join(dir, "pc.yaml"), []byte(config), 0600)
			args := []string{"workflow", "run", "--workspace", dir, "--file", "pc.yaml", "--target", "check", "--request-id", "same", "--process-compose", engine(t), "--timeout-ms", "5000"}
			out, exit := cliCall(t, args...)
			want := 0
			if code != 0 {
				want = 1
			}
			if exit != want {
				t.Fatalf("run exit %d want %d: %s", exit, want, out)
			}
			var record struct {
				RunID      string `json:"run_id"`
				TaskResult string `json:"task_result"`
				Nodes      []struct {
					Name string `json:"name"`
					Exit *int   `json:"exit_code"`
				} `json:"nodes"`
			}
			json.Unmarshal(out, &record)
			if record.RunID == "" {
				t.Fatalf("run_id missing %s", out)
			}
			found := false
			for _, n := range record.Nodes {
				if n.Name == "check" && n.Exit != nil && *n.Exit == code {
					found = true
				}
			}
			if !found {
				t.Fatalf("actual node exit missing: %s", out)
			}
			again, retry := cliCall(t, args...)
			if retry != want || !strings.Contains(string(again), record.RunID) {
				t.Fatalf("retry changed run: %s", again)
			}
			count, _ := os.ReadFile(filepath.Join(dir, "count"))
			if string(count) != "x" {
				t.Fatalf("duplicate execution: %q", count)
			}
			cmd := exec.Command(executable, "workflow", "inspect", "--workspace", dir, "--run-id", record.RunID)
			cmd.Env = []string{"PATH=/nonexistent"}
			history, e := cmd.Output()
			if e != nil || !json.Valid(history) || !strings.Contains(string(history), record.RunID) {
				t.Fatalf("offline history: %v %s", e, history)
			}
		})
	}
}

func TestW2FailureEvidenceOptInAndDefaultNoBody(t *testing.T) {
	for _, retain := range []bool{false, true} {
		t.Run(fmt.Sprint(retain), func(t *testing.T) {
			dir := t.TempDir()
			raw, _ := json.Marshal(map[string]any{"processes": map[string]any{"a": map[string]any{"command": "printf '\\033[31mASSERT_PRIVATE\\033[0m\\n'; exit 7"}}})
			os.WriteFile(filepath.Join(dir, "pc.json"), raw, 0600)
			args := []string{"workflow", "run", "--workspace", dir, "--file", "pc.json", "--target", "a", "--process-compose", engine(t), "--timeout-ms", "5000"}
			if retain {
				args = append(args, "--retain-evidence")
			}
			out, code := cliCall(t, args...)
			if code != 1 {
				t.Fatalf("business failure lost %d %s", code, out)
			}
			var record struct {
				RunID string `json:"run_id"`
			}
			json.Unmarshal(out, &record)
			history, code := cliCall(t, "workflow", "inspect", "--workspace", dir, "--run-id", record.RunID, "--node", "a")
			if code != 0 {
				t.Fatalf("inspect %d %s", code, history)
			}
			var result struct {
				Nodes []struct {
					Diagnostic *struct {
						Status string `json:"status"`
						Text   string `json:"text"`
					} `json:"diagnostic"`
				} `json:"nodes"`
			}
			json.Unmarshal(history, &result)
			if len(result.Nodes) != 1 || result.Nodes[0].Diagnostic == nil {
				t.Fatalf("diagnostic disposition missing %s", history)
			}
			d := result.Nodes[0].Diagnostic
			if retain {
				if d.Status != "available" || !strings.Contains(d.Text, "ASSERT_PRIVATE") || strings.Contains(d.Text, "\x1b") {
					t.Fatalf("retained evidence unavailable/unclean: %s", history)
				}
			} else {
				if d.Status != "not_retained" || d.Text != "" {
					t.Fatalf("default evidence leaked %s", history)
				}
				for _, file := range []string{"snapshot.json", "events.jsonl"} {
					b, _ := os.ReadFile(filepath.Join(dir, ".runcradle/runs", record.RunID, file))
					if strings.Contains(string(b), "ASSERT_PRIVATE") {
						t.Fatalf("raw diagnostic persisted in %s", file)
					}
				}
			}
		})
	}
}
