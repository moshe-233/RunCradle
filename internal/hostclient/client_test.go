package hostclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"runcradle/internal/runs"
	"strings"
	"testing"
)

func TestHTTPFailureSurvivesNonJSONBodyAndTypedDestination(t *testing.T) {
	instance := strings.Repeat("a", 32)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RunCradle-Instance", instance)
		w.WriteHeader(503)
		w.Write([]byte("unavailable"))
	}))
	defer s.Close()
	c := &Client{descriptor: runs.ServerDescriptor{URL: s.URL, InstanceID: instance}, http: s.Client()}
	var output struct {
		RunID string `json:"run_id"`
	}
	err := c.Do(context.Background(), "GET", "/api/v1/runs/example", nil, &output)
	var apiErr *HTTPError
	if !errors.As(err, &apiErr) || apiErr.Status != 503 {
		t.Fatalf("lost HTTP classification: %T %v", err, err)
	}
}

func TestClientRejectsNonCanonicalProductPathsBeforeSending(t *testing.T) {
	hits := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++; w.Write([]byte(`{}`)) }))
	defer s.Close()
	c := &Client{descriptor: runs.ServerDescriptor{URL: s.URL}, http: s.Client()}
	for _, path := range []string{"/api/v1/../session", "/api/v1/%2e%2e/session", "/api/v1//runs", "/api/v1/runs#token", "https://example.invalid/api/v1/runs", "/api/v1/runs?token=secret"} {
		if err := c.Do(context.Background(), "GET", path, nil, nil); err == nil {
			t.Fatalf("accepted %s", path)
		}
	}
	if hits != 0 {
		t.Fatalf("sent %d unsafe requests", hits)
	}
}

func TestMissingHostIsExplicitAndStaleInstanceIsRejected(t *testing.T) {
	dir := t.TempDir()
	if _, e := Connect(context.Background(), dir); e == nil || !strings.Contains(e.Error(), "serve") {
		t.Fatalf("missing host: %v", e)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RunCradle-Instance", strings.Repeat("b", 32))
		w.Write([]byte(`{"status":"ready"}`))
	}))
	defer server.Close()
	store, e := runs.Open(dir, true)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	release, e := store.ClaimServer(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	if e = store.PublishServer(runs.ServerDescriptor{Version: 1, URL: server.URL, Workspace: dir, InstanceID: strings.Repeat("a", 32), Token: strings.Repeat("c", 64)}); e != nil {
		t.Fatal(e)
	}
	if _, e = Connect(context.Background(), dir); e == nil || !strings.Contains(e.Error(), "instance") {
		t.Fatalf("stale endpoint accepted: %v", e)
	}
}

func TestStaleDescriptorWithoutLiveOwnerIsNotContacted(t *testing.T) {
	dir := t.TempDir()
	hits := 0
	instance := strings.Repeat("a", 32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("X-RunCradle-Instance", instance)
		w.Write([]byte(`{"status":"ready","instance_id":"` + instance + `"}`))
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
	if e = store.PublishServer(runs.ServerDescriptor{Version: 1, URL: server.URL, Workspace: dir, InstanceID: instance, Token: strings.Repeat("c", 64)}); e != nil {
		t.Fatal(e)
	}
	if e = release(); e != nil {
		t.Fatal(e)
	}
	if c, e := Connect(context.Background(), dir); e == nil {
		c.Close()
		t.Fatal("stale descriptor treated as live host")
	}
	if hits != 0 {
		t.Fatal("contacted a listener without a live private host owner")
	}
}
