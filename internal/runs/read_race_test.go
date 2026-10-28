package runs

import (
	"bytes"
	"context"
	"encoding/json"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"runcradle/internal/engine"
	"strings"
	"testing"
	"time"
)

func TestOwnerCompletionDuringHistoryReadDoesNotInventInterruption(t *testing.T) {
	s, d := fixture(t)
	intent, e := s.Begin(context.Background(), "read-race", strings.Repeat("a", 64), d)
	if e != nil {
		t.Fatal(e)
	}
	release, e := s.Lease(intent.RunID)
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	running := engine.Snapshot{Version: 1, RunID: intent.RunID, Revision: d.Revision, Reason: "running", TaskResult: "unknown", Cleanup: "unknown", HistoryIntegrity: "partial"}
	if e = s.Save(running, false, time.Now()); e != nil {
		t.Fatal(e)
	}
	var journal bytes.Buffer
	encoder := json.NewEncoder(&journal)
	for i := 1; i <= 8000; i++ {
		if e = encoder.Encode(engine.Event{RunID: intent.RunID, Seq: uint64(i), Kind: strings.Repeat("x", 1024)}); e != nil {
			t.Fatal(e)
		}
	}
	file := filepath.Join(d.Workspace, ".runcradle/runs", intent.RunID, "events.jsonl")
	if e = os.WriteFile(file, journal.Bytes(), 0600); e != nil {
		t.Fatal(e)
	}
	fd, e := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if e != nil {
		t.Fatal(e)
	}
	defer unix.Close(fd)
	if _, e = unix.InotifyAddWatch(fd, file, unix.IN_OPEN); e != nil {
		t.Fatal(e)
	}
	result := make(chan engine.Snapshot, 1)
	failure := make(chan error, 1)
	go func() { state, err := s.Load(intent.RunID, time.Now()); result <- state; failure <- err }()
	// Journal open follows the snapshot read. Publish completion at this exact boundary.
	ready := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		buffer := make([]byte, 4096)
		n, err := unix.Read(fd, buffer)
		if n > 0 {
			ready = true
			break
		}
		if err != unix.EAGAIN && err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	if !ready {
		t.Fatal("reader did not reach journal")
	}
	terminal := running
	terminal.Final = true
	terminal.Reason = "engine_exited"
	if e = s.Save(terminal, false, time.Now()); e != nil {
		t.Fatal(e)
	}
	if e = release(); e != nil {
		t.Fatal(e)
	}
	select {
	case state := <-result:
		if e = <-failure; e != nil || !state.Final || state.Reason == "interrupted" {
			t.Fatalf("completed owner misreported: %+v %v", state, e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("history read exceeded budget")
	}
}
