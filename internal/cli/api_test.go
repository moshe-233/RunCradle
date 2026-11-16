package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runcradle/internal/httpapi"
	"runcradle/internal/runs"
	"strings"
	"testing"
	"time"
)

func apiCommand(t *testing.T, args ...string) (int, map[string]json.RawMessage) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), append([]string{"api"}, args...), &stdout, &stderr)
	if stdout.Len() > 4096 || stderr.Len() != 0 {
		t.Fatalf("unbounded/mixed output: %d %s", stdout.Len(), &stderr)
	}
	var receipt map[string]json.RawMessage
	if err := json.Unmarshal(stdout.Bytes(), &receipt); err != nil {
		t.Fatalf("non-JSON receipt: %s (%v)", &stdout, err)
	}
	return code, receipt
}

func TestAPIConnectDeadlineKeepsTimeoutExitAndRemovesOutput(t *testing.T) {
	dir := t.TempDir()
	instance := strings.Repeat("b", 32)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(time.Second):
		}
		w.Header().Set("X-RunCradle-Instance", instance)
		w.Write([]byte(`{"status":"ready","instance_id":"` + instance + `"}`))
	}))
	defer s.Close()
	store, err := runs.Open(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	release, err := store.ClaimServer(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := store.PublishServer(runs.ServerDescriptor{Version: 1, URL: s.URL, Workspace: dir, InstanceID: instance, Token: strings.Repeat("c", 64)}); err != nil {
		t.Fatal(err)
	}
	code, receipt := apiCommand(t, "request", "--workspace", dir, "--path", "/api/v1/workflows", "--out", "timeout.json", "--timeout-ms", "10")
	if code != 124 || string(receipt["code"]) != `"deadline_exceeded"` {
		t.Fatalf("timeout misclassified: %d %v", code, receipt)
	}
	if _, err := os.Stat(filepath.Join(dir, "timeout.json")); !os.IsNotExist(err) {
		t.Fatalf("reserved output left after timeout: %v", err)
	}
}

func TestAPIDescribeAndOfflineSchema(t *testing.T) {
	dir := t.TempDir()
	code, receipt := apiCommand(t, "describe")
	if code != 0 || string(receipt["api_version"]) != `"v1"` {
		t.Fatalf("describe: %d %v", code, receipt)
	}
	code, receipt = apiCommand(t, "schema", "--workspace", dir, "--out", "openapi.json")
	raw, err := os.ReadFile(filepath.Join(dir, "openapi.json"))
	if code != 0 || err != nil || !bytes.Contains(raw, []byte(`"openapi":"3.1.1"`)) {
		t.Fatalf("schema: %d %v %v", code, receipt, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".runcradle")); !os.IsNotExist(err) {
		t.Fatal("offline schema started/created a host")
	}
}

func TestAPIRequestCoversLibraryAndPreservesStructuredFailures(t *testing.T) {
	dir := t.TempDir()
	s, err := httpapi.Open(httpapi.Config{Workspace: dir, Binary: "/bin/true", Helper: "/bin/true"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	draft := `{"format":"yaml","source":"processes: {a: {command: echo ok}}"}`
	if err := os.WriteFile(filepath.Join(dir, "draft.json"), []byte(draft), 0600); err != nil {
		t.Fatal(err)
	}
	code, receipt := apiCommand(t, "request", "--workspace", dir, "--method", "POST", "--path", "/api/v1/workflows", "--input", "draft.json", "--out", "created.json")
	if code != 0 || string(receipt["http_status"]) != "201" {
		t.Fatalf("create: %d %v", code, receipt)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "created.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Workflow struct {
			ID       string `json:"workflow_id"`
			Revision string `json:"revision"`
		} `json:"workflow"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil || doc.Workflow.ID == "" {
		t.Fatalf("document: %s %v", raw, err)
	}
	path := "/api/v1/workflows/" + doc.Workflow.ID
	update := strings.TrimSuffix(draft, "}") + `,"expected_revision":"stale"}`
	if err := os.WriteFile(filepath.Join(dir, "update.json"), []byte(update), 0600); err != nil {
		t.Fatal(err)
	}
	code, receipt = apiCommand(t, "request", "--workspace", dir, "--method", "PUT", "--path", path, "--input", "update.json", "--out", "conflict.json")
	if code != 2 || string(receipt["code"]) != `"conflict"` || string(receipt["http_status"]) != "409" {
		t.Fatalf("conflict: %d %v", code, receipt)
	}
	code, receipt = apiCommand(t, "request", "--workspace", dir, "--path", path+"/export", "--out", "export.zip")
	raw, err = os.ReadFile(filepath.Join(dir, "export.zip"))
	if code != 0 || err != nil || !bytes.HasPrefix(raw, []byte("PK")) {
		t.Fatalf("export: %d %v %v", code, receipt, err)
	}
	deleteBody := `{"expected_revision":"` + doc.Workflow.Revision + `"}`
	if err := os.WriteFile(filepath.Join(dir, "delete.json"), []byte(deleteBody), 0600); err != nil {
		t.Fatal(err)
	}
	code, receipt = apiCommand(t, "request", "--workspace", dir, "--method", "DELETE", "--path", path, "--out", "no-input.json")
	if code != 2 || string(receipt["code"]) != `"invalid_request"` {
		t.Fatalf("delete needs JSON input: %d %v", code, receipt)
	}
	code, receipt = apiCommand(t, "request", "--workspace", dir, "--method", "DELETE", "--path", path, "--input", "delete.json", "--out", "deleted.json")
	if code != 0 || string(receipt["http_status"]) != "200" {
		t.Fatalf("delete: %d %v", code, receipt)
	}
	raw, err = os.ReadFile(filepath.Join(dir, "deleted.json"))
	var deleted runs.WorkflowDeletion
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &deleted); err != nil || !deleted.Deleted || deleted.WorkflowID != doc.Workflow.ID || deleted.DeletedRevision != doc.Workflow.Revision {
		t.Fatalf("delete response: %s %v", raw, err)
	}
	code, receipt = apiCommand(t, "request", "--workspace", dir, "--method", "DELETE", "--path", path, "--input", "delete.json", "--out", "delete-absent.json")
	if code != 2 || string(receipt["http_status"]) != "404" || string(receipt["code"]) != `"not_found"` {
		t.Fatalf("repeated delete: %d %v", code, receipt)
	}
}

func TestAPIOutputMustBeReservedBeforeAnyWriteRequest(t *testing.T) {
	dir := t.TempDir()
	s, err := httpapi.Open(httpapi.Config{Workspace: dir, Binary: "/bin/true", Helper: "/bin/true"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := os.WriteFile(filepath.Join(dir, "input.json"), []byte(`{"format":"yaml","source":"processes: {a: {command: echo ok}}"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("input.json", filepath.Join(dir, "alias.json")); err != nil {
		t.Fatal(err)
	}
	for _, out := range []string{"input.json", "alias.json", "../escape.json", ".runcradle/forbidden.json"} {
		code, _ := apiCommand(t, "request", "--workspace", dir, "--method", "POST", "--path", "/api/v1/workflows", "--input", "input.json", "--out", out)
		if code != 2 {
			t.Fatalf("unsafe output %s: %d", out, code)
		}
	}
	items, _, err := s.Store.ListWorkflows(0, 20)
	if err != nil || len(items) != 0 {
		t.Fatalf("output preflight performed writes: %v %v", items, err)
	}
}

func TestAPIStatusDoesNotExposePrivateToken(t *testing.T) {
	dir := t.TempDir()
	s, err := httpapi.Open(httpapi.Config{Workspace: dir, Binary: "/bin/true", Helper: "/bin/true"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	code, receipt := apiCommand(t, "status", "--workspace", dir)
	raw, _ := json.Marshal(receipt)
	if code != 0 || string(receipt["url"]) != `"`+s.URL+`"` || bytes.Contains(raw, []byte(`"token"`)) || len(receipt["credential_file"]) == 0 {
		t.Fatalf("status: %d %s", code, raw)
	}
}

func TestAPIRequestKeepsLargeNativeSourceAndInt64Exactly(t *testing.T) {
	dir := t.TempDir()
	s, err := httpapi.Open(httpapi.Config{Workspace: dir, Binary: "/bin/true", Helper: "/bin/true"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	source := `{"processes":{"a":{"command":"echo ok","x-number":9223372036854775807,"x-note":"` + strings.Repeat("x", 12000) + `"}}}`
	input, _ := json.Marshal(map[string]string{"format": "json", "source": source})
	if err := os.WriteFile(filepath.Join(dir, "draft.json"), input, 0600); err != nil {
		t.Fatal(err)
	}
	code, receipt := apiCommand(t, "request", "--workspace", dir, "--method", "POST", "--path", "/api/v1/validate", "--input", "draft.json", "--out", "large.json")
	raw, err := os.ReadFile(filepath.Join(dir, "large.json"))
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Source string `json:"source"`
	}
	if err := json.Unmarshal(raw, &result); err != nil || code != 0 || result.Source != source || len(raw) < 4096 {
		t.Fatalf("large response lost: %d %v %v", code, receipt, err)
	}
}
