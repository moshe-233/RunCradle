package service

import (
	"context"
	"errors"
	"runcradle/internal/engine"
	"runcradle/internal/runs"
	"sync"
	"time"
)

// runExecutor 只接收已受理运行；不会登记请求或再次解析参数。
type runExecutor struct {
	engine engine.Engine
	store  *runs.Store
}

func (s *runExecutor) run(ctx context.Context, accepted acceptedRun) (state engine.Snapshot, err error) {
	// 槽位从 Host 移交到此处；即使 worker 启动前已取消，也必须释放。
	defer func() { err = errors.Join(err, accepted.release()) }()
	req, intent := accepted.request, accepted.intent
	sha, digest := accepted.engineSHA, accepted.invocationDigest
	source, targets, values := accepted.source, accepted.targets, accepted.values
	if ctx.Err() != nil {
		return s.cancelBeforeStart(ctx, accepted)
	}
	handle, e := s.engine.Start(ctx, engine.StartRequest{Source: source, RunID: intent.RunID, Targets: targets, Values: values, Mode: "task", InvocationDigest: digest, RequestID: req.RequestID, Deadline: time.Now().Add(req.Timeout)})
	if e != nil {
		return s.startFailure(ctx, accepted, e)
	}
	var stopOnce sync.Once
	var stopErr error
	stopOwned := func() error {
		stopOnce.Do(func() { _, stopErr = s.engine.Stop(context.Background(), handle) })
		return stopErr
	}
	defer func() {
		if !state.Final {
			err = errors.Join(err, stopOwned())
		}
	}()
	state, e = s.engine.Snapshot(context.Background(), handle)
	if e != nil {
		return state, &Error{125, e}
	}
	state.EngineSHA256 = sha
	events, e := s.engine.Subscribe(context.Background(), handle)
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
	recorder := runRecorder{store: s.store, runID: intent.RunID, retain: req.RetainEvidence}
	for {
		select {
		case event, ok := <-events:
			if !ok {
				events = nil
			} else if e = recorder.append(event); e != nil {
				return state, &Error{125, errors.Join(e, stopOwned())}
			}
		case <-ctxDone:
			cancelled = true
			ctxDone = nil
			if e = stopOwned(); e != nil {
				return s.cancelObservation(ctx, accepted, handle, state, e)
			}
		case <-hard.C:
			return s.observationTimeout(accepted, handle, stopOwned())
		case <-tick.C:
		}
		state, e = s.engine.Snapshot(context.Background(), handle)
		if e != nil {
			return state, &Error{125, e}
		}
		state.EngineSHA256 = sha
		if cancelled {
			markCancelled(ctx, &state)
		}
		if recorder.truncated {
			state.HistoryIntegrity = "truncated"
		}
		if state.Final {
			if e = recorder.drain(events); e != nil {
				return state, &Error{125, e}
			}
			events = nil
			if recorder.truncated {
				state.HistoryIntegrity = "truncated"
			}
		}
		if e = recorder.save(state); e != nil {
			stopOwned()
			return state, &Error{125, e}
		}
		if state.Final {
			return state, nil
		}
	}
}
