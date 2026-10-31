package hostclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"runcradle/internal/output"
	"runcradle/internal/protocol"
	"strings"
)

type Connection struct {
	URL            string `json:"url"`
	InstanceID     string `json:"instance_id"`
	CredentialFile string `json:"credential_file"`
}

func (c *Client) Connection() Connection {
	return Connection{c.descriptor.URL, c.descriptor.InstanceID, filepath.Join(c.descriptor.Workspace, ".runcradle/server/connection.json")}
}

// ValidatePath forbids URL resolution/redirect tricks before adding credentials.
func ValidatePath(value string) error {
	u, err := url.ParseRequestURI(value)
	if err != nil || u.IsAbs() || u.Host != "" || u.Fragment != "" || strings.Contains(value, "#") || !strings.HasPrefix(u.Path, "/api/v1/") || path.Clean(u.Path) != u.Path || strings.Contains(u.Path, "\\") {
		return fmt.Errorf("invalid product API path")
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return fmt.Errorf("invalid product API query")
	}
	for name := range query {
		switch strings.ToLower(name) {
		case "token", "access_token", "authorization":
			return fmt.Errorf("credentials are not permitted in API URLs")
		}
	}
	return nil
}

type Response struct {
	Status      int
	ContentType string
	Body        []byte
}

// Request is finite and bounded; streaming consumers use the documented SSE
// endpoint directly. It never retries a write or follows a redirect.
func (c *Client) Request(ctx context.Context, method, resource string, body []byte) (result Response, err error) {
	if err = ValidatePath(resource); err != nil {
		return result, err
	}
	if len(body) > protocol.MaxRequestBytes {
		return result, fmt.Errorf("product request exceeds budget")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.descriptor.URL+resource, bytes.NewReader(body))
	if err != nil {
		return result, err
	}
	req.Header.Set("Authorization", "Bearer "+c.descriptor.Token)
	req.Header.Set("Accept", "application/json, application/zip, text/html")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, res.Body.Close()) }()
	if res.Header.Get("X-RunCradle-Instance") != c.descriptor.InstanceID {
		return result, fmt.Errorf("host instance mismatch; inspect the workspace descriptor")
	}
	result.Status, result.ContentType = res.StatusCode, res.Header.Get("Content-Type")
	raw, err := io.ReadAll(io.LimitReader(res.Body, protocol.MaxResponseBytes+1))
	if err != nil {
		return result, err
	}
	if len(raw) > protocol.MaxResponseBytes {
		return result, fmt.Errorf("product response exceeds budget")
	}
	result.Body = raw
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return result, nil
	}
	problem := protocol.ProblemFor(res.StatusCode, http.StatusText(res.StatusCode))
	// A malformed error body must not erase the authoritative HTTP classification.
	var supplied protocol.Problem
	if json.Unmarshal(raw, &supplied) == nil {
		if supplied.Message != "" {
			problem.Message = output.Clean(supplied.Message, 512)
		}
		if supplied.Code != "" {
			problem.Code = output.Clean(supplied.Code, 64)
		}
		if supplied.NextAction != "" {
			problem.NextAction = output.Clean(supplied.NextAction, 512)
		}
	}
	return result, &HTTPError{Status: res.StatusCode, Message: problem.Message, Code: problem.Code, NextAction: problem.NextAction}
}

func (c *Client) Do(ctx context.Context, method, resource string, body, out any) error {
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	res, err := c.Request(ctx, method, resource, raw)
	if err != nil {
		var httpErr *HTTPError
		// Preserve any known submission identity, but a decode failure cannot
		// replace the HTTP error. Never decode an untrusted/stale-instance body.
		if out != nil && errors.As(err, &httpErr) {
			_ = json.Unmarshal(res.Body, out)
		}
		return err
	}
	if out != nil {
		if err := json.Unmarshal(res.Body, out); err != nil {
			return fmt.Errorf("invalid product response: %w", err)
		}
	}
	return nil
}
