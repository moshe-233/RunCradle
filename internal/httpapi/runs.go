package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runcradle/internal/engine"
	"runcradle/internal/output"
	"runcradle/internal/protocol"
	"runcradle/internal/runs"
	"runcradle/internal/service"
	"runcradle/internal/workflow"
	"strconv"
	"time"
)

type RunRequest = protocol.RunRequest

func (s *Server) snapshotReply(state engine.Snapshot) any {
	raw, _ := json.MarshalIndent(state.Values, "", "  ")
	return struct {
		engine.Snapshot
		ValuesText string `json:"values_text"`
		Owned      bool   `json:"owned"`
	}{state, string(raw), s.Host.OwnsRun(state.RunID)}
}

func (s *Server) runRoutes(m *http.ServeMux) {
	m.HandleFunc("POST /api/v1/runs", func(w http.ResponseWriter, r *http.Request) {
		var input RunRequest
		if e := readJSON(w, r, &input); e != nil {
			apiError(w, e)
			return
		}
		if input.Mode != "" && input.Mode != "task" || input.Revision == "" || (input.Document == nil) == (input.WorkflowID == "") {
			apiError(w, fmt.Errorf("choose managed workflow or frozen document, fixed revision and task mode"))
			return
		}
		var d workflow.Document
		var e error
		if input.Document != nil {
			d, e = s.document(*input.Document)
		} else {
			_, d, e = s.Store.Workflow(input.WorkflowID, input.Revision)
		}
		if e != nil {
			apiError(w, e)
			return
		}
		if d.Revision != input.Revision {
			apiError(w, runs.ErrConflict)
			return
		}
		if input.TimeoutMS == 0 {
			input.TimeoutMS = 300000
		}
		if input.TimeoutMS < 1 || input.TimeoutMS > 3600000 {
			apiError(w, fmt.Errorf("timeout_ms must be 1..3600000"))
			return
		}
		sub, e := s.Host.StartRun(r.Context(), service.Request{Document: d, WorkflowID: input.WorkflowID, Managed: input.WorkflowID != "", RequestID: input.RequestID, Targets: input.Targets, Timeout: time.Duration(input.TimeoutMS) * time.Millisecond, Preset: input.Preset, Values: input.Values, RetainEvidence: input.Retain})
		if e != nil {
			status := 400
			if errors.Is(e, runs.ErrConflict) {
				status = 409
			} else if service.Code(e) == 125 {
				status = 500
			} else if sub.RunID != "" && !sub.Accepted {
				status = 429
			}
			reply(w, status, struct {
				service.Submission
				protocol.Problem
			}{sub, protocol.ProblemFor(status, output.Clean(e.Error(), 512))})
			return
		}
		reply(w, 202, sub)
	})
	m.HandleFunc("GET /api/v1/runs", func(w http.ResponseWriter, r *http.Request) {
		offset, limit, e := paging(r)
		if e != nil {
			apiError(w, e)
			return
		}
		items, more, e := s.Store.ListRuns(r.URL.Query().Get("workflow_id"), offset, limit)
		if e != nil {
			apiError(w, e)
			return
		}
		reply(w, 200, map[string]any{"runs": items, "has_more": more, "next_offset": offset + len(items)})
	})
	m.HandleFunc("GET /api/v1/run-requests/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" || len(id) > 128 {
			apiError(w, fmt.Errorf("invalid request_id"))
			return
		}
		intent, e := s.Store.Request(id)
		if e != nil {
			apiError(w, e)
			return
		}
		state, e := s.Host.InspectRun(intent.RunID)
		if e != nil {
			apiError(w, e)
			return
		}
		reply(w, 200, map[string]any{"run_id": intent.RunID, "request_id": id, "revision": intent.Revision, "snapshot": s.snapshotReply(state)})
	})
	m.HandleFunc("GET /api/v1/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		state, e := s.Host.InspectRun(r.PathValue("id"))
		if e != nil {
			apiError(w, e)
			return
		}
		reply(w, 200, s.snapshotReply(state))
	})
	m.HandleFunc("GET /api/v1/runs/{id}/definition", func(w http.ResponseWriter, r *http.Request) {
		state, e := s.Host.InspectRun(r.PathValue("id"))
		if e != nil {
			apiError(w, e)
			return
		}
		d, e := s.Store.Definition(state.Revision)
		if e != nil {
			apiError(w, e)
			return
		}
		reply(w, 200, s.documentReply(d, nil))
	})
	m.HandleFunc("GET /api/v1/runs/{id}/invocation", func(w http.ResponseWriter, r *http.Request) {
		raw, e := s.Store.Invocation(r.PathValue("id"))
		if e != nil {
			apiError(w, e)
			return
		}
		reply(w, 200, raw)
	})
	m.HandleFunc("POST /api/v1/runs/{id}/stop", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Code int `json:"cancellation_code,omitempty"`
		}
		if e := readJSON(w, r, &input); e != nil {
			apiError(w, e)
			return
		}
		if input.Code == 0 {
			input.Code = 130
		}
		result, e := s.Host.StopRun(r.PathValue("id"), input.Code)
		if e != nil {
			apiError(w, e)
			return
		}
		reply(w, 202, result)
	})
	m.HandleFunc("GET /api/v1/runs/{id}/events", s.events)
}
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	state, e := s.Host.InspectRun(id)
	if e != nil {
		apiError(w, e)
		return
	}
	text := r.URL.Query().Get("after_seq")
	if text == "" {
		text = r.Header.Get("Last-Event-ID")
	}
	after := uint64(0)
	if text != "" {
		after, e = strconv.ParseUint(text, 10, 64)
		if e != nil {
			apiError(w, e)
			return
		}
	}
	if r.Header.Get("Accept") != "text/event-stream" {
		_, limit, e := paging(r)
		if e != nil {
			apiError(w, e)
			return
		}
		events, next, gap, e := s.Store.Events(id, after, limit)
		if e != nil {
			reply(w, 200, map[string]any{"events": events, "next_seq": next, "gap": true, "error": e.Error()})
			return
		}
		reply(w, 200, map[string]any{"events": events, "next_seq": next, "gap": gap})
		return
	}
	select {
	case s.streams <- struct{}{}:
		defer func() { <-s.streams }()
	default:
		fail(w, 429, fmt.Errorf("SSE subscriber capacity reached"))
		return
	}
	s.mu.Lock()
	if s.subscribers[id] >= 8 {
		s.mu.Unlock()
		fail(w, 429, fmt.Errorf("run has eight subscribers"))
		return
	}
	s.subscribers[id]++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.subscribers[id]--
		if s.subscribers[id] == 0 {
			delete(s.subscribers, id)
		}
		s.mu.Unlock()
	}()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	send := func(kind string, seq uint64, v any) error {
		raw, e := json.Marshal(v)
		if e != nil {
			return e
		}
		if e = controller.SetWriteDeadline(time.Now().Add(2 * time.Second)); e != nil && !errors.Is(e, http.ErrNotSupported) {
			return e
		}
		if seq > 0 {
			if _, e = fmt.Fprintf(w, "id: %d\n", seq); e != nil {
				return e
			}
		}
		if kind != "" {
			if _, e = fmt.Fprintf(w, "event: %s\n", kind); e != nil {
				return e
			}
		}
		if _, e = fmt.Fprintf(w, "data: %s\n\n", raw); e != nil {
			return e
		}
		return controller.Flush()
	}
	if after > 0 {
		page, _, gap, e := s.Store.Events(id, after-1, 1)
		if e != nil || gap || len(page) == 0 || page[0].Seq != after {
			_ = send("gap", 0, map[string]string{"history_integrity": "gap", "reason": "requested cursor is unavailable"})
			after = 0
		}
	}
	if e = send("snapshot", 0, s.snapshotReply(state)); e != nil {
		return
	}
	if !state.Final && !s.Host.OwnsRun(id) {
		_ = send("interrupted", 0, s.snapshotReply(state))
		return
	}
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	stamp := int64(-1)
	heartbeat := time.Now()
	drain := func() error {
		next, gap, err := s.Store.WalkEvents(id, after, func(event engine.Event) error {
			return send("", event.Seq, event)
		})
		after = next
		if gap {
			_ = send("gap", 0, map[string]string{"history_integrity": "gap"})
		}
		return err
	}

	for {
		current, e := s.Store.EventStamp(id)
		if e != nil {
			_ = send("gap", 0, map[string]string{"history_integrity": "gap"})
			return
		}
		if current != stamp {
			if e = drain(); e != nil {
				return
			}
			stamp = current
		}
		state, e = s.Host.InspectRun(id)
		if e != nil {
			_ = send("gap", 0, map[string]string{"history_integrity": "gap"})
			return
		}
		if state.Final {
			// The terminal snapshot is published after the owner has appended its tail.
			if e = drain(); e != nil {
				return
			}
			_ = send("final", 0, s.snapshotReply(state))
			return
		}
		if !s.Host.OwnsRun(id) {
			_ = send("interrupted", 0, s.snapshotReply(state))
			return
		}
		if time.Since(heartbeat) >= 5*time.Second {
			if e = send("snapshot", 0, s.snapshotReply(state)); e != nil {
				return
			}
			heartbeat = time.Now()
		}
		select {
		case <-r.Context().Done():
			return
		case <-s.ctx.Done():
			return
		case <-tick.C:
		}
	}
}
