package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runcradle/internal/engine"
	"runcradle/internal/workflow"
	"strings"
	"sync"
	"testing"
	"time"
)

func storedEventRun(t *testing.T, s *Server, count, size int) string {
	t.Helper()
	d, e := workflow.FromBytes(s.workspace, "events.yaml", "", "yaml", []byte("processes: {a: {command: echo ok}}"), nil)
	if e != nil {
		t.Fatal(e)
	}
	intent, e := s.Store.Begin(context.Background(), "event-fixture", strings.Repeat("e", 64), d)
	if e != nil {
		t.Fatal(e)
	}
	var raw bytes.Buffer
	for seq := 1; seq <= count; seq++ {
		if e = json.NewEncoder(&raw).Encode(engine.Event{RunID: intent.RunID, Seq: uint64(seq), Kind: strings.Repeat("x", size)}); e != nil {
			t.Fatal(e)
		}
	}
	if e = os.WriteFile(filepath.Join(s.workspace, ".runcradle/runs", intent.RunID, "events.jsonl"), raw.Bytes(), 0600); e != nil {
		t.Fatal(e)
	}
	if e = s.Store.Save(engine.Snapshot{Version: 1, RunID: intent.RunID, Revision: d.Revision, Final: true, TaskResult: "unknown", Cleanup: "unknown", Reason: "engine_exited", HistoryIntegrity: "partial"}, false, time.Now()); e != nil {
		t.Fatal(e)
	}
	return intent.RunID
}

func TestSSEIndependentPersistedReplayAndMissingCursor(t *testing.T) {
	s := apiFixture(t)
	id := storedEventRun(t, s, 3, 8)
	start := make(chan struct{})
	var group sync.WaitGroup
	for _, query := range []bool{true, false} {
		group.Add(1)
		go func(query bool) {
			defer group.Done()
			<-start
			path := s.URL + "/api/v1/runs/" + id + "/events"
			if query {
				path += "?after_seq=1"
			}
			req, _ := http.NewRequest("GET", path, nil)
			req.Header.Set("Authorization", "Bearer "+s.descriptor.Token)
			req.Header.Set("Accept", "text/event-stream")
			if !query {
				req.Header.Set("Last-Event-ID", "1")
			}
			res, e := (&http.Client{Timeout: 2 * time.Second}).Do(req)
			if e != nil {
				t.Error(e)
				return
			}
			defer res.Body.Close()
			raw, e := io.ReadAll(res.Body)
			if e != nil || res.StatusCode != 200 || strings.Count(string(raw), "id: ") != 2 || strings.Contains(string(raw), "id: 1\n") || !strings.Contains(string(raw), "id: 3\n") || !strings.Contains(string(raw), "event: final") {
				t.Errorf("independent replay: status %d, %s, %v", res.StatusCode, raw, e)
			}
		}(query)
	}
	close(start)
	group.Wait()
	req, _ := http.NewRequest("GET", s.URL+"/api/v1/runs/"+id+"/events?after_seq=999", nil)
	req.Header.Set("Authorization", "Bearer "+s.descriptor.Token)
	req.Header.Set("Accept", "text/event-stream")
	res, e := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	raw, e := io.ReadAll(res.Body)
	if e != nil || !strings.Contains(string(raw), "event: gap") || strings.Count(string(raw), "id: ") != 3 {
		t.Fatalf("unavailable cursor: %s %v", raw, e)
	}
}

func TestSSESlowTCPReaderReleasesItsSlotWithinWriteBudget(t *testing.T) {
	s := apiFixture(t)
	id := storedEventRun(t, s, 1200, 8192)
	conn, e := net.DialTimeout("tcp", strings.TrimPrefix(s.URL, "http://"), time.Second)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	if e = conn.(*net.TCPConn).SetReadBuffer(1024); e != nil {
		t.Fatal(e)
	}
	if _, e = io.WriteString(conn, "GET /api/v1/runs/"+id+"/events HTTP/1.1\r\nHost: "+strings.TrimPrefix(s.URL, "http://")+"\r\nAuthorization: Bearer "+s.descriptor.Token+"\r\nAccept: text/event-stream\r\n\r\n"); e != nil {
		t.Fatal(e)
	}
	// Do not consume the socket. A blocked network write must release the server slot.
	observed := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		active := s.subscribers[id]
		s.mu.Unlock()
		if active > 0 {
			observed = true
		}
		if observed && active == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("slow reader retained its slot beyond 2s write budget (started=%v)", observed)
}

func TestCookieWriteRequiresOriginAndExpiredPairingFails(t *testing.T) {
	s := apiFixture(t)
	raw, _ := json.Marshal(map[string]string{"token": s.pairToken})
	req, _ := http.NewRequest("POST", s.URL+"/api/v1/session", bytes.NewReader(raw))
	req.Header.Set("Origin", s.URL)
	req.Header.Set("Content-Type", "application/json")
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 200 || len(res.Cookies()) != 1 {
		t.Fatal("pair failed")
	}
	cookie := res.Cookies()[0]
	for _, origin := range []string{"", "https://foreign.invalid", s.URL} {
		req, _ = http.NewRequest("POST", s.URL+"/api/v1/validate", strings.NewReader(`{"format":"yaml","source":"processes: {}"}`))
		req.Header.Set("Content-Type", "application/json")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		req.AddCookie(cookie)
		res, e = http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		res.Body.Close()
		want := 403
		if origin == s.URL {
			want = 200
		}
		if res.StatusCode != want {
			t.Fatalf("cookie write origin %q: %d", origin, res.StatusCode)
		}
	}
	s.mu.Lock()
	s.pairToken = strings.Repeat("f", 64)
	s.pairExpires = time.Now().Add(-time.Second)
	s.mu.Unlock()
	raw, _ = json.Marshal(map[string]string{"token": strings.Repeat("f", 64)})
	req, _ = http.NewRequest("POST", s.URL+"/api/v1/session", bytes.NewReader(raw))
	req.Header.Set("Origin", s.URL)
	req.Header.Set("Content-Type", "application/json")
	res, e = http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatal("expired pairing accepted")
	}
}
