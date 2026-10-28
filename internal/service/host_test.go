package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runcradle/internal/engine"
	"runcradle/internal/runs"
	"runcradle/internal/workflow"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type controlledEngine struct {
	startError error
	mu         sync.Mutex
	state      engine.Snapshot
	starts     *atomic.Int32
	finish     <-chan struct{}
	events     chan engine.Event
}

func (f *controlledEngine) Capabilities(context.Context) (engine.Capabilities, error) {
	return engine.Capabilities{}, nil
}
func (f *controlledEngine) Validate(context.Context, engine.WorkflowSource) (engine.Validation, error) {
	return engine.Validation{Valid: true}, nil
}
func (f *controlledEngine) Start(_ context.Context, r engine.StartRequest) (engine.RunHandle, error) {
	if f.startError != nil {
		return engine.RunHandle{}, f.startError
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts.Add(1)
	f.events = make(chan engine.Event)
	f.state = engine.Snapshot{Version: 1, RunID: r.RunID, Revision: r.Source.Revision, WorkflowID: r.Source.WorkflowID, EngineInstanceID: r.RunID, Targets: r.Targets, TaskResult: "pending", Reason: "running", HistoryIntegrity: "partial", CleanupScope: "registered_process_groups", Nodes: []engine.Node{{ID: r.Source.Document.View.Nodes[0].ID, Name: "a", State: "running"}}}
	return engine.RunHandle{RunID: r.RunID}, nil
}

func TestBackgroundStartFailureHasDurableOperationError(t *testing.T) {
	h, d, _, _ := hostFixture(t)
	h.NewEngine = func() engine.Engine {
		return &controlledEngine{startError: fmt.Errorf("controlled native startup failure")}
	}
	sub, e := h.StartRun(context.Background(), Request{Document: d, Targets: []string{"a"}, Timeout: time.Second, RequestID: "start-error"})
	if e != nil {
		t.Fatal(e)
	}
	state := awaitHost(t, h, sub.RunID)
	if state.OperationError == "" || state.TaskResult != "unknown" || state.Reason != "start_failed" {
		t.Fatalf("operation cause lost: %+v", state)
	}
}

func TestKnownCancellationCodeSurvivesInterruptedObservation(t *testing.T) {
	if got := ExitCode(engine.Snapshot{Reason: "interrupted", TaskResult: "unknown", CancellationCode: 143, Cleanup: "unknown"}); got != 143 {
		t.Fatalf("known cancellation replaced by uncertainty: %d", got)
	}
}
func (f *controlledEngine) Snapshot(_ context.Context, _ engine.RunHandle) (engine.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	select {
	case <-f.finish:
		f.terminal()
	default:
	}
	return f.state, nil
}
func (f *controlledEngine) terminal() {
	zero := 0
	f.state.Final = true
	f.state.TaskResult = "succeeded"
	f.state.EngineExitCode = &zero
	f.state.Cleanup = "complete"
	f.state.IOStatus = "complete"
	f.state.Nodes[0].ExitCode = &zero
	f.state.Nodes[0].State = "completed"
}
func (f *controlledEngine) Subscribe(context.Context, engine.RunHandle) (<-chan engine.Event, error) {
	return f.events, nil
}
func (f *controlledEngine) Stop(context.Context, engine.RunHandle) (engine.StopResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.terminal()
	return engine.StopResult{Requested: true, Terminal: true, Cleanup: "complete"}, nil
}
func hostFixture(t *testing.T) (*Host, workflow.Document, *atomic.Int32, chan struct{}) {
	t.Helper()
	dir := t.TempDir()
	if e := os.WriteFile(filepath.Join(dir, "pc.yaml"), []byte("processes: {a: {command: echo ok}}"), 0600); e != nil {
		t.Fatal(e)
	}
	d, e := workflow.Load(dir, "pc.yaml", "")
	if e != nil {
		t.Fatal(e)
	}
	store, e := runs.Open(dir, true)
	if e != nil {
		t.Fatal(e)
	}
	h := NewHost(store, "/bin/true", "/bin/true")
	count := new(atomic.Int32)
	finish := make(chan struct{})
	h.NewEngine = func() engine.Engine { return &controlledEngine{starts: count, finish: finish} }
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		h.Close(ctx)
		store.Close()
	})
	return h, d, count, finish
}
func awaitHost(t *testing.T, h *Host, id string) engine.Snapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s, e := h.InspectRun(id)
		if e != nil {
			t.Fatal(e)
		}
		if s.Final {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("run not final")
	return engine.Snapshot{}
}
func TestHostAcceptedRequestSurvivesDisconnectAndDeduplicates(t *testing.T) {
	h, d, starts, finish := hostFixture(t)
	req := Request{Document: d, Targets: []string{"a"}, Timeout: time.Second, RequestID: "one"}
	ctx, cancel := context.WithCancel(context.Background())
	sub, e := h.StartRun(ctx, req)
	if e != nil {
		t.Fatal(e)
	}
	cancel()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			same, e := h.StartRun(context.Background(), req)
			if e != nil || same.RunID != sub.RunID {
				t.Errorf("retry %+v %v", same, e)
			}
		}()
	}
	wg.Wait()
	close(finish)
	s := awaitHost(t, h, sub.RunID)
	if starts.Load() != 1 || s.TaskResult != "succeeded" {
		t.Fatalf("duplicate/cancelled: %d %+v", starts.Load(), s)
	}
	req.Timeout = 2 * time.Second
	if _, e = h.StartRun(context.Background(), req); e == nil {
		t.Fatal("conflicting request accepted")
	}
	h.Binary = "/missing/engine"
	req.Timeout = time.Second
	if _, e = h.StartRun(context.Background(), req); e != nil {
		t.Fatalf("retry depends on engine: %v", e)
	}
	// The terminal snapshot is persisted before the worker releases its active
	// handle. Observe release separately; a leaked handle still fails boundedly.
	deadline := time.Now().Add(2 * time.Second)
	for h.ActiveRuns() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if h.ActiveRuns() != 0 {
		t.Fatal("completed adapter retained")
	}
}
func TestHostCapacityStopAndShutdown(t *testing.T) {
	h, d, _, _ := hostFixture(t)
	ids := []string{}
	for _, id := range []string{"a", "b", "c", "d"} {
		sub, e := h.StartRun(context.Background(), Request{Document: d, Targets: []string{"a"}, Timeout: time.Second, RequestID: id})
		if e != nil {
			t.Fatal(e)
		}
		ids = append(ids, sub.RunID)
	}
	rejected, e := h.StartRun(context.Background(), Request{Document: d, Targets: []string{"a"}, Timeout: time.Second, RequestID: "full"})
	if e == nil || rejected.RunID == "" {
		t.Fatalf("capacity: %+v %v", rejected, e)
	}
	state, e := h.InspectRun(rejected.RunID)
	if e != nil || !state.Final || state.Reason != "not_submitted" {
		t.Fatalf("capacity lost: %+v %v", state, e)
	}
	if _, e = h.StopRun(ids[0], 143); e != nil {
		t.Fatal(e)
	}
	state = awaitHost(t, h, ids[0])
	if state.CancellationCode != 143 {
		raw, _ := json.Marshal(state)
		t.Fatalf("signal lost: %s", raw)
	}
	if _, e = h.StopRun(ids[0], 143); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if e = h.Close(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = h.StartRun(context.Background(), Request{Document: d, Targets: []string{"a"}, Timeout: time.Second, RequestID: "closed"}); e == nil {
		t.Fatal("closed host accepts")
	}
}

func TestAcceptedCancellationBeforeWorkerDoesNotLeakLease(t *testing.T) {
	h, d, _, _ := hostFixture(t)
	src, err := engine.BindSource(engine.WorkflowSource{Document: d})
	if err != nil {
		t.Fatal(err)
	}
	req := Request{Document: d, Targets: []string{"a"}, Timeout: time.Second, RequestID: "before-worker"}
	sha, err := fingerprint("/bin/true")
	if err != nil {
		t.Fatal(err)
	}
	_, digest := invocation(req, src, req.Targets, nil, sha)
	intent, err := h.Store.Begin(context.Background(), req.RequestID, digest, d)
	if err != nil {
		t.Fatal(err)
	}
	release, err := h.Store.Lease(intent.RunID)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	prepared, err := prepareRun(req)
	if err != nil {
		t.Fatal(err)
	}
	accepted := acceptedRun{preparedRun: prepared, intent: intent, engineSHA: sha, invocationDigest: digest, release: release}
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(CancelSignal(15))
	executor := runExecutor{engine: h.NewEngine(), store: h.Store}
	_, _ = executor.run(ctx, accepted)
	state, err := h.Store.Load(intent.RunID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	active, err := h.Store.Active(intent.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if active || !state.Final || state.CancellationCode != 143 {
		t.Fatalf("accepted cancellation lost/lease leaked: active=%v state=%+v", active, state)
	}
}

func TestShutdownBetweenAdmissionAndWorkerReleasesSlot(t *testing.T) {
	h, document, starts, _ := hostFixture(t)
	request := Request{Document: document, Targets: []string{"a"}, Timeout: time.Second, RequestID: "shutdown-handoff"}
	prepared, err := prepareRun(request)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := h.identifyRun(prepared)
	if err != nil {
		t.Fatal(err)
	}
	submission, accepted, err := h.admitRun(context.Background(), prepared, identity)
	if err != nil || accepted == nil {
		t.Fatalf("admission: %v", err)
	}
	if err = h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	submission, err = h.launchRun(submission, *accepted)
	if err == nil || submission.Accepted {
		t.Fatalf("closed host started worker: %+v %v", submission, err)
	}
	state, err := h.InspectRun(submission.RunID)
	if err != nil || !state.Final || state.Reason != "not_submitted" || state.Cleanup != "not_started" {
		t.Fatalf("shutdown evidence: %+v %v", state, err)
	}
	active, err := h.Store.Active(submission.RunID)
	if err != nil || active || h.ActiveRuns() != 0 || starts.Load() != 0 {
		t.Fatalf("ownership leaked: active=%v starts=%d err=%v", active, starts.Load(), err)
	}
}

func TestInvocationCanonicalEncodingRemainsStable(t *testing.T) {
	request := Request{Timeout: time.Second, Preset: "p", RetainEvidence: true}
	record, digest := invocation(request, engine.WorkflowSource{WorkflowID: "wf", Revision: "rev"}, []string{"a"}, map[string]json.RawMessage{"count": json.RawMessage("7")}, "sha")
	const expected = `{"workflow_id":"wf","revision":"rev","target_nodes":["a"],"timeout_ms":1000,"engine_binary_sha256":"sha","preset_id":"p","values":{"count":7},"retain_evidence":true}`
	raw, err := json.Marshal(record)
	if err != nil || string(raw) != expected || digest != fmt.Sprintf("%x", sha256.Sum256([]byte(expected))) {
		t.Fatalf("canonical invocation changed: %s %s %v", raw, digest, err)
	}
}
