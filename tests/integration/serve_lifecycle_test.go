package integration

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	eng "runcradle/internal/engine"
	"runcradle/internal/hostclient"
	"runcradle/internal/httpapi"
	"runcradle/internal/runs"
	"runcradle/internal/service"
	"runcradle/internal/workflow"
	"syscall"
	"testing"
	"time"
)

func TestActualServeOwnsDisconnectedRunAndStopsOnlyItsInstance(t *testing.T) {
	dir := t.TempDir()
	source := []byte("processes: {a: {command: 'touch live; exec sleep 30', shutdown: {timeout_seconds: 1}}}")
	d, e := workflow.FromBytes(dir, "pc.yaml", "", "yaml", source, nil)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "serve", "--workspace", dir, "--process-compose", engine(t))
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	done := false
	defer func() {
		if !done {
			cmd.Process.Signal(syscall.SIGTERM)
			cmd.Wait()
		}
	}()
	var ready httpapi.Ready
	if e = json.NewDecoder(pipe).Decode(&ready); e != nil {
		t.Fatal(e)
	}
	client, e := hostclient.Connect(ctx, dir)
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	requestCtx, disconnect := context.WithCancel(context.Background())
	var sub service.Submission
	input := httpapi.RunRequest{Document: &httpapi.Draft{Format: "yaml", Entry: "pc.yaml", Source: string(source)}, Revision: d.Revision, Targets: []string{"a"}, RequestID: "detached", TimeoutMS: 10000}
	if e = client.Do(requestCtx, "POST", "/api/v1/runs", input, &sub); e != nil {
		t.Fatal(e)
	}
	disconnect()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, e = os.Stat(filepath.Join(dir, "live")); e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("task not started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	var state eng.Snapshot
	if e = client.Do(ctx, "GET", "/api/v1/runs/"+sub.RunID, nil, &state); e != nil || state.Final {
		t.Fatalf("request cancelled owned run %+v %v", state, e)
	}
	if e = cmd.Process.Signal(syscall.SIGTERM); e != nil {
		t.Fatal(e)
	}
	e = cmd.Wait()
	done = true
	if cmd.ProcessState.ExitCode() != 143 || ctx.Err() != nil {
		t.Fatalf("serve termination %v: %v", cmd.ProcessState, e)
	}
	store, e := runs.Open(dir, false)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	state, e = store.Load(sub.RunID, time.Now())
	if e != nil || !state.Final || state.CancellationCode != 143 || state.Reason != "cancelled" || state.Cleanup != "complete" || state.CleanupScope != "registered_process_groups" {
		t.Fatalf("shutdown facts %+v %v", state, e)
	}
	if _, e = store.Server(); !os.IsNotExist(e) {
		t.Fatalf("stale live descriptor remains %v", e)
	}
	go func() {
		c, e := listener.Accept()
		if e == nil {
			c.Write([]byte("untouched"))
			c.Close()
		}
	}()
	c, e := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(time.Second))
	raw, e := io.ReadAll(c)
	if e != nil || string(raw) != "untouched" {
		t.Fatal("foreign listener changed")
	}
}
