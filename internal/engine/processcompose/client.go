package processcompose

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"io"
	"net"
	"net/http"
	"net/url"
	"runcradle/internal/engine"
	"runcradle/internal/output"
	"strings"
	"time"
)

type client struct {
	socket string
	http   *http.Client
}

func newClient(socket string) *client {
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}
	return &client{socket: socket, http: &http.Client{Transport: &http.Transport{DialContext: dial, DisableKeepAlives: true}, Timeout: time.Second}}
}
func (c *client) request(ctx context.Context, method, path string, value any) error {
	req, e := http.NewRequestWithContext(ctx, method, "http://runcradle"+path, nil)
	if e != nil {
		return e
	}
	resp, e := c.http.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("upstream HTTP %d", resp.StatusCode)
	}
	if value == nil {
		return nil
	}
	raw, e := io.ReadAll(io.LimitReader(resp.Body, 2<<20+1))
	if e != nil {
		return e
	}
	if len(raw) > 2<<20 {
		return fmt.Errorf("upstream snapshot exceeds limit")
	}
	return json.Unmarshal(raw, value)
}
func (c *client) states(ctx context.Context) ([]nativeState, error) {
	var v struct {
		Data []nativeState `json:"data"`
	}
	e := c.request(ctx, "GET", "/processes", &v)
	return v.Data, e
}
func (c *client) stop(ctx context.Context) error { return c.request(ctx, "POST", "/project/stop", nil) }
func (c *client) subscribe(ctx context.Context) (*websocket.Conn, error) {
	dialer := websocket.Dialer{HandshakeTimeout: time.Second, NetDialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", c.socket)
	}}
	conn, _, e := dialer.DialContext(ctx, "ws://runcradle/process/states/ws", nil)
	if conn != nil {
		conn.SetReadLimit(16384)
	}
	return conn, e
}

func (c *client) diagnostic(ctx context.Context, name string) *engine.Diagnostic {
	op, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	for {
		d := c.readDiagnostic(op, name)
		if d.Status == "available" || op.Err() != nil {
			return d
		}
		// Direct exit receipts can precede the native log reader's final publication.
		// These are read-only retries inside the original collection budget.
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-op.Done():
			timer.Stop()
			return d
		case <-timer.C:
		}
	}
}

func (c *client) readDiagnostic(ctx context.Context, name string) *engine.Diagnostic {
	d := &engine.Diagnostic{Status: "unavailable", Source: "native_combined_logs"}
	var value struct {
		Logs []string `json:"logs"`
	}
	if c.request(ctx, "GET", "/process/logs/"+url.PathEscape(name)+"/0/100", &value) != nil {
		return d
	}
	text := strings.Join(value.Logs, "\n")
	if text == "" {
		return d
	}
	if len(text) > 65536 {
		text = text[len(text)-65536:]
		d.Truncated = true
	}
	d.Text = output.Clean(text, 512)
	d.Truncated = d.Truncated || len(text) > 512
	d.Status = "available"
	return d
}

type nativeState struct {
	Name     string     `json:"name"`
	Status   string     `json:"status"`
	Health   string     `json:"is_ready"`
	HasProbe bool       `json:"has_ready_probe"`
	ExitCode int        `json:"exit_code"`
	PID      int        `json:"pid"`
	Restarts int        `json:"restarts"`
	Start    *time.Time `json:"process_start_time"`
	End      *time.Time `json:"process_end_time"`
}
type nativeEvent struct {
	Snapshot bool        `json:"snapshot"`
	State    nativeState `json:"state"`
}
