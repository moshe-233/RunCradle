package hostclient

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"runcradle/internal/runs"
	"strconv"
	"time"
)

type Client struct {
	descriptor runs.ServerDescriptor
	http       *http.Client
}
type HTTPError struct {
	Status     int
	Message    string
	Code       string
	NextAction string
}

func (e *HTTPError) Error() string { return e.Message }
func Connect(ctx context.Context, workspace string) (*Client, error) {
	start := func(e error) (*Client, error) {
		return nil, fmt.Errorf("workspace host unavailable: %w; start runcradle serve --workspace %q", e, workspace)
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
