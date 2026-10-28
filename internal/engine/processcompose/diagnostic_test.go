package processcompose

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDelayedNativeFailureLogWithinExistingCollectionBudget(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(map[bool]string{false: "eventually available", true: "no body"}[empty], func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if calls.Add(1) == 1 || empty {
					w.Write([]byte(`{"logs":[]}`))
					return
				}
				w.Write([]byte(`{"logs":["\u001b[31mASSERT\u001b[0m"]}`))
			}))
			socket := filepath.Join(shortSocketDir(t), "logs.sock")
			listener, e := net.Listen("unix", socket)
			if e != nil {
				t.Fatal(e)
			}
			server.Listener = listener
			server.Start()
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
			defer cancel()
			start := time.Now()
			d := newClient(socket).diagnostic(ctx, "a")
			if time.Since(start) > 300*time.Millisecond {
				t.Fatal("diagnostic exceeded caller budget")
			}
			if !empty && (d.Status != "available" || !strings.Contains(d.Text, "ASSERT") || strings.Contains(d.Text, "\x1b")) {
				t.Fatalf("delayed native log discarded: %+v (%d reads)", d, calls.Load())
			}
			if empty && d.Status != "unavailable" {
				t.Fatalf("empty diagnostic invented: %+v", d)
			}
		})
	}
}
