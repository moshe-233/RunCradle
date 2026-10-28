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
	"runcradle/internal/workflow"
	"strings"
	"testing"
	"time"
)

func TestUnsupportedStoredDefinitionReturnsConfigurationExitWithoutStarting(t *testing.T) {
	dir := t.TempDir()
	d, e := workflow.FromBytes(dir, "pc.yaml", "", "yaml", []byte("processes: {a: {command: 'touch forbidden', replicas: 2}}"), nil)
	if e != nil || !d.Valid() {
		t.Fatalf("static definition: %v", e)
	}
	server, e := httpapi.Open(httpapi.Config{Workspace: dir, Binary: "/bin/true", Helper: "/bin/true"})
	if e != nil {
		t.Fatal(e)
	}
	defer server.Close()
	item, e := server.Store.CreateWorkflow(context.Background(), d)
	if e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	code := Run(context.Background(), []string{"workflow", "run", "--workspace", dir, "--workflow-id", item.ID, "--revision", item.Revision, "--target", "a", "--request-id", "unsupported"}, &out, &out)
	var result runtimeResult
	if e = json.Unmarshal(out.Bytes(), &result); e != nil || code != 2 || result.Cleanup != "not_started" || result.Reason != "validation_failed" {
		t.Fatalf("unsupported result: %d %s %v", code, &out, e)
	}
	if _, e = os.Stat(filepath.Join(dir, "forbidden")); !os.IsNotExist(e) {
		t.Fatal("unsupported definition executed")
	}
}

func TestRunRequiresExplicitServeAndDoesNotStartTask(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "pc.yaml"), []byte("processes: {a: {command: 'touch forbidden'}}"), 0600)
	var out bytes.Buffer
	code := Run(context.Background(), []string{"workflow", "run", "--workspace", dir, "--file", "pc.yaml", "--target", "a"}, &out, &out)
	if code != 2 || !strings.Contains(out.String(), "serve") {
		t.Fatalf("missing serve: %d %s", code, out.String())
	}
	if _, e := os.Stat(filepath.Join(dir, "forbidden")); !os.IsNotExist(e) {
		t.Fatal("implicit task")
	}
}

// 防止入口拆分改变错误优先级，或让非法来源提前登记运行。
func TestRunSourceRulesAndHostErrorPrecedence(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	for _, command := range []string{"run", "stop"} {
		out.Reset()
		code := Run(context.Background(), []string{"workflow", command, "--workspace", dir}, &out, &out)
		if code != 2 || !strings.Contains(out.String(), "workspace host unavailable") {
			t.Fatalf("%s: %d %s", command, code, &out)
		}
	}
	server, err := httpapi.Open(httpapi.Config{Workspace: dir, Binary: "/bin/true", Helper: "/bin/true"})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	cases := [][]string{
		{}, {"--file", "pc.yaml", "--workflow-id", "library", "--revision", "rev"},
		{"--workflow-id", "library"}, {"--file", "pc.yaml", "--revision", "rev"},
		{"--workflow-id", "library", "--revision", "rev", "--ui", "ui.json"},
		{"--file", "pc.yaml", "--timeout-ms", "0"},
	}
	for _, extra := range cases {
		out.Reset()
		args := append([]string{"workflow", "run", "--workspace", dir, "--target", "a"}, extra...)
		code := Run(context.Background(), args, &out, &out)
		var result runtimeResult
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if code != 2 || result.Message != "choose --file or --workflow-id/--revision; explicit target and timeout 1..3600000 required" || result.RunID != "" {
			t.Fatalf("%v: %d %s", extra, code, &out)
		}
	}
	items, _, err := server.Store.ListRuns("", 0, 20)
	if err != nil || len(items) != 0 {
		t.Fatalf("invalid sources registered runs: %v %v", items, err)
	}
}

func TestUnownedAmbiguousSubmissionReturnsEvidenceWithoutTaskWait(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "pc.yaml"), []byte("processes: {a: {command: echo ok}}"), 0600)
	id := strings.Repeat("a", 32)
	instance := strings.Repeat("b", 32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RunCradle-Instance", instance)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/health":
			json.NewEncoder(w).Encode(map[string]string{"status": "ready", "instance_id": instance})
		case strings.Contains(r.URL.Path, "run-requests"):
			w.WriteHeader(404)
			w.Write([]byte(`{"error":"not found"}`))
		case r.Method == "POST" && r.URL.Path == "/api/v1/runs":
			w.WriteHeader(202)
			json.NewEncoder(w).Encode(map[string]any{"run_id": id, "accepted": true})
		case r.Method == "GET":
			json.NewEncoder(w).Encode(map[string]any{"run_id": id, "owned": false, "final": false, "reason": "submission_unknown", "task_result": "unknown", "cleanup": "unknown"})
		default:
			w.WriteHeader(400)
			w.Write([]byte(`{"error":"unowned"}`))
		}
	}))
	defer server.Close()
	store, e := runs.Open(dir, true)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	release, e := store.ClaimServer(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	if e = store.PublishServer(runs.ServerDescriptor{Version: 1, URL: server.URL, Workspace: dir, InstanceID: instance, Token: strings.Repeat("c", 64)}); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var out bytes.Buffer
	start := time.Now()
	code := Run(ctx, []string{"workflow", "run", "--workspace", dir, "--file", "pc.yaml", "--target", "a", "--request-id", "uncertain"}, &out, &out)
	if code != 125 || time.Since(start) > time.Second || !strings.Contains(out.String(), id) {
		t.Fatalf("ambiguous request waited/cancelled instead of returning evidence: %d %s", code, out.String())
	}
}
