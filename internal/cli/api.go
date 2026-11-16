package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"runcradle/internal/hostclient"
	"runcradle/internal/httpapi"
	"runcradle/internal/output"
	"runcradle/internal/protocol"
	"runcradle/internal/workflow"
	"time"
)

type apiReceipt struct {
	Status          string `json:"status"`
	ContractVersion string `json:"contract_version"`
	HTTPStatus      int    `json:"http_status,omitempty"`
	Output          string `json:"output,omitempty"`
	Bytes           int    `json:"bytes,omitempty"`
	SHA256          string `json:"sha256,omitempty"`
	ContentType     string `json:"content_type,omitempty"`
	Code            string `json:"code,omitempty"`
	Message         string `json:"error,omitempty"`
	RunID           string `json:"run_id,omitempty"`
	RequestID       string `json:"request_id,omitempty"`
	Revision        string `json:"revision,omitempty"`
	Accepted        *bool  `json:"accepted,omitempty"`
	NextAction      string `json:"next_action"`
}

func apiJSON(out *output.Sink, value any, code int) int {
	raw, err := json.Marshal(value)
	if err != nil || len(raw)+1 > protocol.MaxCLIBytes {
		raw = []byte(`{"status":"error","code":"output_budget_exceeded","next_action":"use the explicit response file; do not repeat a write without reconciling its request"}`)
		code = 125
	}
	n, err := out.WriteContext(context.Background(), append(raw, '\n'))
	if err != nil || n != len(raw)+1 {
		return 125
	}
	return code
}

