package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

var executable string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "runcradle-integration-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	executable = filepath.Join(dir, "runcradle")
	command := exec.Command("go", "build", "-buildvcs=false", "-o", executable, "./cmd/runcradle")
	command.Dir = "../.."
	command.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, err := command.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "build: %v %s", err, out)
		_ = os.RemoveAll(dir)
		os.Exit(2)
	}
	code := m.Run()
	if err = os.RemoveAll(dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		code = 125
	}
	os.Exit(code)
}
func TestActualCLIRenderAndEngineFeedback(t *testing.T) {
	binary := engine(t)
	dir := t.TempDir()
	for _, name := range []string{"yaml", "json"} {
		raw, e := os.ReadFile(filepath.Join("..", "..", "examples", "http-contract", "process-compose."+name))
		if e != nil {
			t.Fatal(e)
		}
		file := "workflow." + name
		os.WriteFile(filepath.Join(dir, file), raw, 0600)
		command := exec.Command(executable, "workflow", "validate", "--workspace", dir, "--file", file, "--check-engine", "--process-compose", binary)
		out, e := command.Output()
		if e != nil || !json.Valid(out) || !bytes.Contains(out, []byte(`"engine_check":"passed"`)) {
			t.Fatalf("CLI engine: %v %s", e, out)
		}
		command = exec.Command(executable, "workflow", "render", "--workspace", dir, "--file", file, "--out", "page-"+name+".html")
		out, e = command.Output()
		if e != nil || !bytes.Contains(out, []byte(`"status":"rendered"`)) {
			t.Fatalf("CLI render %v %s", e, out)
		}
	}
}
func TestActualSignalsStopOnlyValidationChild(t *testing.T) {
	for _, signal := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(signal.String(), func(t *testing.T) {
			dir := t.TempDir()
			ready := filepath.Join(dir, "ready")
			binary := filepath.Join(dir, "engine")
			body := "#!/bin/sh\ncase \"$*\" in *'version --short'*) echo v1.122.0; exit 0;; esac\nprintf ready > " + ready + "\nexec sleep 30\n"
			os.WriteFile(binary, []byte(body), 0700)
			os.WriteFile(filepath.Join(dir, "workflow.yaml"), []byte("processes: {a: {command: echo ok}}"), 0600)
			cmd := exec.Command(executable, "workflow", "validate", "--workspace", dir, "--file", "workflow.yaml", "--check-engine", "--process-compose", binary)
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &out
			if e := cmd.Start(); e != nil {
				t.Fatal(e)
			}
			// This fixture execs sleep: the directly owned validation child has no detached descendants.
			deadline := time.Now().Add(4 * time.Second)
			for {
				if _, e := os.Stat(ready); e == nil {
					break
				}
				if time.Now().After(deadline) {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
					t.Fatal("validation child never became ready")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if e := cmd.Process.Signal(signal); e != nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				t.Fatal(e)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				_ = cmd.Process.Kill()
				<-done
				t.Fatal("cancellation unbounded")
			}
			if cmd.ProcessState.ExitCode() != 128+int(signal) || !json.Valid(out.Bytes()) || !strings.Contains(out.String(), `"engine_check":"cancelled"`) {
				t.Fatalf("signal %d: %v %s", signal, cmd.ProcessState, out.String())
			}
		})
	}
}
func TestActualClosedOutputAndClosedStdin(t *testing.T) {
	r, w, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	r.Close()
	cmd := exec.Command(executable, "version")
	cmd.Stdout = w
	if e = cmd.Start(); e != nil {
		w.Close()
		t.Fatal(e)
	}
	w.Close()
	_ = cmd.Wait()
	if cmd.ProcessState.ExitCode() != 125 {
		t.Fatalf("closed pipe exit=%d", cmd.ProcessState.ExitCode())
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "engine")
	os.WriteFile(binary, []byte("#!/bin/sh\ncase \"$*\" in *'version --short'*) echo v1.122.0; exit 0;; esac\nif read value; then exit 9; fi\nexit 0\n"), 0700)
	os.WriteFile(filepath.Join(dir, "workflow.yaml"), []byte("processes: {a: {command: echo ok}}"), 0600)
	out, e := exec.Command(executable, "workflow", "validate", "--workspace", dir, "--file", "workflow.yaml", "--check-engine", "--process-compose", binary).Output()
	if e != nil || !bytes.Contains(out, []byte(`"engine_check":"passed"`)) {
		t.Fatalf("stdin not closed: %v %s", e, out)
	}
}
