package service

import (
	"encoding/json"
	"runcradle/internal/engine"
	"runcradle/internal/runs"
	"time"
)

// runRecorder 保留事件截断事实；快照独立保存，不能用完整快照掩盖历史缺口。
type runRecorder struct {
	store             *runs.Store
	runID             string
	retain, truncated bool
	lastSnapshot      string
}

func (r *runRecorder) append(event engine.Event) error {
	cut, err := r.store.Append(r.runID, event)
	r.truncated = r.truncated || cut
	return err
}
func (r *runRecorder) drain(events <-chan engine.Event) error {
	// 终态出现后仍保存已经入队的尾部事件，但不无限等待上游关闭。
	for events != nil {
		select {
		case event, ok := <-events:
			if !ok {
				return nil
			}
			if err := r.append(event); err != nil {
				return err
			}
		default:
			return nil
		}
	}
	return nil
}
func (r *runRecorder) save(state engine.Snapshot) error {
	raw, _ := json.Marshal(state)
	if string(raw) == r.lastSnapshot {
		return nil
	}
	if err := r.store.Save(state, r.retain, time.Now()); err != nil {
		return err
	}
	r.lastSnapshot = string(raw)
	return nil
}
