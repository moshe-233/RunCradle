package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"runcradle/internal/processcompose"
	"runcradle/internal/workflow"
)

func engine(t *testing.T) string {
	t.Helper()
	bin := os.Getenv("RUNCRADLE_PROCESS_COMPOSE")
	if bin == "" {
		bin = filepath.Join("..", "..", ".cache", "process-compose", "v1.122.0", "process-compose")
	}
	absolute, e := filepath.Abs(bin)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(absolute)
	if e != nil {
		t.Fatalf("required real engine missing; run make prepare: %v", e)
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != "7127df85c0d0c62f2f79b4d2b3082ca1d85f7c0c74523457617861d32150b0dd" {
		t.Fatal("integration requires the fixed official Linux amd64 binary")
	}
	return absolute
}
func input(t *testing.T, raw []byte) workflow.Document {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "workspace 'with spaces'")
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(dir, "process compose.json"), raw, 0600)
	d, e := workflow.Load(dir, "process compose.json", "")
	if e != nil || !d.Valid() {
		t.Fatalf("invalid fixture %v %+v", e, d.Issues)
	}
	return d
}
func TestRealDryRunDoesNotExecuteAnything(t *testing.T) {
	binary := engine(t)
	markers := t.TempDir()
	command := func(name string) string { return "touch " + filepath.Join(markers, name) }
	config := map[string]any{"version": "0.5", "processes": map[string]any{
		"api":   map[string]any{"command": command("node"), "log_location": filepath.Join(markers, "log"), "readiness_probe": map[string]any{"exec": map[string]any{"command": command("probe")}}, "shutdown": map[string]any{"command": command("shutdown")}},
		"check": map[string]any{"command": command("check"), "depends_on": map[string]any{"api": map[string]any{"condition": "process_healthy"}}},
	}}
	raw, _ := json.Marshal(config)
	d := input(t, raw)
	t.Setenv("PC_CONFIG_FILES", filepath.Join(markers, "must-not-load.yaml"))
	t.Setenv("PC_LOG_FILE", filepath.Join(markers, "ambient-log"))
	r := processcompose.Check(context.Background(), d, binary)
	if r.Code != 0 || r.Status != "passed" || r.EngineExit == nil || *r.EngineExit != 0 {
		t.Fatalf("real check failed: %+v", r)
	}
	entries, e := os.ReadDir(markers)
	if e != nil || len(entries) != 0 {
		t.Fatalf("check executed/wrote external declarations: %v %v", entries, e)
	}
	after, _ := os.ReadFile(filepath.Join(d.Workspace, d.Entry))
	if string(after) != string(raw) {
		t.Fatal("source changed")
	}
	entries, _ = os.ReadDir(d.Workspace)
	if len(entries) != 1 {
		t.Fatalf("workspace residue: %v", entries)
	}
}
func TestRealRejectionAndIndependentConcurrentChecks(t *testing.T) {
	binary := engine(t)
	invalid := input(t, []byte(`{"version":"0.5","log_level":"imaginary","processes":{"a":{"command":"echo ok"}}}`))
	if r := processcompose.Check(context.Background(), invalid, binary); r.Code != 2 || r.Status != "rejected" {
		t.Fatalf("upstream invalid config accepted: %+v", r)
	}
	d := input(t, []byte(`{"version":"0.5","processes":{"a":{"entrypoint":["echo","literal spaces and quotes '"]}}}`))
	var wg sync.WaitGroup
	results := make(chan processcompose.Result, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- processcompose.Check(context.Background(), d, binary) }()
	}
	wg.Wait()
	close(results)
	for r := range results {
		if r.Code != 0 {
			t.Fatalf("concurrent check failed: %+v", r)
		}
	}
}

func TestWorkspacePCEnvCannotChangeEngineAcceptance(t *testing.T) {
	binary := engine(t)
	d := input(t, []byte(`{"version":"0.5","log_level":"imaginary","processes":{"a":{"command":"echo ok"}}}`))
	for _, contents := range []string{"", "PC_LOG_LEVEL=info\n", "PC_PORT_NUM=not-a-number\n"} {
		if err := os.WriteFile(filepath.Join(d.Workspace, ".pc_env"), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		if r := processcompose.Check(context.Background(), d, binary); r.Status != "rejected" || r.Code != 2 || r.EngineExit == nil || *r.EngineExit != 1 {
			t.Fatalf("hidden workspace file changed acceptance: %+v", r)
		}
	}
}

func TestRealEngineAcceptsLiteralMergeProcessName(t *testing.T) {
	d := input(t, []byte(`{"version":"0.5","processes":{"<<":{"command":"echo ok"}}}`))
	if r := processcompose.Check(context.Background(), d, engine(t)); r.Status != "passed" || r.Code != 0 {
		t.Fatalf("literal << process rejected: %+v", r)
	}
}
