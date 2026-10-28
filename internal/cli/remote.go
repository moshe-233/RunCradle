package cli

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"runcradle/internal/engine"
	"runcradle/internal/hostclient"
	"runcradle/internal/httpapi"
	"runcradle/internal/output"
	"runcradle/internal/service"
	"runcradle/internal/webui"
	"runcradle/internal/workflow"
	"time"
)

func serve(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	workspace := flags.String("workspace", "", "")
	port := flags.Int("port", 0, "")
	binary := flags.String("process-compose", "process-compose", "")
	if e := flags.Parse(args); e != nil {
		return failure(stdout, 2, "arguments", e.Error())
	}
	if *workspace == "" || flags.NArg() != 0 {
		return failure(stdout, 2, "arguments", "--workspace required; no positional arguments")
	}
	helper, e := os.Executable()
	if e != nil {
		return failure(stdout, 125, "host", e.Error())
	}
	server, e := httpapi.Open(httpapi.Config{Workspace: *workspace, Binary: *binary, Helper: helper, Port: *port, Page: webui.Handler()})
	if e != nil {
		return failure(stdout, 2, "host", e.Error())
	}
	sinks, e := output.New(stdout, stderr, 2*time.Second)
	if e != nil {
		server.Close()
		return 125
	}
	defer sinks.Close()
	raw, e := json.Marshal(server.Ready())
	if e == nil {
		_, e = sinks.Out.WriteContext(context.Background(), append(raw, '\n'))
	}
	if e != nil {
		server.Close()
		return 125
	}
	code := 0
	select {
	case <-ctx.Done():
		code = CancellationCode(ctx)
	case e = <-server.Done():
		if e != nil {
			code = 125
		}
	}
	if e = server.Close(); e != nil {
		return 125
	}
	return code
}

