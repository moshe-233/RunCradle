package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"runcradle/internal/engine"
	pc "runcradle/internal/engine/processcompose"
	"runcradle/internal/output"
	"runcradle/internal/protocol"
	"runcradle/internal/runs"
	"sync"
	"time"
)

type Submission = protocol.Submission
type activeRun struct {
	cancel context.CancelCauseFunc
	done   chan struct{}
}
type Host struct {
	Store       *runs.Store
	Binary      string
	NewEngine   func() engine.Engine
	mu          sync.Mutex
	active      map[string]*activeRun
	closing     bool
	workerError error
}
type Invocation struct {
	WorkflowID string                     `json:"workflow_id"`
	Revision   string                     `json:"revision"`
	Targets    []string                   `json:"target_nodes"`
	TimeoutMS  int64                      `json:"timeout_ms"`
	EngineSHA  string                     `json:"engine_binary_sha256"`
	Preset     string                     `json:"preset_id,omitempty"`
	Values     map[string]json.RawMessage `json:"values,omitempty"`
	Retain     bool                       `json:"retain_evidence"`
}

func invocation(req Request, source engine.WorkflowSource, targets []string, values map[string]json.RawMessage, sha string) (Invocation, string) {
	record := Invocation{source.WorkflowID, source.Revision, targets, req.Timeout.Milliseconds(), sha, req.Preset, values, req.RetainEvidence}
	raw, _ := json.Marshal(record)
	return record, fmt.Sprintf("%x", sha256.Sum256(raw))
}
func NewHost(store *runs.Store, binary, helper string) *Host {
	h := &Host{Store: store, Binary: binary, active: map[string]*activeRun{}}
	h.NewEngine = func() engine.Engine { return pc.New(binary, helper) }
	return h
}

type CancelSignal int

func (s CancelSignal) Error() string     { return fmt.Sprintf("cancelled by signal %d", s) }
func (s CancelSignal) SignalNumber() int { return int(s) }

func (h *Host) StartRun(ctx context.Context, req Request) (Submission, error) {
	op, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	h.mu.Lock()
	closing := h.closing
	h.mu.Unlock()
	if closing {
		return Submission{}, &Error{2, fmt.Errorf("host is shutting down")}
	}
	if op.Err() != nil {
		return Submission{}, &Error{2, op.Err()}
	}
	prepared, err := prepareRun(req)
	if err != nil {
		return Submission{}, &Error{2, err}
	}
	identity, err := h.identifyRun(prepared)
	if err != nil {
		if Code(err) == 125 && identity.previous != nil {
			previous := identity.previous
			return Submission{RunID: previous.RunID, RequestID: req.RequestID, Revision: previous.Revision}, err
		}
		return Submission{}, err
	}
	if identity.previous != nil {
		return h.replayRun(identity, req.RequestID)
	}
	sub, accepted, err := h.admitRun(op, prepared, identity)
	if err != nil || accepted == nil {
		return sub, err
	}
	return h.launchRun(sub, *accepted)
}

func (h *Host) launchRun(sub Submission, accepted acceptedRun) (Submission, error) {
	// 受理后脱离 HTTP 请求上下文；只有宿主关闭或显式取消才停止此运行。
	runCtx, cancel := context.WithCancelCause(context.Background())
	active := &activeRun{cancel: cancel, done: make(chan struct{})}
	h.mu.Lock()
	if h.closing {
		h.mu.Unlock()
		cancel(CancelSignal(15))
		accepted.release()
		state := accepted.initial
		state.Final, state.Reason, state.Cleanup = true, "not_submitted", "not_started"
		sub.Accepted = false
		err := h.Store.Save(state, false, time.Now())
		return sub, &Error{2, errors.Join(fmt.Errorf("host is shutting down"), err)}
	}
	h.active[accepted.intent.RunID] = active
	execution := h.NewEngine()
	h.mu.Unlock()
	go h.runWorker(runCtx, active, accepted, execution)
	return sub, nil
}

func (h *Host) runWorker(ctx context.Context, active *activeRun, accepted acceptedRun, execution engine.Engine) {
	defer close(active.done)
	defer active.cancel(nil)
	executor := runExecutor{engine: execution, store: h.Store}
	result, err := executor.run(ctx, accepted)
	if err != nil && !result.Final {
		h.recordWorkerError(accepted, err)
	}
	// 终态可能已经落盘；活动句柄移除之后，worker 才关闭 done。
	h.mu.Lock()
	delete(h.active, accepted.intent.RunID)
	h.mu.Unlock()
}

func (h *Host) recordWorkerError(accepted acceptedRun, cause error) {
	runID := accepted.intent.RunID
	latest, err := h.Store.Load(runID, time.Now())
	if err == nil {
		latest.Reason, latest.TaskResult, latest.Cleanup = "owner_interrupted", "unknown", "unknown"
		latest.OperationError = output.Clean(cause.Error(), 512)
		err = h.Store.Save(latest, accepted.request.RetainEvidence, time.Now())
	}
	if err != nil {
		h.mu.Lock()
		h.workerError = fmt.Errorf("run %s: %s", runID, output.Clean(errors.Join(cause, err).Error(), 512))
		h.mu.Unlock()
	}
}
func (h *Host) InspectRun(id string) (engine.Snapshot, error) { return h.Store.Load(id, time.Now()) }
func (h *Host) ActiveRuns() int                               { h.mu.Lock(); defer h.mu.Unlock(); return len(h.active) }
func (h *Host) OwnsRun(id string) bool                        { h.mu.Lock(); defer h.mu.Unlock(); return h.active[id] != nil }
func (h *Host) StopRun(id string, code int) (engine.StopResult, error) {
	if code != 130 && code != 143 {
		return engine.StopResult{}, &Error{2, fmt.Errorf("cancellation code must be 130 or 143")}
	}
	h.mu.Lock()
	active := h.active[id]
	if active != nil {
		active.cancel(CancelSignal(code - 128))
	}
	h.mu.Unlock()
	state, e := h.Store.Load(id, time.Now())
	if e != nil {
		return engine.StopResult{}, e
	}
	if active == nil && !state.Final {
		return engine.StopResult{}, fmt.Errorf("run is not owned by this host; inspect interrupted evidence")
	}
	return engine.StopResult{Requested: active != nil || state.StopRequested || state.Reason == "cancelled", Terminal: state.Final, Cleanup: state.Cleanup, Reason: state.Reason}, nil
}
func (h *Host) Close(ctx context.Context) error {
	h.mu.Lock()
	h.closing = true
	active := make([]*activeRun, 0, len(h.active))
	for _, r := range h.active {
		r.cancel(CancelSignal(15))
		active = append(active, r)
	}
	h.mu.Unlock()
	for _, r := range active {
		select {
		case <-r.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.workerError
}
