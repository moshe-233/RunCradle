package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runcradle/internal/engine"
	"runcradle/internal/runs"
	"runcradle/internal/workflow"
	"sort"
	"time"
)

// preparedRun 由请求校验创建；只包含绑定后的来源和规范化参数，不持有运行资源。
type preparedRun struct {
	request Request
	source  engine.WorkflowSource
	targets []string
	values  map[string]json.RawMessage
}

// acceptedRun 只能由 Host 受理后创建；移交执行器后，由执行器负责释放槽位。
type acceptedRun struct {
	preparedRun
	intent                      runs.Intent
	initial                     engine.Snapshot
	engineSHA, invocationDigest string
	release                     func() error
}

func prepareRun(req Request) (preparedRun, error) {
	if req.RequestID == "" || len(req.RequestID) > 128 || req.Timeout < time.Millisecond || req.Timeout > time.Hour || len(req.Targets) == 0 || !req.Document.Valid() {
		return preparedRun{}, fmt.Errorf("request_id, valid definition, explicit targets and timeout 1..3600000ms required")
	}
	source, e := engine.BindSource(engine.WorkflowSource{Document: req.Document, WorkflowID: req.WorkflowID, Managed: req.Managed})
	if e != nil {
		return preparedRun{}, e
	}
	targets := append([]string(nil), req.Targets...)
	sort.Strings(targets)
	names := map[string]bool{}
	for _, n := range req.Document.View.Nodes {
		names[n.Name] = true
	}
	for i, n := range targets {
		if !names[n] || i > 0 && targets[i-1] == n {
			return preparedRun{}, fmt.Errorf("invalid or duplicate target")
		}
	}
	values, e := workflow.ResolveValues(req.Document, req.Preset, req.Values)
	if e != nil {
		return preparedRun{}, e
	}

	return preparedRun{request: req, source: source, targets: targets, values: values}, nil
}

// runIdentity 将幂等判断所需的持久身份与本次请求关联，不赋予进程控制权。
type runIdentity struct {
	previous  *runs.Intent
	engineSHA string
	record    Invocation
	digest    string
}

func (h *Host) identifyRun(prepared preparedRun) (runIdentity, error) {
	var identity runIdentity
	existing, err := h.Store.Request(prepared.request.RequestID)
	if err == nil {
		identity.previous = &existing
		raw, readErr := h.Store.Invocation(existing.RunID)
		if readErr == nil {
			var previous Invocation
			if err = json.Unmarshal(raw, &previous); err != nil {
				return identity, &Error{2, err}
			}
			identity.engineSHA = previous.EngineSHA
		} else if !errors.Is(readErr, os.ErrNotExist) {
			return identity, &Error{2, readErr}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return identity, &Error{2, err}
	}
	// 重放必须沿用原引擎指纹：当前引擎已删除，也不能让同一请求重新执行。
	if identity.engineSHA == "" {
		identity.engineSHA, err = fingerprint(h.Binary)
		if err != nil {
			if identity.previous != nil {
				return identity, &Error{125, fmt.Errorf("submission is uncertain; inspect the original request")}
			}
			return identity, &Error{2, err}
		}
	}
	identity.record, identity.digest = invocation(prepared.request, prepared.source, prepared.targets, prepared.values, identity.engineSHA)
	return identity, nil
}

func (h *Host) replayRun(identity runIdentity, requestID string) (Submission, error) {
	previous := identity.previous
	if previous.Digest != identity.digest {
		return Submission{}, &Error{2, runs.ErrConflict}
	}
	state, err := h.Store.Load(previous.RunID, time.Now())
	sub := Submission{RunID: previous.RunID, RequestID: requestID, Revision: previous.Revision}
	if err != nil {
		return sub, &Error{125, err}
	}
	if state.Reason == "" || state.Reason == "submission_unknown" {
		return sub, &Error{125, fmt.Errorf("submission acceptance is unknown; inspect the original request before retrying")}
	}
	sub.Accepted = state.Reason != "not_submitted"
	if !sub.Accepted {
		return sub, &Error{2, fmt.Errorf("original request was not submitted; use a new request after capacity is available")}
	}
	return sub, nil
}

func (h *Host) admitRun(ctx context.Context, prepared preparedRun, identity runIdentity) (Submission, *acceptedRun, error) {
	req := prepared.request
	if _, err := workflow.ApplyValuesContext(ctx, req.Document, prepared.values); err != nil {
		return Submission{}, nil, &Error{2, err}
	}
	if ctx.Err() != nil {
		return Submission{}, nil, &Error{2, ctx.Err()}
	}
	intent, err := h.Store.Begin(ctx, req.RequestID, identity.digest, req.Document)
	if err != nil {
		return Submission{}, nil, &Error{2, err}
	}
	sub := Submission{RunID: intent.RunID, RequestID: req.RequestID, Revision: req.Document.Revision, Accepted: true}
	// Begin 也会在锁内去重；另一个请求先登记时，本调用不取得槽位或启动 worker。
	if !intent.New {
		identity.previous = &intent
		sub, err = h.replayRun(identity, req.RequestID)
		return sub, nil, err
	}
	raw, err := json.Marshal(identity.record)
	if err == nil {
		err = h.Store.SaveInvocation(intent.RunID, raw)
	}
	if err != nil {
		return sub, nil, &Error{125, err}
	}
	release, leaseErr := h.Store.Lease(intent.RunID)
	state, err := h.Store.Load(intent.RunID, time.Now())
	if err != nil {
		if release != nil {
			release()
		}
		return sub, nil, &Error{125, err}
	}
	state.WorkflowID, state.WorkspaceID, state.Targets = prepared.source.WorkflowID, prepared.source.WorkspaceID, prepared.targets
	state.Values, state.EngineSHA256, state.InvocationDigest = prepared.values, identity.engineSHA, identity.digest
	if leaseErr != nil {
		state.Final, state.Reason, state.Cleanup = true, "not_submitted", "not_started"
		now := time.Now().UTC()
		state.EndedAt = &now
		sub.Accepted = false
		return sub, nil, &Error{2, errors.Join(leaseErr, h.Store.Save(state, false, now))}
	}
	state.Reason = "accepted"
	if err = h.Store.Save(state, req.RetainEvidence, time.Now()); err != nil {
		release()
		return sub, nil, &Error{125, err}
	}
	return sub, &acceptedRun{preparedRun: prepared, intent: intent, initial: state, engineSHA: identity.engineSHA, invocationDigest: identity.digest, release: release}, nil
}
