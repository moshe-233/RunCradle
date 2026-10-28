package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runcradle/internal/engine"
	"runcradle/internal/output"
	"runcradle/internal/runs"
	"runcradle/internal/workflow"
	"sort"
	"sync"
	"time"
)

type Error struct {
	Code  int
	Cause error
}

func (e *Error) Error() string { return e.Cause.Error() }
func (e *Error) Unwrap() error { return e.Cause }
func Code(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return 125
}

func markCancelled(ctx context.Context, state *engine.Snapshot) {
	state.Reason = "cancelled"
	state.CancellationCode = 130
	if signal, ok := context.Cause(ctx).(interface{ SignalNumber() int }); ok {
		state.CancellationCode = 128 + signal.SignalNumber()
	}
}

type Service struct {
	Engine engine.Engine
	Store  *runs.Store
	Binary string
}
type Request struct {
	WorkflowID     string
	Managed        bool
	Preset         string
	Values         map[string]json.RawMessage
	accepted       *admission
	Document       workflow.Document
	Targets        []string
	Timeout        time.Duration
	RequestID      string
	RetainEvidence bool
}

func fingerprint(binary string) (string, error) {
	path, e := exec.LookPath(binary)
	if e != nil {
		return "", e
	}
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() {
		return "", fmt.Errorf("engine must be a regular executable")
	}
	h := sha256.New()
	n, e := io.Copy(h, io.LimitReader(f, 256<<20+1))
	if e != nil {
		return "", e
	}
	if n > 256<<20 {
		return "", fmt.Errorf("engine binary exceeds limit")
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func (s *Service) Run(ctx context.Context, req Request) (state engine.Snapshot, err error) {
	if ctx.Err() != nil {
		if req.accepted != nil {
			defer func() { err = errors.Join(err, req.accepted.release()) }()
			state, e := s.Store.Load(req.accepted.intent.RunID, time.Now())
			if e != nil {
				return state, &Error{125, e}
			}
			markCancelled(ctx, &state)
			state.Final = true
			state.Cleanup = "not_started"
			now := time.Now().UTC()
			state.EndedAt = &now
			return state, &Error{state.CancellationCode, errors.Join(ctx.Err(), s.Store.Save(state, req.RetainEvidence, now))}
		}
		return state, ctx.Err()
	}
	if req.Timeout < time.Millisecond || req.Timeout > time.Hour {
		return state, &Error{2, fmt.Errorf("timeout must be 1..3600000 ms")}
	}
	if len(req.Targets) == 0 {
		return state, &Error{2, fmt.Errorf("explicit target required")}
	}
	targets := append([]string(nil), req.Targets...)
	sort.Strings(targets)
	for i := 1; i < len(targets); i++ {
		if targets[i] == targets[i-1] {
			return state, &Error{2, fmt.Errorf("duplicate target")}
		}
	}
	for _, target := range targets {
		found := false
		for _, n := range req.Document.View.Nodes {
			if n.Name == target {
				found = true
			}
		}
		if !found {
			return state, &Error{2, fmt.Errorf("target does not exist")}
		}
	}
	source, e := engine.BindSource(engine.WorkflowSource{Document: req.Document, Revision: req.Document.Revision, WorkflowID: req.WorkflowID, Managed: req.Managed})
	if e != nil {
		return state, &Error{2, e}
	}
	values, e := workflow.ResolveValues(req.Document, req.Preset, req.Values)
	if e != nil {
		return state, &Error{2, e}
	}
	sha := ""
	var intent runs.Intent
	var release func() error
	if req.accepted != nil {
		sha, intent, release = req.accepted.sha, req.accepted.intent, req.accepted.release
	} else {
		op, cancel := context.WithTimeout(ctx, 5*time.Second)
		valid, err := s.Engine.Validate(op, source)
		cancel()
		if err != nil {
			return state, &Error{2, err}
		}
		if !valid.Valid {
			return state, &Error{2, fmt.Errorf("validation rejected")}
		}
		sha, e = fingerprint(s.Binary)
		if e != nil {
			return state, &Error{2, e}
		}
	}
	_, digest := invocation(req, source, targets, values, sha)
	if req.accepted == nil {
		intent, e = s.Store.Begin(ctx, req.RequestID, digest, req.Document)
		if e != nil {
			return state, &Error{2, e}
		}
	}
	if !intent.New {
		return s.Store.Load(intent.RunID, time.Now())
	}
	if release == nil {
		release, e = s.Store.Lease(intent.RunID)
	}
	if e != nil {
		state, loadErr := s.Store.Load(intent.RunID, time.Now())
		if loadErr != nil {
			return state, &Error{125, errors.Join(e, loadErr)}
		}
		state.Final = true
		state.Reason = "not_submitted"
		state.Cleanup = "not_started"
		state.Targets = targets
		state.InvocationDigest = digest
		state.WorkspaceID, state.WorkflowID = source.WorkspaceID, source.WorkflowID
		now := time.Now().UTC()
		state.EndedAt = &now
		if saveErr := s.Store.Save(state, false, now); saveErr != nil {
			return state, &Error{125, errors.Join(e, saveErr)}
		}
		return state, &Error{2, e}
	}
	defer func() { err = errors.Join(err, release()) }()
	h, e := s.Engine.Start(ctx, engine.StartRequest{Source: source, RunID: intent.RunID, Targets: targets, Values: values, Mode: "task", InvocationDigest: digest, RequestID: req.RequestID, Deadline: time.Now().Add(req.Timeout)})
	if e != nil {
		state, _ = s.Store.Load(intent.RunID, time.Now())
		state.Final = true
		state.Reason = "start_failed"
		state.OperationError = output.Clean(e.Error(), 512)
		code := 125
		var rejection *engine.StartRejection
		if errors.As(e, &rejection) {
			state.Reason = "validation_failed"
			state.Cleanup = "not_started"
			code = 2
		}
		if errors.Is(e, context.DeadlineExceeded) {
			state.Reason = "deadline"
			state.Cleanup = "not_started"
			code = 124
		}
		if ctx.Err() != nil {
			markCancelled(ctx, &state)
			code = state.CancellationCode
		}
		now := time.Now().UTC()
		state.EndedAt = &now
		saveErr := s.Store.Save(state, false, now)
		return state, &Error{code, errors.Join(e, saveErr)}
	}
	var stopOnce sync.Once
	var stopErr error
	stopOwned := func() error {
		stopOnce.Do(func() { _, stopErr = s.Engine.Stop(context.Background(), h) })
		return stopErr
	}
	defer func() {
		if !state.Final {
			err = errors.Join(err, stopOwned())
		}
	}()
	state, e = s.Engine.Snapshot(context.Background(), h)
	if e != nil {
		return state, &Error{125, e}
	}
	state.EngineSHA256 = sha
	events, e := s.Engine.Subscribe(context.Background(), h)
	if e != nil {
		stopOwned()
		return state, &Error{125, e}
	}
	cancelled := false
	ctxDone := ctx.Done()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	hard := time.NewTimer(req.Timeout + 11*time.Second)
	defer hard.Stop()
	truncated := false
	var last string
	appendEvent := func(event engine.Event) error {
		cut, e := s.Store.Append(intent.RunID, event)
		truncated = truncated || cut
		return e
	}
	for {
		select {
		case event, ok := <-events:
			if !ok {
				events = nil
			} else if e = appendEvent(event); e != nil {
				return state, &Error{125, errors.Join(e, stopOwned())}
			}
		case <-ctxDone:
			cancelled = true
			ctxDone = nil
			if e = stopOwned(); e != nil {
				latest, snapshotErr := s.Engine.Snapshot(context.Background(), h)
				if snapshotErr == nil {
					state = latest
				}
				markCancelled(ctx, &state)
				state.StopObservation = "timed_out"
				state.Cleanup = "unknown"
				saveErr := s.Store.Save(state, req.RetainEvidence, time.Now())
				return state, &Error{state.CancellationCode, errors.Join(e, snapshotErr, saveErr)}
			}
		case <-hard.C:
			e = stopOwned()
			state, _ = s.Engine.Snapshot(context.Background(), h)
			if state.Reason != "deadline" {
				state.Reason = "stop_observation_timeout"
			}
			state.StopObservation = "timed_out"
			state.Cleanup = "unknown"
			saveErr := s.Store.Save(state, req.RetainEvidence, time.Now())
			return state, &Error{125, errors.Join(fmt.Errorf("run lifetime exceeded observation budget"), e, saveErr)}
		case <-tick.C:
		}
		state, e = s.Engine.Snapshot(context.Background(), h)
		if e != nil {
			return state, &Error{125, e}
		}
		state.EngineSHA256 = sha
		if cancelled {
			markCancelled(ctx, &state)
		}
		if truncated {
			state.HistoryIntegrity = "truncated"
		}
		if state.Final {
			for events != nil {
				select {
				case event, ok := <-events:
					if !ok {
						events = nil
					} else if e = appendEvent(event); e != nil {
						return state, &Error{125, e}
					}
				default:
					events = nil
				}
			}
			if truncated {
				state.HistoryIntegrity = "truncated"
			}
		}
		raw, _ := json.Marshal(state)
		if string(raw) != last {
			if e = s.Store.Save(state, req.RetainEvidence, time.Now()); e != nil {
				stopOwned()
				return state, &Error{125, e}
			}
			last = string(raw)
		}
		if state.Final {
			return state, nil
		}
	}
}

func ExitCode(s engine.Snapshot) int {
	if s.CancellationCode >= 128 && s.CancellationCode <= 192 {
		return s.CancellationCode
	}
	if s.Reason == "not_submitted" || s.Reason == "validation_failed" && s.Cleanup == "not_started" {
		return 2
	}
	if s.Reason == "cancelled" {
		if s.CancellationCode >= 128 && s.CancellationCode <= 192 {
			return s.CancellationCode
		}
		return 130
	}
	if s.Reason == "deadline" {
		return 124
	}
	if !s.Final || s.TaskResult == "unknown" {
		return 125
	}
	if s.TaskResult == "failed" {
		return 1
	}
	if s.EngineExitCode == nil || *s.EngineExitCode != 0 || s.Cleanup != "complete" || s.IOStatus != "complete" {
		return 125
	}
	return 0
}
