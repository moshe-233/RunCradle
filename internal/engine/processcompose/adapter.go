package processcompose

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runcradle/internal/engine"
	"runcradle/internal/nodeexec"
	check "runcradle/internal/processcompose"
	"runcradle/internal/workflow"
	"strings"
	"sync"
	"syscall"
	"time"
)

type engineIssue = workflow.Issue

type Adapter struct {
	binary, helper string
	mu             sync.Mutex
	runs           map[string]*run
}
type run struct {
	receiptError             bool
	mu                       sync.Mutex
	state                    engine.Snapshot
	source                   engine.WorkflowSource
	cmd                      *exec.Cmd
	client                   *client
	dir, receipts            string
	events                   chan engine.Event
	eventsClosed, subscribed bool
	done                     chan struct{}
	wait                     chan struct{}
	stop                     chan string
	deadline                 time.Time
	stopAt                   time.Time
	records                  []nodeexec.Receipt
	out, err                 tail
	native                   chan nativeEvent
	gap                      chan struct{}
	streamCancel             context.CancelFunc
}
type tail struct {
	mu        sync.Mutex
	b         []byte
	truncated bool
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := len(p)
	t.b = append(t.b, p...)
	if len(t.b) > 65536 {
		t.b = append([]byte(nil), t.b[len(t.b)-65536:]...)
		t.truncated = true
	}
	return n, nil
}

func New(binary, helper string) *Adapter {
	return &Adapter{binary: binary, helper: helper, runs: map[string]*run{}}
}
func (a *Adapter) Capabilities(ctx context.Context) (engine.Capabilities, error) {
	if e := ctx.Err(); e != nil {
		return engine.Capabilities{}, e
	}
	version, e := check.Probe(ctx, a.binary)
	if e != nil {
		return engine.Capabilities{}, e
	}
	f := map[string]engine.Feature{}
	for _, k := range []string{"native_single_file", "dependencies", "probes", "direct_exit", "early_exit", "snapshot", "state_stream", "stop"} {
		f[k] = engine.Feature{Observable: true, Executable: true}
	}
	return engine.Capabilities{EngineKind: "process-compose", EngineVersion: version, SchemaVersion: 1, Features: f}, nil
}
func (a *Adapter) Validate(ctx context.Context, src engine.WorkflowSource) (engine.Validation, error) {
	var err error
	src, err = engine.BindSource(src)
	if err != nil {
		return engine.Validation{Issues: []engineIssue{{Severity: "error", Code: "source_identity", Message: err.Error()}}}, nil
	}
	if _, e := Prepare(src.Document, a.helper, "validation", "/not-executed"); e != nil {
		return engine.Validation{Issues: []engineIssue{{Severity: "error", Code: "unsupported", Message: e.Error()}}}, nil
	}
	r := check.Check(ctx, src.Document, a.binary)
	if r.Code != 0 {
		return engine.Validation{Issues: []engineIssue{{Severity: "error", Code: r.Status, Message: r.Message}}}, nil
	}
	return engine.Validation{Valid: true}, nil
}

