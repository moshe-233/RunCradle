package service

import (
	"context"
	"encoding/json"
	"errors"
	"runcradle/internal/engine"
	"runcradle/internal/workflow"
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

type Request struct {
	WorkflowID     string
	Managed        bool
	Preset         string
	Values         map[string]json.RawMessage
	Document       workflow.Document
	Targets        []string
	Timeout        time.Duration
	RequestID      string
	RetainEvidence bool
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
