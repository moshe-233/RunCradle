package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProtocolDetachedSubmissionReplaysOnceAndSurvivesClientExit(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pc.yaml"), []byte("processes:\n  a:\n    command: \"sleep 0.2; echo once >> calls\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"workflow", "run", "--workspace", dir, "--file", "pc.yaml", "--target", "a", "--request-id", "adapter/retry-1", "--detach", "--timeout-ms", "10000", "--process-compose", engine(t)}
	var runID string
	for n := 0; n < 2; n++ {
		raw, code := cliCall(t, args...)
		var accepted struct {
			Accepted bool   `json:"accepted"`
			RunID    string `json:"run_id"`
			Status   string `json:"status"`
		}
		if err := json.Unmarshal(raw, &accepted); err != nil || code != 0 || !accepted.Accepted || accepted.Status != "accepted" {
			t.Fatalf("submit: %d %s %v", code, raw, err)
		}
		if runID != "" && runID != accepted.RunID {
			t.Fatal("retry started a different run")
		}
		runID = accepted.RunID
	}
	deadline := time.Now().Add(12 * time.Second)
	for {
		raw, code := cliCall(t, "workflow", "inspect", "--workspace", dir, "--run-id", runID)
		var observed struct {
			Final      bool   `json:"final"`
			TaskResult string `json:"task_result"`
			Cleanup    string `json:"cleanup"`
		}
		if err := json.Unmarshal(raw, &observed); err != nil || code != 0 {
			t.Fatalf("inspect: %d %s %v", code, raw, err)
		}
		if observed.Final {
			if observed.TaskResult != "succeeded" || observed.Cleanup != "complete" {
				t.Fatalf("run facts: %s", raw)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("detached task did not finish: %s", raw)
		}
		time.Sleep(50 * time.Millisecond)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "calls"))
	if err != nil || string(raw) != "once\n" {
		t.Fatalf("execution count: %q %v", raw, err)
	}
	out, code := cliCall(t, "api", "request", "--workspace", dir, "--path", "/api/v1/runs/"+runID+"/events?limit=2", "--out", "events.json")
	if code != 0 {
		t.Fatalf("events CLI: %d %s", code, out)
	}
	raw, err = os.ReadFile(filepath.Join(dir, "events.json"))
	if err != nil || !json.Valid(raw) {
		t.Fatalf("events response: %s %v", raw, err)
	}
}