func (a *Adapter) Start(ctx context.Context, req engine.StartRequest) (engine.RunHandle, error) {
	op, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var bindErr error
	req.Source, bindErr = engine.BindSource(req.Source)
	if bindErr != nil {
		return engine.RunHandle{}, bindErr
	}
	if req.Mode != "" && req.Mode != "task" {
		return engine.RunHandle{}, fmt.Errorf("unsupported runtime mode")
	}
	values, e := workflow.ResolveValues(req.Source.Document, "", req.Values)
	if e != nil {
		return engine.RunHandle{}, e
	}
	effective, e := workflow.ApplyValuesContext(op, req.Source.Document, values)
	if e != nil {
		return engine.RunHandle{}, e
	}
	if len(req.Targets) == 0 {
		return engine.RunHandle{}, fmt.Errorf("at least one target is required")
	}
	names := map[string]bool{}
	for _, n := range req.Source.Document.View.Nodes {
		names[n.Name] = true
	}
	for _, n := range req.Targets {
		if !names[n] {
			return engine.RunHandle{}, fmt.Errorf("target does not exist: %s", n)
		}
	}
	validationSource := req.Source
	validationSource.Document, validationSource.Revision = effective, effective.Revision
	valid, e := a.Validate(op, validationSource)
	if e != nil {
		return engine.RunHandle{}, e
	}
	if !valid.Valid {
		return engine.RunHandle{}, &engine.StartRejection{Cause: fmt.Errorf("validation: %s", valid.Issues[0].Message)}
	}
	if op.Err() != nil {
		return engine.RunHandle{}, op.Err()
	}
	if req.Deadline.IsZero() {
		req.Deadline = time.Now().Add(5 * time.Minute)
	}
	if !req.Deadline.After(time.Now()) {
		return engine.RunHandle{}, context.DeadlineExceeded
	}
	if time.Until(req.Deadline) > time.Hour {
		return engine.RunHandle{}, fmt.Errorf("invalid task deadline")
	}
	if req.RunID == "" {
		var id [16]byte
		if _, e = rand.Read(id[:]); e != nil {
			return engine.RunHandle{}, e
		}
		req.RunID = fmt.Sprintf("%x", id)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.runs[req.RunID]; ok {
		return engine.RunHandle{}, fmt.Errorf("run already submitted")
	}
	dir, e := os.MkdirTemp("", "runcradle-run-")
	if e != nil {
		return engine.RunHandle{}, e
	}
	failed := true
	defer func() {
		if failed {
			_ = os.RemoveAll(dir)
		}
	}()
	receipts := filepath.Join(dir, "receipts")
	if e = os.Mkdir(receipts, 0700); e != nil {
		return engine.RunHandle{}, e
	}
	raw, e := Prepare(effective, a.helper, req.RunID, receipts)
	if e != nil {
		return engine.RunHandle{}, e
	}
	file := filepath.Join(dir, "native.yaml")
	if e = os.WriteFile(file, raw, 0600); e != nil {
		return engine.RunHandle{}, e
	}
	binary, e := exec.LookPath(a.binary)
	if e != nil {
		return engine.RunHandle{}, e
	}
	binary, e = filepath.Abs(binary)
	if e != nil {
		return engine.RunHandle{}, e
	}
	socket := filepath.Join(dir, "pc.sock")
	args := []string{"--log-file", "/dev/null", "up", "--config", file, "--tui=false", "--keep-project", "--no-watch", "--disable-dotenv", "--use-uds", "--unix-socket", socket}
	args = append(args, "--")
	args = append(args, req.Targets...)
	cmd := exec.Command(binary, args...)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 100 * time.Millisecond
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "PC_") && !strings.HasPrefix(v, "XDG_CONFIG_HOME=") && !strings.HasPrefix(v, "XDG_CONFIG_DIRS=") {
			cmd.Env = append(cmd.Env, v)
		}
	}
	cmd.Env = append(cmd.Env, "TERM=dumb", "NO_COLOR=1", "XDG_CONFIG_HOME="+dir, "XDG_CONFIG_DIRS="+dir)
	r := &run{source: req.Source, dir: dir, receipts: receipts, cmd: cmd, client: newClient(socket), events: make(chan engine.Event, 256), done: make(chan struct{}), wait: make(chan struct{}), stop: make(chan string, 1), deadline: req.Deadline, native: make(chan nativeEvent, 256), gap: make(chan struct{}, 1)}
	r.state = engine.Snapshot{Version: 1, RunID: req.RunID, Revision: req.Source.Document.Revision, EngineInstanceID: filepath.Base(dir), Targets: append([]string(nil), req.Targets...), TaskResult: "pending", Cleanup: "unknown", CleanupScope: "registered_process_groups", HistoryIntegrity: "partial", IOStatus: "unknown", Reason: "running", StartedAt: time.Now().UTC()}
	r.state.EngineVersion = check.Version
	r.state.Values = values
	r.state.ResidualPath = dir
	r.state.WorkflowID = req.Source.WorkflowID
	r.state.WorkspaceID = req.Source.WorkspaceID
	r.state.ExecutionDigest = fmt.Sprintf("%x", sha256.Sum256(raw))
	r.state.InvocationDigest = req.InvocationDigest
	for _, n := range req.Source.Document.View.Nodes {
		r.state.Nodes = append(r.state.Nodes, engine.Node{ID: n.ID, Name: n.Name, State: "declared", Health: "not_configured", Source: "declaration"})
	}
	cmd.Stdout = &r.out
	cmd.Stderr = &r.err
	if e = cmd.Start(); e != nil {
		return engine.RunHandle{}, e
	}
	failed = false
	a.runs[req.RunID] = r
	go func() {
		waitErr := cmd.Wait()
		r.mu.Lock()
		if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() >= 0 {
			v := cmd.ProcessState.ExitCode()
			r.state.EngineExitCode = &v
		}
		if cmd.ProcessState != nil {
			if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				r.state.EngineSignal = int(status.Signal())
			}
		}
		r.state.IOStatus = "complete"
		var exit *exec.ExitError
		if waitErr != nil && !errors.As(waitErr, &exit) {
			r.state.IOStatus = "incomplete"
		}
		r.mu.Unlock()
		close(r.wait)
	}()
	streamCtx, streamCancel := context.WithCancel(context.Background())
	r.streamCancel = streamCancel
	go r.stream(streamCtx)
	go r.monitor()
	return engine.RunHandle{RunID: req.RunID, EngineKind: "process-compose", EngineInstanceID: r.state.EngineInstanceID}, nil
}

