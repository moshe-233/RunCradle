package processcompose

import (
	"context"
	"github.com/gorilla/websocket"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runcradle/internal/engine"
	"sync/atomic"
	"testing"
	"time"
)

func TestStateDisconnectReportsGapAndReconnectSnapshot(t *testing.T) {
	socket := filepath.Join(shortSocketDir(t), "api.sock")
	listener, e := net.Listen("unix", socket)
	if e != nil {
		t.Fatal(e)
	}
	var connections atomic.Int32
	up := websocket.Upgrader{}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		conn, e := up.Upgrade(w, q, nil)
		if e != nil {
			return
		}
		defer conn.Close()
		n := connections.Add(1)
		_ = conn.WriteJSON(nativeEvent{Snapshot: n > 1, State: nativeState{Name: "a", Status: "Running", PID: int(n)}})
		if n > 1 {
			var v any
			_ = conn.ReadJSON(&v)
		}
	}))
	server.Listener = listener
	server.Start()
	defer server.Close()
	r := &run{client: newClient(socket), native: make(chan nativeEvent, 8), gap: make(chan struct{}, 1)}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() { r.stream(ctx); close(done) }()
	select {
	case <-r.native:
	case <-ctx.Done():
		t.Fatal("first event missing")
	}
	select {
	case <-r.gap:
	case <-ctx.Done():
		t.Fatal("disconnect hidden")
	}
	select {
	case v := <-r.native:
		if !v.Snapshot || v.State.PID != 2 {
			t.Fatalf("bad recovery %+v", v)
		}
	case <-ctx.Done():
		t.Fatal("no reconnect snapshot")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stream reader leaked")
	}
}
func TestSlowProductConsumerIsClosedAndGapRetained(t *testing.T) {
	r := &run{events: make(chan engine.Event, 1), state: engine.Snapshot{RunID: "x", HistoryIntegrity: "partial"}}
	r.publish("first", "fixture", nil)
	r.publish("second", "fixture", nil)
	if r.state.HistoryIntegrity != "gap" || !r.eventsClosed {
		t.Fatal("slow consumer hid missing events")
	}
	<-r.events
	if _, ok := <-r.events; ok {
		t.Fatal("consumer still open")
	}
}

func shortSocketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "sock-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	return dir
}
