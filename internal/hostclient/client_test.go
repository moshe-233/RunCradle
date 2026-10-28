package hostclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runcradle/internal/runs"
	"strings"
	"testing"
)

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
