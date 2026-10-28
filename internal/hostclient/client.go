package hostclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"runcradle/internal/runs"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	descriptor runs.ServerDescriptor
	http       *http.Client
}
type HTTPError struct {
	Status  int
	Message string
}

func (e *HTTPError) Error() string { return e.Message }
func Connect(ctx context.Context, workspace string) (*Client, error) {
	start := func(e error) (*Client, error) {
		return nil, fmt.Errorf("workspace host unavailable: %v; start runcradle serve --workspace %q", e, workspace)
	}
	abs, e := filepath.Abs(workspace)
	if e != nil {
		return start(e)
	}
	abs, e = filepath.EvalSymlinks(abs)
	if e != nil {
		return start(e)
	}
	store, e := runs.Open(abs, false)
	if e != nil {
		return start(e)
	}
	descriptor, e := store.Server()
	if e == nil {
		active, err := store.ServerActive()
		if err != nil {
			e = err
		} else if !active {
			e = fmt.Errorf("no live workspace host owner")
		}
	}
	closeErr := store.Close()
	if e != nil {
		return start(e)
	}
	if closeErr != nil {
		return start(closeErr)
	}
	u, e := url.Parse(descriptor.URL)
	if e != nil {
		return start(e)
	}
	port, pe := strconv.Atoi(u.Port())
	if u.Scheme != "http" || u.Hostname() != "127.0.0.1" || pe != nil || port < 1 || port > 65535 || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Host != net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) {
		return start(fmt.Errorf("invalid loopback descriptor"))
	}
	transport := &http.Transport{DialContext: (&net.Dialer{Timeout: time.Second}).DialContext, ResponseHeaderTimeout: 5 * time.Second, MaxResponseHeaderBytes: 16 << 10, MaxIdleConnsPerHost: 2}
	c := &Client{descriptor, &http.Client{Transport: transport, Timeout: 6 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	op, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var health struct {
		Status   string `json:"status"`
		Instance string `json:"instance_id"`
	}
	if e = c.Do(op, "GET", "/api/v1/health", nil, &health); e != nil {
		c.Close()
		return start(e)
	}
	if health.Status != "ready" || health.Instance != descriptor.InstanceID {
		c.Close()
		return start(fmt.Errorf("stale host instance"))
	}
	return c, nil
}
func (c *Client) Close() { c.http.CloseIdleConnections() }
func (c *Client) Do(ctx context.Context, method, path string, body, out any) error {
	if !strings.HasPrefix(path, "/api/v1/") || strings.HasPrefix(path, "//") {
		return fmt.Errorf("invalid product API path")
	}
	var reader io.Reader
	if body != nil {
		raw, e := json.Marshal(body)
		if e != nil {
			return e
		}
		reader = bytes.NewReader(raw)
	}
	req, e := http.NewRequestWithContext(ctx, method, c.descriptor.URL+path, reader)
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+c.descriptor.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, e := c.http.Do(req)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	if res.Header.Get("X-RunCradle-Instance") != c.descriptor.InstanceID {
		return fmt.Errorf("host instance mismatch; inspect the workspace descriptor")
	}
	raw, e := io.ReadAll(io.LimitReader(res.Body, 32<<20+1))
	if e != nil {
		return e
	}
	if len(raw) > 32<<20 {
		return fmt.Errorf("product response exceeds budget")
	}
	if out != nil {
		if e = json.Unmarshal(raw, out); e != nil {
			return fmt.Errorf("invalid product response: %w", e)
		}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		var message struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &message)
		if message.Error == "" {
			message.Error = http.StatusText(res.StatusCode)
		}
		return &HTTPError{res.StatusCode, message.Error}
	}
	return nil
}
