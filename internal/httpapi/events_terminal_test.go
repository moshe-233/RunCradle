package httpapi

import (
	"context"
	"net/http/httptest"
	"runcradle/internal/engine"
	"runcradle/internal/service"
	"runcradle/internal/workflow"
	"strings"
	"sync"
	"testing"
	"time"
)

type journalEngine struct {
	mu     sync.Mutex
	state  engine.Snapshot
	events chan engine.Event
}

func (f *journalEngine) Capabilities(context.Context) (engine.Capabilities, error) {
	return engine.Capabilities{}, nil
}
func (f *journalEngine) Validate(context.Context, engine.WorkflowSource) (engine.Validation, error) {
	return engine.Validation{Valid: true}, nil
}
func (f *journalEngine) Start(_ context.Context, r engine.StartRequest) (engine.RunHandle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = make(chan engine.Event)
	f.state = engine.Snapshot{Version: 1, RunID: r.RunID, Revision: r.Source.Revision, Targets: r.Targets, TaskResult: "pending", Reason: "running", HistoryIntegrity: "complete", Nodes: []engine.Node{{ID: r.Source.Document.View.Nodes[0].ID, Name: "a"}}}
	return engine.RunHandle{RunID: r.RunID}, nil
}
func (f *journalEngine) Snapshot(context.Context, engine.RunHandle) (engine.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state, nil
}
func (f *journalEngine) Subscribe(context.Context, engine.RunHandle) (<-chan engine.Event, error) {
	return f.events, nil
}
func (f *journalEngine) Stop(context.Context, engine.RunHandle) (engine.StopResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state.Final = true
	f.state.TaskResult = "unknown"
	return engine.StopResult{Requested: true, Terminal: true}, nil
}

type boundaryWriter struct {
	*httptest.ResponseRecorder
	injected bool
	inject   func()
}

func (w *boundaryWriter) SetWriteDeadline(time.Time) error { return nil }
func (w *boundaryWriter) Flush() {
	if !w.injected && strings.Contains(w.Body.String(), "id: 1\n") {
		w.injected = true
		w.inject()
	}
	w.ResponseRecorder.Flush()
}

func TestSSEFinalDrainsTailPublishedDuringEventFlush(t *testing.T) {
	dir := t.TempDir()
	s, e := Open(Config{Workspace: dir, Binary: "/bin/true", Helper: "/bin/true"})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	native := &journalEngine{}
	s.Host.NewEngine = func() engine.Engine { return native }
	d, e := workflow.FromBytes(dir, "pc.yaml", "", "yaml", []byte("processes: {a: {command: echo ok}}"), nil)
	if e != nil {
		t.Fatal(e)
	}
	sub, e := s.Host.StartRun(context.Background(), service.Request{Document: d, Targets: []string{"a"}, RequestID: "review-terminal-tail", Timeout: time.Second})
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 200; i++ {
		state, _ := s.Store.Load(sub.RunID, time.Now())
		if state.Reason == "running" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	for i := 0; i < 3; i++ {
		if _, e = s.Store.Append(sub.RunID, engine.Event{Kind: "earlier"}); e != nil {
			t.Fatal(e)
		}
	}
	writer := &boundaryWriter{ResponseRecorder: httptest.NewRecorder()}
	writer.inject = func() {
		if _, e = s.Store.Append(sub.RunID, engine.Event{Kind: "tail-before-final"}); e != nil {
			t.Fatal(e)
		}
		state, e := s.Store.Load(sub.RunID, time.Now())
		if e != nil {
			t.Fatal(e)
		}
		state.Final = true
		state.Reason = "fixture_terminal"
		state.TaskResult = "unknown"
		native.mu.Lock()
		native.state = state
		native.mu.Unlock()
		if e = s.Store.Save(state, false, time.Now()); e != nil {
			t.Fatal(e)
		}
	}
	req := httptest.NewRequest("GET", "/api/v1/runs/"+sub.RunID+"/events", nil)
	req.SetPathValue("id", sub.RunID)
	req.Header.Set("Accept", "text/event-stream")
	s.events(writer, req)
	tail, _, gap, e := s.Store.Events(sub.RunID, 0, 100)
	if e != nil || gap || len(tail) == 0 {
		t.Fatalf("journal %v %v %+v", e, gap, tail)
	}
	t.Logf("journal_tail=%d final_sent=%v tail_sent=%v", tail[len(tail)-1].Seq, strings.Contains(writer.Body.String(), "event: final"), strings.Contains(writer.Body.String(), "tail-before-final"))
	if !strings.Contains(writer.Body.String(), "tail-before-final") {
		t.Fatal("terminal notification overtook persisted event tail")
	}
}
