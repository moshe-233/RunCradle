package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runcradle/internal/engine"
	"runcradle/internal/protocol"
	"runcradle/internal/runs"
	"runcradle/internal/service"
	"strings"
	"testing"
	"time"
)

func TestWorkflowDeleteBoundaryCASAndReceipt(t *testing.T) {
	s := apiFixture(t)
	d, err := s.document(Draft{Format: "yaml", Source: "processes: {a: {command: echo ok}}"})
	if err != nil {
		t.Fatal(err)
	}
	item, err := s.Store.CreateWorkflow(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/workflows/" + item.ID
	body := map[string]any{"expected_revision": item.Revision}
	if code, raw := callAPI(t, s, "DELETE", path, body, false); code != 401 {
		t.Fatalf("auth: %d %s", code, raw)
	}
	for _, tc := range []struct {
		body   any
		status int
		code   string
	}{
		{nil, 400, "invalid_request"},
		{map[string]any{}, 400, "invalid_request"},
		{map[string]any{"expected_revision": "bad"}, 400, "invalid_request"},
		{map[string]any{"expected_revision": strings.ToUpper(item.Revision)}, 400, "invalid_request"},
		{map[string]any{"expected_revision": item.Revision, "unknown": true}, 400, "invalid_request"},
		{map[string]any{"expected_revision": strings.Repeat("0", 64)}, 409, "conflict"},
	} {
		code, raw := callAPI(t, s, "DELETE", path, tc.body, true)
		var problem protocol.Problem
		if err := json.Unmarshal(raw, &problem); err != nil || code != tc.status || problem.Code != tc.code {
			t.Fatalf("boundary %v: %d %s %v", tc.body, code, raw, err)
		}
	}
	req, err := http.NewRequest("DELETE", s.URL+path, strings.NewReader(`{"expected_revision":"`+item.Revision+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+s.descriptor.Token)
	req.Header.Set("Content-Type", "text/plain")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 415 {
		t.Fatalf("media: %d", res.StatusCode)
	}
	// An archive removed outside RunCradle must still leave a manageable record.
	if err = os.Remove(filepath.Join(s.workspace, ".runcradle/workflows", item.Revision, "definition.json")); err != nil {
		t.Fatal(err)
	}
	if code, raw := callAPI(t, s, "GET", "/api/v1/workflows", nil, true); code != 200 {
		t.Fatalf("dangling list: %d %s", code, raw)
	}
	code, raw := callAPI(t, s, "DELETE", path, body, true)
	var deleted runs.WorkflowDeletion
	if err := json.Unmarshal(raw, &deleted); err != nil || code != 200 || !deleted.Deleted || deleted.WorkflowID != item.ID || deleted.DeletedRevision != item.Revision {
		t.Fatalf("receipt: %d %s %v", code, raw, err)
	}
	for _, method := range []string{"GET", "DELETE"} {
		var input any
		if method == "DELETE" {
			input = body
		}
		if code, raw := callAPI(t, s, method, path, input, true); code != 404 {
			t.Fatalf("absent %s: %d %s", method, code, raw)
		}
	}
	if code, raw := callAPI(t, s, "DELETE", "/api/v1/workflows/invalid", body, true); code != 400 {
		t.Fatalf("invalid ID: %d %s", code, raw)
	}
}

func TestWorkflowDeleteLeavesActiveRunAndRunDefinitionAvailable(t *testing.T) {
	s := apiFixture(t)
	native := &journalEngine{}
	s.Host.NewEngine = func() engine.Engine { return native }
	d, err := s.document(Draft{Format: "yaml", Source: "processes: {a: {command: echo ok}}"})
	if err != nil {
		t.Fatal(err)
	}
	item, err := s.Store.CreateWorkflow(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	sub, err := s.Host.StartRun(context.Background(), service.Request{WorkflowID: item.ID, Managed: true, Document: d, Targets: []string{"a"}, RequestID: "delete-active-run", Timeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		state, err := s.Store.Load(sub.RunID, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if state.Reason == "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("worker never started: %+v", state)
		}
		time.Sleep(time.Millisecond)
	}
	if code, raw := callAPI(t, s, "DELETE", "/api/v1/workflows/"+item.ID, map[string]string{"expected_revision": item.Revision}, true); code != 200 {
		t.Fatalf("delete: %d %s", code, raw)
	}
	if s.Host.ActiveRuns() != 1 {
		t.Fatal("delete removed active run")
	}
	state, err := s.Store.Load(sub.RunID, time.Now())
	if err != nil || state.Final || state.Reason != "running" {
		t.Fatalf("delete stopped run: %+v %v", state, err)
	}
	for _, suffix := range []string{"", "/definition", "/invocation", "/events"} {
		if code, raw := callAPI(t, s, "GET", "/api/v1/runs/"+sub.RunID+suffix, nil, true); code != 200 {
			t.Fatalf("run %s unavailable: %d %s", suffix, code, raw)
		}
	}
	if code, raw := callAPI(t, s, "GET", "/api/v1/runs?workflow_id="+item.ID, nil, true); code != 200 || !strings.Contains(string(raw), sub.RunID) {
		t.Fatalf("history unavailable: %d %s", code, raw)
	}
}

func TestWorkflowDeleteProtocolAndSchema(t *testing.T) {
	found := false
	for _, feature := range protocol.Describe().Features {
		found = found || feature == "workflow_delete"
	}
	if !found {
		t.Fatal("workflow_delete feature missing")
	}
	spec := OpenAPI()
	op, ok := spec["paths"].(schema)["/api/v1/workflows/{id}"].(schema)["delete"].(schema)
	if !ok || op["operationId"] != "workflow.delete" {
		t.Fatal("delete operation missing")
	}
	schemas := spec["components"].(schema)["schemas"].(schema)
	input, ok := schemas["DeleteWorkflowRequest"].(schema)
	if !ok || input["additionalProperties"] != false {
		t.Fatal("strict delete request missing")
	}
	if field := input["properties"].(schema)["expected_revision"].(schema); field["pattern"] != "^[0-9a-f]{64}$" {
		t.Fatalf("revision shape: %+v", field)
	}
	if schemas["WorkflowDeletion"] == nil {
		t.Fatal("delete receipt schema missing")
	}
	// Unsupported methods must advertise DELETE among allowed operations.
	s := apiFixture(t)
	req, err := http.NewRequest("PATCH", s.URL+"/api/v1/workflows/"+strings.Repeat("a", 64), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+s.descriptor.Token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != 405 || !strings.Contains(res.Header.Get("Allow"), "DELETE") {
		t.Fatalf("allow: %d %s %s", res.StatusCode, res.Header.Get("Allow"), raw)
	}
}
