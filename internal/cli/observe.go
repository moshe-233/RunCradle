package cli

import (
	"context"
	"fmt"
	"net/url"
	"runcradle/internal/engine"
	"runcradle/internal/hostclient"
	"runcradle/internal/service"
	"time"
)

// 观察超时只结束客户端等待；只有收到显式取消才请求宿主停止任务。
func (result *remoteResult) observe(ctx context.Context, client *hostclient.Client, timeout int) int {
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	hard := time.NewTimer(time.Duration(timeout)*time.Millisecond + 25*time.Second)
	defer hard.Stop()
	for {
		var observed struct {
			engine.Snapshot
			Owned bool `json:"owned"`
		}
		op, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := client.Do(op, "GET", "/api/v1/runs/"+url.PathEscape(result.runID), nil, &observed)
		cancel()
		state := observed.Snapshot
		if err != nil && ctx.Err() == nil {
			return result.fail(125, err)
		}
		if ctx.Err() != nil {
			return result.cancelRun(ctx, client, state, tick.C)
		}
		if state.Final || state.Reason == "interrupted" || state.Reason == "owner_interrupted" {
			return result.write(runtimeSummary(state, result.requestID), service.ExitCode(state))
		}
		if !observed.Owned {
			summary := runtimeSummary(state, result.requestID)
			summary.Message = "submission has no live owner; inspect this run before intentional retry"
			return result.write(summary, 125)
		}
		select {
		case <-ctx.Done():
		case <-tick.C:
		case <-hard.C:
			return result.fail(125, fmt.Errorf("client observation exceeded budget; host still owns accepted run"))
		}
	}
}

func (result *remoteResult) cancelRun(ctx context.Context, client *hostclient.Client, state engine.Snapshot, tick <-chan time.Time) int {
	var err error
	code := CancellationCode(ctx)
	op, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	var stop engine.StopResult
	err = client.Do(op, "POST", "/api/v1/runs/"+url.PathEscape(result.runID)+"/stop", map[string]int{"cancellation_code": code}, &stop)
	cancel()
	if err != nil {
		return result.fail(code, fmt.Errorf("stop unconfirmed; inspect run: %w", err))
	}
	deadline := time.NewTimer(11 * time.Second)
	defer deadline.Stop()
	for {
		op, cancel := context.WithTimeout(context.Background(), time.Second)
		err = client.Do(op, "GET", "/api/v1/runs/"+url.PathEscape(result.runID), nil, &state)
		cancel()
		if err == nil && state.Final {
			return result.write(runtimeSummary(state, result.requestID), code)
		}
		select {
		case <-deadline.C:
			return result.fail(code, fmt.Errorf("stop observation incomplete; inspect run"))
		case <-tick:
		}
	}
}