func (a *Adapter) owned(h engine.RunHandle) (*run, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	r := a.runs[h.RunID]
	if r == nil || h.EngineKind != "process-compose" || r.state.EngineInstanceID != h.EngineInstanceID {
		return nil, fmt.Errorf("unknown or unowned run handle")
	}
	return r, nil
}
func (a *Adapter) Snapshot(ctx context.Context, h engine.RunHandle) (engine.Snapshot, error) {
	if e := ctx.Err(); e != nil {
		return engine.Snapshot{}, e
	}
	r, e := a.owned(h)
	if e != nil {
		return engine.Snapshot{}, e
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	raw, _ := json.Marshal(r.state)
	var s engine.Snapshot
	e = json.Unmarshal(raw, &s)
	return s, e
}
func (a *Adapter) Subscribe(ctx context.Context, h engine.RunHandle) (<-chan engine.Event, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	r, e := a.owned(h)
	if e != nil {
		return nil, e
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.subscribed {
		return nil, fmt.Errorf("run already subscribed")
	}
	r.subscribed = true
	return r.events, nil
}
func (a *Adapter) Stop(ctx context.Context, h engine.RunHandle) (engine.StopResult, error) {
	r, e := a.owned(h)
	if e != nil {
		return engine.StopResult{}, e
	}
	select {
	case <-r.done:
	default:
		select {
		case r.stop <- "stop_requested":
		default:
		}
	}
	op, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	force := time.NewTimer(8 * time.Second) // reserve time for Wait and final evidence
	defer force.Stop()
	select {
	case <-r.done:
	case <-force.C:
		_ = r.cmd.Process.Kill() // only the directly owned manager
	case <-op.Done():
		return engine.StopResult{Requested: true, Cleanup: "unknown", Reason: "stop_observation_timeout"}, op.Err()
	}
	select {
	case <-r.done:
	case <-op.Done():
		return engine.StopResult{Requested: true, Cleanup: "unknown", Reason: "stop_observation_timeout"}, op.Err()
	}
	s, e := a.Snapshot(context.Background(), h)
	return engine.StopResult{Requested: true, Terminal: s.Final, Cleanup: s.Cleanup, Reason: s.Reason}, e
}
