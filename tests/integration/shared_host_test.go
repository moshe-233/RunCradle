package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runcradle/internal/hostclient"
	"runcradle/internal/httpapi"
	"runcradle/internal/runs"
	"runcradle/internal/service"
	"runcradle/internal/workflow"
	"strings"
	"sync"
	"testing"
	"time"
)

var testHosts = struct {
	sync.Mutex
	items map[string]*httpapi.Server
}{items: map[string]*httpapi.Server{}}

// W2 assertions run through an explicitly created test host. Product CLI never starts one.
func w3RunArgs(t *testing.T, args []string) []string {
	t.Helper()
	if len(args) < 2 || args[0] != "workflow" || args[1] != "run" {
		return args
	}
	workspace, binary := "", ""
	result := []string{}
	for i := 0; i < len(args); i++ {
		if args[i] == "--workspace" && i+1 < len(args) {
			workspace = args[i+1]
		}
		if args[i] == "--process-compose" && i+1 < len(args) {
			binary = args[i+1]
			i++
			continue
		}
		result = append(result, args[i])
	}
	if workspace == "" {
		return result
	}
	if binary == "" {
		binary = engineBinary(t)
	}
	ensureTestHost(t, workspace, binary)
	return result
}
func engineBinary(t *testing.T) string { return engine(t) }
func ensureTestHost(t *testing.T, workspace, binary string) *httpapi.Server {
	t.Helper()
	testHosts.Lock()
	defer testHosts.Unlock()
	if s := testHosts.items[workspace]; s != nil {
		return s
	}
	s, e := httpapi.Open(httpapi.Config{Workspace: workspace, Binary: binary, Helper: executable})
	if e != nil {
		t.Fatal(e)
	}
	testHosts.items[workspace] = s
	t.Cleanup(func() {
		if e := s.Close(); e != nil {
			t.Errorf("host close: %v", e)
		}
		testHosts.Lock()
		delete(testHosts.items, workspace)
		testHosts.Unlock()
	})
	return s
}
func TestW3HTTPSubmissionRetrySSEAndOfflineHistory(t *testing.T) {
	dir := t.TempDir()
	source := []byte("processes: {a: {command: 'touch marker; sleep 0.4; exit 7', availability: {exit_on_end: true}}}")
	d, e := workflow.FromBytes(dir, "pc.yaml", "", "yaml", source, nil)
	if e != nil {
		t.Fatal(e)
	}
	s := ensureTestHost(t, dir, engine(t))
	client, e := hostclient.Connect(context.Background(), dir)
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	input := httpapi.RunRequest{Document: &httpapi.Draft{Format: "yaml", Source: string(source), Entry: "pc.yaml"}, Revision: d.Revision, RequestID: "network-retry", Targets: []string{"a"}, TimeoutMS: 5000}
	ctx, cancel := context.WithCancel(context.Background())
	var sub service.Submission
	if e = client.Do(ctx, "POST", "/api/v1/runs", input, &sub); e != nil {
		t.Fatal(e)
	}
	cancel()
	var again service.Submission
	if e = client.Do(context.Background(), "POST", "/api/v1/runs", input, &again); e != nil || again.RunID != sub.RunID {
		t.Fatalf("duplicate %+v %v", again, e)
	}
	descriptor, e := s.Store.Server()
	if e != nil {
		t.Fatal(e)
	}
	request, _ := http.NewRequest("GET", s.URL+"/api/v1/runs/"+sub.RunID+"/events", nil)
	request.Header.Set("Authorization", "Bearer "+descriptor.Token)
	request.Header.Set("Accept", "text/event-stream")
	stream := &http.Client{Timeout: 5 * time.Second}
	response, e := stream.Do(request)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := io.ReadAll(response.Body)
	response.Body.Close()
	if e != nil || response.StatusCode != 200 || !strings.Contains(string(raw), "event: final") || !strings.Contains(string(raw), `"exit_code":7`) {
		t.Fatalf("stream: %s %v", raw, e)
	}
	before, e := os.Stat(filepath.Join(dir, "marker"))
	if e != nil {
		t.Fatal(e)
	}
	if e = client.Do(context.Background(), "POST", "/api/v1/runs", input, &again); e != nil {
		t.Fatal(e)
	}
	after, e := os.Stat(filepath.Join(dir, "marker"))
	if e != nil || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("replayed business task")
	}
	store, e := runs.Open(dir, false)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	state, e := store.Load(sub.RunID, time.Now())
	if e != nil || !state.Final || state.Nodes[0].ExitCode == nil || *state.Nodes[0].ExitCode != 7 {
		encoded, _ := json.Marshal(state)
		t.Fatalf("offline: %s %v", encoded, e)
	}
}
