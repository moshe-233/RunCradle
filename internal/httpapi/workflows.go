package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	pc "runcradle/internal/engine/processcompose"
	"runcradle/internal/runs"
	"runcradle/internal/webui"
	"runcradle/internal/workflow"
	"strconv"
	"time"
)

type Draft struct {
	Format   string  `json:"format"`
	Source   string  `json:"source"`
	UISource *string `json:"ui_source,omitempty"`
	Entry    string  `json:"entry,omitempty"`
	UIPath   string  `json:"ui_path,omitempty"`
}
type draftRequest struct {
	Draft
	ExpectedRevision string                  `json:"expected_revision,omitempty"`
	Edit             *workflow.EditRequest   `json:"edit,omitempty"`
	WorkbenchEdit    *workflow.WorkbenchEdit `json:"workbench_edit,omitempty"`
}

func (s *Server) document(input Draft) (workflow.Document, error) {
	if input.Entry == "" {
		input.Entry = "process-compose." + input.Format
	}
	var ui []byte
	if input.UISource != nil {
		ui = []byte(*input.UISource)
		if input.UIPath == "" {
			input.UIPath = "ui.json"
		}
	} else {
		input.UIPath = ""
	}
	return workflow.FromBytes(s.workspace, input.Entry, input.UIPath, input.Format, []byte(input.Source), ui)
}
func documentDraft(d workflow.Document) Draft {
	v := Draft{Format: d.Format, Source: string(d.Source), Entry: d.Entry, UIPath: d.UIPath}
	if d.UIPath != "" {
		text := string(d.UIRaw)
		v.UISource = &text
	}
	return v
}
func (s *Server) documentReply(d workflow.Document, record *runs.WorkflowRecord) any {
	display, _ := webui.Display(d)
	supported := false
	reason := "static validation failed"
	if d.Valid() {
		_, e := pc.Prepare(d, "/not-executed/runcradle", "preview", "/not-executed/receipts")
		supported = e == nil
		if e == nil {
			reason = "declared subset only; upstream acceptance and actual execution are checked on run"
		} else {
			reason = e.Error()
		}
	}
	return struct {
		Draft
		Revision  string               `json:"revision"`
		Valid     bool                 `json:"valid"`
		Issues    []workflow.Issue     `json:"issues"`
		Display   json.RawMessage      `json:"display"`
		Workflow  *runs.WorkflowRecord `json:"workflow,omitempty"`
		Supported bool                 `json:"run_supported"`
		Reason    string               `json:"execution_note"`
	}{documentDraft(d), d.Revision, d.Valid(), d.Issues, display, record, supported, reason}
}
func apiError(w http.ResponseWriter, e error) {
	status := 400
	if errors.Is(e, runs.ErrConflict) {
		status = 409
	} else if errors.Is(e, os.ErrNotExist) {
		status = 404
	}
	fail(w, status, e)
}
func paging(r *http.Request) (int, int, error) {
	offset, limit := 0, 20
	var e error
	if v := r.URL.Query().Get("offset"); v != "" {
		offset, e = strconv.Atoi(v)
		if e != nil {
			return 0, 0, e
		}
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, e = strconv.Atoi(v)
	}
	if e != nil || offset < 0 || limit < 1 || limit > 100 {
		return 0, 0, fmt.Errorf("offset >=0 and limit 1..100 required")
	}
	return offset, limit, nil
}
func (s *Server) routes(m *http.ServeMux, cfg Config) {
	m.HandleFunc("GET /api/v1/workspace-files", s.listWorkspaceFiles)
	if cfg.Page != nil {
		m.Handle("GET /", cfg.Page)
	}
	m.HandleFunc("POST /api/v1/session", s.pair)
	m.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, map[string]string{"status": "ready", "instance_id": s.descriptor.InstanceID})
	})
	m.HandleFunc("GET /api/v1/workflows", func(w http.ResponseWriter, r *http.Request) {
		offset, limit, e := paging(r)
		if e != nil {
			apiError(w, e)
			return
		}
		items, more, e := s.Store.ListWorkflows(offset, limit)
		if e != nil {
			apiError(w, e)
			return
		}
		reply(w, 200, map[string]any{"workflows": items, "has_more": more, "next_offset": offset + len(items)})
	})
	m.HandleFunc("POST /api/v1/imports", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			File string `json:"file"`
			UI   string `json:"ui,omitempty"`
		}
		if e := readJSON(w, r, &input); e != nil {
			apiError(w, e)
			return
		}
		d, e := workflow.Load(s.workspace, input.File, input.UI)
		if e != nil {
			apiError(w, e)
			return
		}
		reply(w, 200, s.documentReply(d, nil))
	})
	m.HandleFunc("POST /api/v1/validate", func(w http.ResponseWriter, r *http.Request) {
		var input draftRequest
		if e := readJSON(w, r, &input); e != nil {
			apiError(w, e)
			return
		}
		if input.Edit != nil && input.WorkbenchEdit != nil {
			apiError(w, fmt.Errorf("edit and workbench_edit are mutually exclusive"))
			return
		}
		d, e := s.document(input.Draft)
		if e == nil && input.WorkbenchEdit != nil {
			d, e = workflow.ApplyWorkbenchEdit(d, *input.WorkbenchEdit)
		}
		if e == nil && input.Edit != nil {
			d, e = workflow.Edit(d, *input.Edit)
		}
		if e != nil {
			apiError(w, e)
			return
		}
		reply(w, 200, s.documentReply(d, nil))
	})
	m.HandleFunc("POST /api/v1/render", func(w http.ResponseWriter, r *http.Request) {
		var input Draft
		if e := readJSON(w, r, &input); e != nil {
			apiError(w, e)
			return
		}
		d, e := s.document(input)
		if e != nil {
			apiError(w, e)
			return
		}
		page, e := webui.Render(d, time.Now())
		if e != nil {
			apiError(w, e)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=workflow.html")
		_, _ = w.Write(page)
	})
	m.HandleFunc("POST /api/v1/workflows", func(w http.ResponseWriter, r *http.Request) {
		var input Draft
		if e := readJSON(w, r, &input); e != nil {
			apiError(w, e)
			return
		}
		d, e := s.document(input)
		if e != nil {
			apiError(w, e)
			return
		}
		record, e := s.Store.CreateWorkflow(r.Context(), d)
		if e != nil {
			apiError(w, e)
			return
		}
		reply(w, 201, s.documentReply(d, &record))
	})
	m.HandleFunc("GET /api/v1/workflows/{id}", func(w http.ResponseWriter, r *http.Request) {
		record, d, e := s.Store.Workflow(r.PathValue("id"), r.URL.Query().Get("revision"))
		if e != nil {
			apiError(w, e)
			return
		}
		reply(w, 200, s.documentReply(d, &record))
	})
	m.HandleFunc("PUT /api/v1/workflows/{id}", func(w http.ResponseWriter, r *http.Request) {
		var input draftRequest
		if e := readJSON(w, r, &input); e != nil {
			apiError(w, e)
			return
		}
		if input.ExpectedRevision == "" {
			apiError(w, fmt.Errorf("expected_revision is required"))
			return
		}
		d, e := s.document(input.Draft)
		if e != nil {
			apiError(w, e)
			return
		}
		record, e := s.Store.SaveWorkflow(r.Context(), r.PathValue("id"), input.ExpectedRevision, d)
		if e != nil {
			apiError(w, e)
			return
		}
		reply(w, 200, s.documentReply(d, &record))
	})
	m.HandleFunc("POST /api/v1/workflows/{id}/copy", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Revision string `json:"revision"`
		}
		if e := readJSON(w, r, &input); e != nil {
			apiError(w, e)
			return
		}
		record, e := s.Store.CopyWorkflow(r.Context(), r.PathValue("id"), input.Revision)
		if e != nil {
			apiError(w, e)
			return
		}
		_, d, e := s.Store.Workflow(record.ID, record.Revision)
		if e != nil {
			apiError(w, e)
			return
		}
		reply(w, 201, s.documentReply(d, &record))
	})
	m.HandleFunc("GET /api/v1/workflows/{id}/export", func(w http.ResponseWriter, r *http.Request) {
		raw, e := s.Store.ExportWorkflow(r.PathValue("id"), r.URL.Query().Get("revision"))
		if e != nil {
			apiError(w, e)
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", "attachment; filename=runcradle-config.zip")
		_, _ = w.Write(raw)
	})
	m.HandleFunc("GET /api/v1/capabilities", func(w http.ResponseWriter, r *http.Request) {
		caps, e := s.Host.NewEngine().Capabilities(r.Context())
		if e != nil {
			reply(w, 200, map[string]any{"engine_available": false, "reason": e.Error(), "mode": "task"})
			return
		}
		reply(w, 200, map[string]any{"engine_available": true, "engine": caps, "mode": "task", "max_active_runs": 4, "active_runs": s.Host.ActiveRuns()})
	})
	s.runRoutes(m)
}
