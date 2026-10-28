package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"runcradle/internal/engine"
	"runcradle/internal/workflow"
	"strings"
	"testing"
	"time"
)

func apiFixture(t *testing.T) *Server {
	t.Helper()
	s, e := Open(Config{Workspace: t.TempDir(), Binary: "/bin/true", Helper: "/bin/true"})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func callAPI(t *testing.T, s *Server, method, path string, body any, token bool) (int, []byte) {
	t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	req, e := http.NewRequest(method, s.URL+path, bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("Content-Type", "application/json")
	if token {
		req.Header.Set("Authorization", "Bearer "+s.descriptor.Token)
	}
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	out, e := io.ReadAll(res.Body)
	if e != nil {
		t.Fatal(e)
	}
	return res.StatusCode, out
}
func TestAuthorizationOriginHostAndPairing(t *testing.T) {
	s := apiFixture(t)
	if code, _ := callAPI(t, s, "GET", "/api/v1/workflows", nil, false); code != 401 {
		t.Fatalf("unauthenticated %d", code)
	}
	if code, _ := callAPI(t, s, "GET", "/api/v1/workflows", nil, true); code != 200 {
		t.Fatalf("authenticated %d", code)
	}
	for _, headers := range []map[string]string{{"Origin": "https://foreign.invalid"}, {"Host": "foreign.invalid"}} {
		req, _ := http.NewRequest("GET", s.URL+"/api/v1/workflows", nil)
		req.Header.Set("Authorization", "Bearer "+s.descriptor.Token)
		for k, v := range headers {
			if k == "Host" {
				req.Host = v
			} else {
				req.Header.Set(k, v)
			}
		}
		res, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		res.Body.Close()
		if res.StatusCode != 403 {
			t.Fatalf("boundary bypass: %d", res.StatusCode)
		}
	}
	pair := func() (*http.Response, []byte) {
		raw, _ := json.Marshal(map[string]string{"token": s.pairToken})
		req, _ := http.NewRequest("POST", s.URL+"/api/v1/session", bytes.NewReader(raw))
		req.Header.Set("Origin", s.URL)
		req.Header.Set("Content-Type", "application/json")
		res, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		out, _ := io.ReadAll(res.Body)
		res.Body.Close()
		return res, out
	}
	res, _ := pair()
	if res.StatusCode != 200 || len(res.Cookies()) != 1 || !res.Cookies()[0].HttpOnly || res.Cookies()[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe pairing cookie")
	}
	req, _ := http.NewRequest("GET", s.URL+"/api/v1/workflows", nil)
	req.AddCookie(res.Cookies()[0])
	response, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("cookie not usable")
	}
	res, _ = pair()
	if res.StatusCode != 401 {
		t.Fatal("pair token reused")
	}
	ready, _ := json.Marshal(s.Ready())
	if strings.Contains(string(ready), s.descriptor.Token) {
		t.Fatal("permanent token in startup receipt")
	}
	if _, e = Open(Config{Workspace: s.workspace, Binary: "/bin/true", Helper: "/bin/true"}); e == nil {
		t.Fatal("duplicate host accepted")
	}
}
func TestAPILibraryDraftCASAndExport(t *testing.T) {
	s := apiFixture(t)
	draft := map[string]any{"format": "yaml", "source": "processes: {a: {command: echo old}}"}
	code, raw := callAPI(t, s, "POST", "/api/v1/validate", draft, true)
	if code != 200 {
		t.Fatalf("validate %d %s", code, raw)
	}
	code, raw = callAPI(t, s, "POST", "/api/v1/workflows", draft, true)
	if code != 201 {
		t.Fatalf("create %d %s", code, raw)
	}
	var created struct {
		Workflow struct {
			ID       string `json:"workflow_id"`
			Revision string `json:"revision"`
		} `json:"workflow"`
	}
	if e := json.Unmarshal(raw, &created); e != nil {
		t.Fatal(e)
	}
	path := "/api/v1/workflows/" + created.Workflow.ID
	update := map[string]any{"format": "yaml", "source": "processes: {a: {command: echo new}}", "expected_revision": created.Workflow.Revision}
	if code, raw = callAPI(t, s, "PUT", path, update, true); code != 200 {
		t.Fatalf("save %d %s", code, raw)
	}
	if code, _ = callAPI(t, s, "PUT", path, update, true); code != 409 {
		t.Fatalf("stale save %d", code)
	}
	if code, raw = callAPI(t, s, "GET", path+"?revision="+created.Workflow.Revision, nil, true); code != 200 || !strings.Contains(string(raw), "echo old") {
		t.Fatalf("old revision lost %d %s", code, raw)
	}
	if code, raw = callAPI(t, s, "GET", path+"/export?revision="+created.Workflow.Revision, nil, true); code != 200 || !bytes.HasPrefix(raw, []byte("PK")) {
		t.Fatalf("zip %d %s", code, raw)
	}
	if code, _ = callAPI(t, s, "POST", "/api/v1/imports", map[string]string{"file": "../escape.yaml"}, true); code != 400 {
		t.Fatalf("path escape %d", code)
	}
	if code, _ = callAPI(t, s, "POST", "/api/v1/validate", map[string]any{"format": "yaml", "source": "processes: {}", "pid": 123}, true); code != 400 {
		t.Fatalf("unknown input %d", code)
	}
}

func TestInterruptedRunIsReadOnlyAndDoesNotKeepStreaming(t *testing.T) {
	s := apiFixture(t)
	d, e := workflow.FromBytes(s.workspace, "pc.yaml", "", "yaml", []byte("processes: {a: {command: echo ok}}"), nil)
	if e != nil {
		t.Fatal(e)
	}
	intent, e := s.Store.Begin(context.Background(), "lost-owner", strings.Repeat("a", 64), d)
	if e != nil {
		t.Fatal(e)
	}
	state := engine.Snapshot{Version: 1, RunID: intent.RunID, Revision: d.Revision, Reason: "running", TaskResult: "unknown", Cleanup: "unknown", HistoryIntegrity: "partial"}
	if e = s.Store.Save(state, false, time.Now()); e != nil {
		t.Fatal(e)
	}
	code, raw := callAPI(t, s, "GET", "/api/v1/runs/"+intent.RunID, nil, true)
	if code != 200 {
		t.Fatal(string(raw))
	}
	var decoded map[string]json.RawMessage
	json.Unmarshal(raw, &decoded)
	if string(decoded["owned"]) != "false" {
		t.Fatal("history did not explicitly deny ownership")
	}
	if code, _ = callAPI(t, s, "POST", "/api/v1/runs/"+intent.RunID+"/stop", map[string]any{}, true); code != 400 {
		t.Fatal("history grants stop authority")
	}
	req, _ := http.NewRequest("GET", s.URL+"/api/v1/runs/"+intent.RunID+"/events", nil)
	req.Header.Set("Authorization", "Bearer "+s.descriptor.Token)
	req.Header.Set("Accept", "text/event-stream")
	client := &http.Client{Timeout: time.Second}
	res, e := client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	raw, e = io.ReadAll(res.Body)
	if e != nil || !strings.Contains(string(raw), "event: interrupted") {
		t.Fatalf("unowned run kept streaming: %s %v", raw, e)
	}
}
