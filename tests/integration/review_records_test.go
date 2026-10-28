package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runcradle/internal/runs"
	"runcradle/internal/workflow"
	"strings"
	"testing"
	"time"
)

func TestW2EmptyProbeCwdInheritsNodeCwd(t *testing.T) {
	d := engineDocument(t, map[string]any{"processes": map[string]any{
		"api":   map[string]any{"working_dir": "sub", "entrypoint": []string{"/bin/sh", "-c", "touch ready; exec sleep 30"}, "shutdown": map[string]any{"timeout_seconds": 1}, "readiness_probe": map[string]any{"exec": map[string]any{"command": "test -f ready", "working_dir": ""}, "period_seconds": 1, "failure_threshold": 1}},
		"check": map[string]any{"entrypoint": []string{"/bin/true"}, "depends_on": map[string]any{"api": map[string]any{"condition": "process_healthy"}}, "availability": map[string]any{"exit_on_end": true}},
	}})
	os.Mkdir(filepath.Join(d.Workspace, "sub"), 0700)
	// Isolate cwd inheritance from first-probe versus job-start scheduling.
	os.WriteFile(filepath.Join(d.Workspace, "sub", "ready"), []byte("ready"), 0600)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	native := exec.CommandContext(ctx, engine(t), "--log-file", "/dev/null", "up", "--config", filepath.Join(d.Workspace, d.Entry), "--tui=false", "--no-server", "--disable-dotenv")
	native.Dir = d.Workspace
	if e := native.Run(); e != nil {
		t.Fatalf("native empty-cwd control failed: %v", e)
	}
	out, code := cliCall(t, "workflow", "run", "--workspace", d.Workspace, "--file", d.Entry, "--target", "check", "--process-compose", engine(t), "--timeout-ms", "5000")
	if code != 0 {
		t.Fatalf("empty probe cwd changed native semantics: %d %s", code, out)
	}
}

func TestW2CapacityRefusalIsKnownNotSubmitted(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "pc.yaml"), []byte("processes: {a: {command: 'touch marker'}}"), 0600)
	d, e := workflow.Load(dir, "pc.yaml", "")
	if e != nil {
		t.Fatal(e)
	}
	store, e := runs.Open(dir, true)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	var releases []func() error
	for i := 0; i < 4; i++ {
		intent, e := store.Begin(context.Background(), fmt.Sprint("held-", i), strings.Repeat("a", 64), d)
		if e != nil {
			t.Fatal(e)
		}
		release, e := store.Lease(intent.RunID)
		if e != nil {
			t.Fatal(e)
		}
		releases = append(releases, release)
	}
	defer func() {
		for _, release := range releases {
			_ = release()
		}
	}()
	args := []string{"workflow", "run", "--workspace", dir, "--file", "pc.yaml", "--target", "a", "--request-id", "capacity", "--process-compose", engine(t)}
	out, code := cliCall(t, args...)
	var response struct {
		RunID  string `json:"run_id"`
		Final  bool   `json:"final"`
		Reason string `json:"reason"`
	}
	json.Unmarshal(out, &response)
	if code != 2 || response.RunID == "" || !response.Final || response.Reason != "not_submitted" {
		t.Fatalf("capacity became ambiguous: %d %s", code, out)
	}
	for _, release := range releases {
		_ = release()
	}
	releases = nil
	retry, code := cliCall(t, args...)
	if code != 2 || !strings.Contains(string(retry), response.RunID) || !strings.Contains(string(retry), "new request") {
		t.Fatalf("capacity retry changed fact: %d %s", code, retry)
	}
	if _, e := os.Stat(filepath.Join(dir, "marker")); !os.IsNotExist(e) {
		t.Fatal("refused request executed")
	}
	// Replace the explicit request value by position-independent lookup.
	for i, arg := range args {
		if arg == "--request-id" {
			args[i+1] = "after-capacity"
		}
	}
	out, code = cliCall(t, args...)
	if code != 0 {
		t.Fatalf("fresh request could not execute: %d %s", code, out)
	}
}

func TestW2CachedSuccessCannotOverrideMissingActualExit(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "pc.yaml"), []byte("processes: {a: {entrypoint: [/bin/true]}}"), 0600)
	out, code := cliCall(t, "workflow", "run", "--workspace", dir, "--file", "pc.yaml", "--target", "a", "--process-compose", engine(t))
	if code != 0 {
		t.Fatal(string(out))
	}
	var summary struct {
		RunID string `json:"run_id"`
	}
	json.Unmarshal(out, &summary)
	file := filepath.Join(dir, ".runcradle/runs", summary.RunID, "snapshot.json")
	raw, _ := os.ReadFile(file)
	var record map[string]any
	json.Unmarshal(raw, &record)
	snapshot := record["snapshot"].(map[string]any)
	snapshot["nodes"].([]any)[0].(map[string]any)["exit_code"] = nil
	raw, _ = json.Marshal(record)
	os.WriteFile(file, raw, 0600)
	out, code = cliCall(t, "workflow", "inspect", "--workspace", dir, "--run-id", summary.RunID)
	if code != 125 || strings.Contains(string(out), `"status":"succeeded"`) {
		t.Fatalf("cached success hid absent proof: %d %s", code, out)
	}
}