func remoteWorkflow(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	sinks, e := output.New(stdout, stderr, 2*time.Second)
	if e != nil {
		return 125
	}
	defer sinks.Close()
	requestID, runID := "", ""
	write := func(r runtimeResult, code int) int { return runtimeWrite(context.Background(), sinks.Out, r, code) }
	bad := func(code int, e error) int {
		return write(runtimeResult{Status: "error", RunID: runID, RequestID: requestID, Message: e.Error(), TaskResult: "unknown", Cleanup: "unknown", IOStatus: "unknown", HistoryIntegrity: "unknown", NextAction: "inspect this request before intentional retry; start serve explicitly if unavailable"}, code)
	}
	flags := flag.NewFlagSet("workflow "+args[1], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	workspace := flags.String("workspace", "", "")
	file, ui, workflowID, revision, preset, valueFile, legacyBinary := "", "", "", "", "", "", ""
	timeout := 300000
	retain := false
	var targets targetsFlag
	if args[1] == "stop" {
		flags.StringVar(&runID, "run-id", "", "")
	} else {
		flags.StringVar(&file, "file", "", "")
		flags.StringVar(&ui, "ui", "", "")
		flags.StringVar(&workflowID, "workflow-id", "", "")
		flags.StringVar(&revision, "revision", "", "")
		flags.StringVar(&preset, "preset", "", "")
		flags.StringVar(&valueFile, "values", "", "")
		flags.StringVar(&requestID, "request-id", "", "")
		flags.StringVar(&legacyBinary, "process-compose", "", "")
		flags.IntVar(&timeout, "timeout-ms", 300000, "")
		flags.BoolVar(&retain, "retain-evidence", false, "")
		flags.Var(&targets, "target", "")
	}
	if e = flags.Parse(args[2:]); e != nil {
		return bad(2, e)
	}
	if *workspace == "" || flags.NArg() != 0 {
		return bad(2, fmt.Errorf("--workspace required; no positional arguments"))
	}
	if legacyBinary != "" {
		return bad(2, fmt.Errorf("engine selection moved to runcradle serve --workspace %q --process-compose %q", *workspace, legacyBinary))
	}
	if ctx.Err() != nil {
		return bad(CancellationCode(ctx), ctx.Err())
	}
	client, e := hostclient.Connect(ctx, *workspace)
	if e != nil {
		return bad(2, e)
	}
	defer client.Close()
	if args[1] == "stop" {
		if runID == "" {
			return bad(2, fmt.Errorf("--run-id required"))
		}
		var stopped engine.StopResult
		e = client.Do(ctx, "POST", "/api/v1/runs/"+url.PathEscape(runID)+"/stop", map[string]int{"cancellation_code": 130}, &stopped)
		if e != nil {
			return bad(2, e)
		}
		return write(runtimeResult{Status: "stop_requested", RunID: runID, Final: stopped.Terminal, Cleanup: stopped.Cleanup, Reason: stopped.Reason, TaskResult: "unknown", IOStatus: "unknown", HistoryIntegrity: "unknown", NextAction: "inspect run until terminal; stop acceptance does not prove cleanup"}, 0)
	}
	if (file == "") == (workflowID == "") || workflowID != "" && revision == "" || file != "" && revision != "" || file == "" && ui != "" || len(targets) == 0 || timeout < 1 || timeout > 3600000 {
		return bad(2, fmt.Errorf("choose --file or --workflow-id/--revision; explicit target and timeout 1..3600000 required"))
	}
	if requestID == "" {
		var id [16]byte
		if _, e = rand.Read(id[:]); e != nil {
			return bad(125, e)
		}
		requestID = fmt.Sprintf("%x", id)
	}
	// Query first: source/dependency errors must not hide an existing submission.
	var previous struct {
		RunID string `json:"run_id"`
	}
	queryErr := client.Do(ctx, "GET", "/api/v1/run-requests/"+url.PathEscape(requestID), nil, &previous)
	if queryErr == nil {
		runID = previous.RunID
	} else {
		var httpErr *hostclient.HTTPError
		if !errors.As(queryErr, &httpErr) || httpErr.Status != 404 {
			return bad(125, queryErr)
		}
	}
	input := httpapi.RunRequest{WorkflowID: workflowID, Revision: revision, RequestID: requestID, Targets: targets, TimeoutMS: int64(timeout), Preset: preset, Retain: retain}
	if file != "" {
		d, e := workflow.Load(*workspace, file, ui)
		if e != nil {
			return bad(2, e)
		}
		if !d.Valid() {
			return bad(2, fmt.Errorf("invalid native definition; old v2 policies require explicit migration"))
		}
		draft := httpapi.Draft{Format: d.Format, Source: string(d.Source), Entry: d.Entry, UIPath: d.UIPath}
		if d.UIPath != "" {
			text := string(d.UIRaw)
			draft.UISource = &text
		}
		input.Document = &draft
		input.Revision = d.Revision
	}
	if valueFile != "" {
		raw, _, e := workflow.ReadFile(*workspace, valueFile, workflow.MaxUI)
		if e != nil {
			return bad(2, e)
		}
		if e = json.Unmarshal(raw, &input.Values); e != nil || input.Values == nil {
			return bad(2, fmt.Errorf("--values must contain a JSON object"))
		}
	}
	var sub service.Submission
	e = client.Do(ctx, "POST", "/api/v1/runs", input, &sub)
	if sub.RunID != "" {
		runID = sub.RunID
	}
	if e != nil {
		var httpErr *hostclient.HTTPError
		if errors.As(e, &httpErr) {
			if httpErr.Status == 429 && sub.RunID != "" {
				var known engine.Snapshot
				if readErr := client.Do(context.Background(), "GET", "/api/v1/runs/"+url.PathEscape(sub.RunID), nil, &known); readErr == nil {
					result := runtimeSummary(known, requestID)
					result.Message = e.Error()
					return write(result, 2)
				}
			}
			code := 2
			if httpErr.Status >= 500 {
				code = 125
			}
			return bad(code, e)
		}
		op, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		var known struct {
			RunID string `json:"run_id"`
		}
		lookup := client.Do(op, "GET", "/api/v1/run-requests/"+url.PathEscape(requestID), nil, &known)
		cancel()
		if lookup != nil {
			return bad(125, fmt.Errorf("submission response uncertain: %w", e))
		}
		runID = known.RunID
	}
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	hard := time.NewTimer(time.Duration(timeout)*time.Millisecond + 25*time.Second)
	defer hard.Stop()
	for {
		var observed struct {
			engine.Snapshot
			Owned bool `json:"owned"`
		}
		op, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		e = client.Do(op, "GET", "/api/v1/runs/"+url.PathEscape(runID), nil, &observed)
		cancel()
		state := observed.Snapshot
		if e != nil && ctx.Err() == nil {
			return bad(125, e)
		}
		if ctx.Err() != nil {
			code := CancellationCode(ctx)
			op, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			var stop engine.StopResult
			e = client.Do(op, "POST", "/api/v1/runs/"+url.PathEscape(runID)+"/stop", map[string]int{"cancellation_code": code}, &stop)
			cancel()
			if e != nil {
				return bad(code, fmt.Errorf("stop unconfirmed; inspect run: %w", e))
			}
			deadline := time.NewTimer(11 * time.Second)
			defer deadline.Stop()
			for {
				op, cancel := context.WithTimeout(context.Background(), time.Second)
				e = client.Do(op, "GET", "/api/v1/runs/"+url.PathEscape(runID), nil, &state)
				cancel()
				if e == nil && state.Final {
					return write(runtimeSummary(state, requestID), code)
				}
				select {
				case <-deadline.C:
					return bad(code, fmt.Errorf("stop observation incomplete; inspect run"))
				case <-tick.C:
				}
			}
		}
		if state.Final || state.Reason == "interrupted" || state.Reason == "owner_interrupted" {
			return write(runtimeSummary(state, requestID), service.ExitCode(state))
		}
		if !observed.Owned {
			result := runtimeSummary(state, requestID)
			result.Message = "submission has no live owner; inspect this run before intentional retry"
			return write(result, 125)
		}
		select {
		case <-ctx.Done():
		case <-tick.C:
		case <-hard.C:
			return bad(125, fmt.Errorf("client observation exceeded budget; host still owns accepted run"))
		}
	}
}
