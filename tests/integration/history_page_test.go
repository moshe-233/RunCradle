package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	eng "runcradle/internal/engine"
	"runcradle/internal/runs"
	"runcradle/internal/workflow"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Synthetic persisted data tests the CLI read protocol, not native execution.
func TestW2HistoryNodePaginationDoesNotSkipOnOutputBudget(t *testing.T) {
	dir := t.TempDir()
	nodes := map[string]any{}
	for i := 0; i < 128; i++ {
		nodes[fmt.Sprintf("a%03d", i)] = map[string]any{"entrypoint": []string{"/bin/true"}}
	}
	raw, _ := json.Marshal(map[string]any{"processes": nodes})
	os.WriteFile(filepath.Join(dir, "pc.json"), raw, 0600)
	d, e := workflow.Load(dir, "pc.json", "")
	if e != nil {
		t.Fatal(e)
	}
	store, e := runs.Open(dir, true)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	intent, e := store.Begin(context.Background(), "read-fixture", strings.Repeat("a", 64), d)
	if e != nil {
		t.Fatal(e)
	}
	state := eng.Snapshot{Version: 1, RunID: intent.RunID, Revision: d.Revision, Final: true, TaskResult: "unknown"}
	for _, n := range d.View.Nodes {
		state.Nodes = append(state.Nodes, eng.Node{ID: n.ID, Name: n.Name, State: "declared", Source: "declaration", Health: "not_configured"})
	}
	if e = store.Save(state, false, time.Now()); e != nil {
		t.Fatal(e)
	}
	seen := map[string]bool{}
	offset := 0
	for k := 0; k < 128 && offset < 128; k++ {
		out, code := cliCall(t, "workflow", "inspect", "--workspace", dir, "--run-id", intent.RunID, "--nodes-after", strconv.Itoa(offset), "--limit", "100")
		if code != 0 {
			t.Fatalf("page query failed %d %s", code, out)
		}
		var page struct {
			Next  int `json:"next_node_offset"`
			Nodes []struct {
				ID string `json:"id"`
			} `json:"nodes"`
		}
		json.Unmarshal(out, &page)
		if page.Next <= offset || len(page.Nodes) == 0 {
			t.Fatalf("pagination stalled %s", out)
		}
		for _, n := range page.Nodes {
			if seen[n.ID] {
				t.Fatalf("duplicate node %s", n.ID)
			}
			seen[n.ID] = true
		}
		offset = page.Next
	}
	if len(seen) != 128 || offset != 128 {
		t.Fatalf("nodes lost to clipping: %d, offset %d", len(seen), offset)
	}
}