func apiCommandRun(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	sinks, err := output.New(stdout, stderr, 2*time.Second)
	if err != nil {
		return 125
	}
	defer sinks.Close()
	receipt := apiReceipt{ContractVersion: protocol.ContractVersion, NextAction: "use api describe or api schema for supported operations"}
	bad := func(code int, kind string, err error) int {
		receipt.Status, receipt.Code, receipt.Message = "error", kind, output.Clean(err.Error(), 512)
		return apiJSON(sinks.Out, receipt, code)
	}
	if len(args) == 1 && args[0] == "describe" {
		return apiJSON(sinks.Out, protocol.Describe(), 0)
	}
	if len(args) == 0 || (args[0] != "schema" && args[0] != "status" && args[0] != "request") {
		return bad(2, "invalid_request", fmt.Errorf("choose api describe, schema, status or request"))
	}
	command := args[0]
	f := flag.NewFlagSet("api "+command, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	workspace := f.String("workspace", "", "")
	name, method, resource, input := "", "GET", "", ""
	timeout := 6000
	if command != "status" {
		f.StringVar(&name, "out", "", "")
	}
	if command == "request" {
		f.StringVar(&method, "method", "GET", "")
		f.StringVar(&resource, "path", "", "")
		f.StringVar(&input, "input", "", "")
		f.IntVar(&timeout, "timeout-ms", 6000, "")
	}
	if err := f.Parse(args[1:]); err != nil {
		return bad(2, "invalid_request", err)
	}
	if *workspace == "" || f.NArg() != 0 || timeout < 1 || timeout > 6000 {
		return bad(2, "invalid_request", fmt.Errorf("--workspace required, timeout 1..6000ms; no positional arguments"))
	}
	if ctx.Err() != nil {
		return bad(CancellationCode(ctx), "cancelled", ctx.Err())
	}
	if command == "status" {
		client, err := hostclient.Connect(ctx, *workspace)
		if err != nil {
			code, kind := apiConnectionFailure(ctx, err)
			return bad(code, kind, err)
		}
		defer client.Close()
		return apiJSON(sinks.Out, struct {
			hostclient.Connection
			Status     string `json:"status"`
			NextAction string `json:"next_action"`
		}{client.Connection(), "ready", "use this live instance; credentials remain in the private file"}, 0)
	}
	var body []byte
	if command == "request" {
		if method != "GET" && method != "POST" && method != "PUT" && method != "DELETE" {
			return bad(2, "invalid_request", fmt.Errorf("method must be GET, POST, PUT or DELETE"))
		}
		if err := hostclient.ValidatePath(resource); err != nil {
			return bad(2, "invalid_request", err)
		}
		if method == "GET" && input != "" || method != "GET" && input == "" {
			return bad(2, "invalid_request", fmt.Errorf("GET has no input; POST/PUT/DELETE require --input containing one JSON object"))
		}
		if input != "" {
			body, _, err = workflow.ReadFile(*workspace, input, protocol.MaxRequestBytes)
			if err != nil {
				return bad(2, "invalid_request", err)
			}
			trimmed := bytes.TrimSpace(body)
			if !json.Valid(body) || len(trimmed) == 0 || trimmed[0] != '{' {
				return bad(2, "invalid_request", fmt.Errorf("--input must contain exactly one JSON object"))
			}
			var identity struct {
				RequestID string `json:"request_id"`
			}
			if json.Unmarshal(body, &identity) == nil {
				receipt.RequestID = output.Clean(identity.RequestID, 128)
			}
		}
	}
	artifact, err := reserveAPIFile(*workspace, name)
	if err != nil {
		return bad(2, "invalid_output", err)
	}
	if command == "schema" {
		raw, err := json.Marshal(httpapi.OpenAPI())
		if err != nil {
			return bad(125, "internal_error", errors.Join(err, artifact.abort()))
		}
		if err := artifact.finish(raw); err != nil {
			return bad(125, "output_failed", err)
		}
		receipt.Status, receipt.Output, receipt.Bytes, receipt.SHA256 = "exported", artifact.name, len(raw), fmt.Sprintf("%x", sha256.Sum256(raw))
		return apiJSON(sinks.Out, receipt, 0)
	}
	op, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
	defer cancel()
	client, err := hostclient.Connect(op, *workspace)
	if err != nil {
		code, kind := apiConnectionFailure(ctx, err)
		return bad(code, kind, errors.Join(err, artifact.abort()))
	}
	defer client.Close()
	res, requestErr := client.Request(op, method, resource, body)
	receipt.HTTPStatus = res.Status
	var httpErr *hostclient.HTTPError
	if requestErr != nil && !errors.As(requestErr, &httpErr) {
		code, kind := 125, "transport_error"
		if ctx.Err() != nil {
			code, kind = CancellationCode(ctx), "cancelled"
		} else if errors.Is(requestErr, context.DeadlineExceeded) {
			code, kind = 124, "deadline_exceeded"
		}
		receipt.NextAction = "inspect the original request_id before retrying; transport failure does not prove a write was rejected"
		return bad(code, kind, errors.Join(requestErr, artifact.abort()))
	}
	var identity struct {
		RunID     string `json:"run_id"`
		RequestID string `json:"request_id"`
		Revision  string `json:"revision"`
		Accepted  *bool  `json:"accepted"`
	}
	if json.Unmarshal(res.Body, &identity) == nil {
		receipt.RunID, receipt.Revision, receipt.Accepted = output.Clean(identity.RunID, 128), output.Clean(identity.Revision, 128), identity.Accepted
		if identity.RequestID != "" {
			receipt.RequestID = output.Clean(identity.RequestID, 128)
		}
	}
	if err := artifact.finish(res.Body); err != nil {
		receipt.NextAction = "response could not be saved; reconcile the original request_id before retrying a write"
		return bad(125, "output_failed", err)
	}
	receipt.Status, receipt.Output, receipt.Bytes, receipt.SHA256 = "ok", artifact.name, len(res.Body), fmt.Sprintf("%x", sha256.Sum256(res.Body))
	receipt.ContentType = output.Clean(res.ContentType, 128)
	receipt.NextAction = "read the response file; successful API transport does not imply a successful task"
	if httpErr != nil {
		code := 2
		if httpErr.Status >= 500 {
			code = 125
		}
		receipt.NextAction = httpErr.NextAction
		return bad(code, httpErr.Code, httpErr)
	}
	return apiJSON(sinks.Out, receipt, 0)
}

func apiConnectionFailure(ctx context.Context, err error) (int, string) {
	if ctx.Err() != nil {
		return CancellationCode(ctx), "cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return 124, "deadline_exceeded"
	}
	return 2, "host_unavailable"
}
