package service

import (
	"context"
	"errors"
	"fmt"
	"runcradle/internal/engine"
	"runcradle/internal/output"
	"time"
)

func (s *runExecutor) cancelBeforeStart(ctx context.Context, accepted acceptedRun) (state engine.Snapshot, err error) {
	state, err = s.store.Load(accepted.intent.RunID, time.Now())
	if err != nil {
		return state, &Error{125, err}
	}
	markCancelled(ctx, &state)
	state.Final, state.Cleanup = true, "not_started"
	now := time.Now().UTC()
	state.EndedAt = &now
	return state, &Error{state.CancellationCode, errors.Join(ctx.Err(), s.store.Save(state, accepted.request.RetainEvidence, now))}
}

// 未创建原生实例的拒绝，才能确认为 not_started；其他启动故障保留未知事实。
func (s *runExecutor) startFailure(ctx context.Context, accepted acceptedRun, e error) (state engine.Snapshot, err error) {
	state, _ = s.store.Load(accepted.intent.RunID, time.Now())
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
	saveErr := s.store.Save(state, false, now)
	return state, &Error{code, errors.Join(e, saveErr)}
}

// 取消码已知也不代表停止被证实；停止观察失败仍保留 cleanup=unknown。
func (s *runExecutor) cancelObservation(ctx context.Context, accepted acceptedRun, handle engine.RunHandle, state engine.Snapshot, cause error) (engine.Snapshot, error) {
	latest, snapshotErr := s.engine.Snapshot(context.Background(), handle)
	if snapshotErr == nil {
		state = latest
	}
	markCancelled(ctx, &state)
	state.StopObservation = "timed_out"
	state.Cleanup = "unknown"
	saveErr := s.store.Save(state, accepted.request.RetainEvidence, time.Now())
	return state, &Error{state.CancellationCode, errors.Join(cause, snapshotErr, saveErr)}
}
func (s *runExecutor) observationTimeout(accepted acceptedRun, handle engine.RunHandle, cause error) (engine.Snapshot, error) {
	state, _ := s.engine.Snapshot(context.Background(), handle)
	if state.Reason != "deadline" {
		state.Reason = "stop_observation_timeout"
	}
	state.StopObservation = "timed_out"
	state.Cleanup = "unknown"
	saveErr := s.store.Save(state, accepted.request.RetainEvidence, time.Now())
	return state, &Error{125, errors.Join(fmt.Errorf("run lifetime exceeded observation budget"), cause, saveErr)}
}
