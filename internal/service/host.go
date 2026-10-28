package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runcradle/internal/engine"
	pc "runcradle/internal/engine/processcompose"
	"runcradle/internal/output"
	"runcradle/internal/runs"
	"runcradle/internal/workflow"
	"sort"
	"sync"
	"time"
)

type Submission struct {
	RunID     string `json:"run_id"`
	RequestID string `json:"request_id"`
	Revision  string `json:"revision"`
	Accepted  bool   `json:"accepted"`
}
type admission struct {
	intent  runs.Intent
	sha     string
	release func() error
}
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
	bad := func(e error) (Submission, error) { return Submission{}, &Error{2, e} }
	h.mu.Lock()
	closing := h.closing
	h.mu.Unlock()
	if closing {
		return bad(fmt.Errorf("host is shutting down"))
	}
	if op.Err() != nil {
		return bad(op.Err())
	}
	if req.RequestID == "" || len(req.RequestID) > 128 || req.Timeout < time.Millisecond || req.Timeout > time.Hour || len(req.Targets) == 0 || !req.Document.Valid() {
		return bad(fmt.Errorf("request_id, valid definition, explicit targets and timeout 1..3600000ms required"))
	}
	source, e := engine.BindSource(engine.WorkflowSource{Document: req.Document, WorkflowID: req.WorkflowID, Managed: req.Managed})
	if e != nil {
		return bad(e)
	}
	targets := append([]string(nil), req.Targets...)
	sort.Strings(targets)
	names := map[string]bool{}
	for _, n := range req.Document.View.Nodes {
		names[n.Name] = true
	}
	for i, n := range targets {
		if !names[n] || i > 0 && targets[i-1] == n {
			return bad(fmt.Errorf("invalid or duplicate target"))
		}
	}
	values, e := workflow.ResolveValues(req.Document, req.Preset, req.Values)
	if e != nil {
		return bad(e)
	}
	existing, priorErr := h.Store.Request(req.RequestID)
	sha := ""
	if priorErr == nil {
		raw, err := h.Store.Invocation(existing.RunID)
		if err == nil {
			var old Invocation
			if err = json.Unmarshal(raw, &old); err != nil {
				return bad(err)
			}
			sha = old.EngineSHA
		} else if !errors.Is(err, os.ErrNotExist) {
			return bad(err)
		}
	} else if !errors.Is(priorErr, os.ErrNotExist) {
		return bad(priorErr)
	}
	if sha == "" {
		sha, e = fingerprint(h.Binary)
		if e != nil {
			if priorErr == nil {
				return Submission{existing.RunID, req.RequestID, existing.Revision, false}, &Error{125, fmt.Errorf("submission is uncertain; inspect the original request")}
			}
			return bad(e)
		}
	}
	record, digest := invocation(req, source, targets, values, sha)
	if priorErr == nil {
		if existing.Digest != digest {
			return Submission{}, &Error{2, runs.ErrConflict}
		}
		state, err := h.Store.Load(existing.RunID, time.Now())
		if err != nil {
			return Submission{}, &Error{125, err}
		}
		sub := Submission{existing.RunID, req.RequestID, existing.Revision, state.Reason != "not_submitted"}
		if state.Reason == "not_submitted" {
			return sub, &Error{2, fmt.Errorf("original request was not submitted; use a new request after capacity is available")}
		}
		return sub, nil
	}
	if _, e = workflow.ApplyValuesContext(op, req.Document, values); e != nil {
		return bad(e)
	}
	if op.Err() != nil {
		return bad(op.Err())
	}
	intent, e := h.Store.Begin(op, req.RequestID, digest, req.Document)
	if e != nil {
		return bad(e)
	}
	sub := Submission{intent.RunID, req.RequestID, req.Document.Revision, true}
	if !intent.New {
		return sub, nil
	}
	raw, e := json.Marshal(record)
	if e == nil {
		e = h.Store.SaveInvocation(intent.RunID, raw)
	}
	if e != nil {
		return sub, &Error{125, e}
	}
	release, e := h.Store.Lease(intent.RunID)
	state, loadErr := h.Store.Load(intent.RunID, time.Now())
	if loadErr != nil {
		if release != nil {
			release()
		}
		return sub, &Error{125, loadErr}
	}
	state.WorkflowID, state.WorkspaceID, state.Targets = source.WorkflowID, source.WorkspaceID, targets
	state.Values, state.EngineSHA256, state.InvocationDigest = values, sha, digest
	if e != nil {
		state.Final = true
		state.Reason = "not_submitted"
		state.Cleanup = "not_started"
		now := time.Now().UTC()
		state.EndedAt = &now
		sub.Accepted = false
		saveErr := h.Store.Save(state, false, now)
		return sub, &Error{2, errors.Join(e, saveErr)}
	}
	state.Reason = "accepted"
	if e = h.Store.Save(state, req.RetainEvidence, time.Now()); e != nil {
		release()
		return sub, &Error{125, e}
	}
	runCtx, runCancel := context.WithCancelCause(context.Background())
	active := &activeRun{runCancel, make(chan struct{})}
	h.mu.Lock()
	if h.closing {
		h.mu.Unlock()
		runCancel(CancelSignal(15))
		release()
		state.Final = true
		state.Reason = "not_submitted"
		state.Cleanup = "not_started"
		sub.Accepted = false
		err := h.Store.Save(state, false, time.Now())
		return sub, &Error{2, errors.Join(fmt.Errorf("host is shutting down"), err)}
	}
	h.active[intent.RunID] = active
	req.accepted = &admission{intent, sha, release}
	req.Values = values
	adapter := h.NewEngine()
	h.mu.Unlock()
	go func() {
		defer close(active.done)
		defer runCancel(nil)
		svc := Service{Engine: adapter, Store: h.Store, Binary: h.Binary}
		result, err := svc.Run(runCtx, req)
		if err != nil && !result.Final {
			latest, loadErr := h.Store.Load(intent.RunID, time.Now())
			if loadErr == nil {
				latest.Reason = "owner_interrupted"
				latest.TaskResult = "unknown"
				latest.Cleanup = "unknown"
				latest.OperationError = output.Clean(err.Error(), 512)
				loadErr = h.Store.Save(latest, req.RetainEvidence, time.Now())
			}
			if loadErr != nil {
				h.mu.Lock()
				h.workerError = fmt.Errorf("run %s: %s", intent.RunID, output.Clean(errors.Join(err, loadErr).Error(), 512))
				h.mu.Unlock()
			}
		}
		h.mu.Lock()
		delete(h.active, intent.RunID)
		h.mu.Unlock()
	}()
	return sub, nil
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
