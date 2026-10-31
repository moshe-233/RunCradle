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
	"runcradle/internal/engine"
	"runcradle/internal/hostclient"
	"runcradle/internal/output"
	"runcradle/internal/protocol"
	"runcradle/internal/service"
	"runcradle/internal/workflow"
	"time"
)

// runOptions 只保存命令输入；来源校验保持在连接宿主之后，以保留错误优先级。
type runOptions struct {
	workspace, file, ui, workflowID, revision, preset, valueFile, requestID, legacyBinary string
	timeout                                                                               int
	retain                                                                                bool
	detach                                                                                bool
	targets                                                                               targetsFlag
}

func parseRunOptions(args []string) (runOptions, error) {
	var opts runOptions
	flags := flag.NewFlagSet("workflow run", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&opts.workspace, "workspace", "", "")
	flags.StringVar(&opts.file, "file", "", "")
	flags.StringVar(&opts.ui, "ui", "", "")
	flags.StringVar(&opts.workflowID, "workflow-id", "", "")
	flags.StringVar(&opts.revision, "revision", "", "")
	flags.StringVar(&opts.preset, "preset", "", "")
	flags.StringVar(&opts.valueFile, "values", "", "")
	flags.StringVar(&opts.requestID, "request-id", "", "")
	flags.StringVar(&opts.legacyBinary, "process-compose", "", "")
	flags.IntVar(&opts.timeout, "timeout-ms", 300000, "")
	flags.BoolVar(&opts.retain, "retain-evidence", false, "")
	flags.BoolVar(&opts.detach, "detach", false, "return after host acceptance")
	flags.Var(&opts.targets, "target", "")
	if err := flags.Parse(args); err != nil {
		return opts, err
	}
	if opts.workspace == "" || flags.NArg() != 0 {
		return opts, fmt.Errorf("--workspace required; no positional arguments")
	}
	if opts.detach && opts.requestID == "" {
		return opts, fmt.Errorf("--detach requires an explicit --request-id for reconciliation")
	}
	if opts.legacyBinary != "" {
		return opts, fmt.Errorf("engine selection moved to runcradle serve --workspace %q --process-compose %q", opts.workspace, opts.legacyBinary)
	}
	return opts, nil
}

func (opts runOptions) validSource() bool {
	if (opts.file == "") == (opts.workflowID == "") {
		return false
	}
	if opts.workflowID != "" && opts.revision == "" {
		return false
	}
	if opts.file != "" && opts.revision != "" {
		return false
	}
	if opts.file == "" && opts.ui != "" {
		return false
	}
	return len(opts.targets) > 0 && opts.timeout >= 1 && opts.timeout <= 3600000
}

// remoteResult 保留已知身份；提交结果不确定时，错误输出仍能用于查询原请求。
type remoteResult struct {
	out              *output.Sink
	requestID, runID string
	submission       protocol.Submission
}

func (result *remoteResult) write(value runtimeResult, code int) int {
	return runtimeWrite(context.Background(), result.out, value, code)
}
func (result *remoteResult) fail(code int, err error) int {
	kind := "internal_error"
	if code == 2 {
		kind = "invalid_request"
	} else if code == 124 {
		kind = "deadline_exceeded"
	} else if code >= 128 {
		kind = "cancelled"
	}
	var httpErr *hostclient.HTTPError
	if errors.As(err, &httpErr) {
		kind = httpErr.Code
	}
	return result.write(runtimeResult{Status: "error", Code: kind, RunID: result.runID, RequestID: result.requestID, Message: err.Error(), TaskResult: "unknown", Cleanup: "unknown", IOStatus: "unknown", HistoryIntegrity: "unknown", NextAction: "inspect this request before intentional retry; start serve explicitly if unavailable"}, code)
}

func runWorkflow(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	sinks, err := output.New(stdout, stderr, 2*time.Second)
	if err != nil {
		return 125
	}
	defer sinks.Close()
	opts, err := parseRunOptions(args)
	result := remoteResult{out: sinks.Out, requestID: opts.requestID}
	if err != nil {
		return result.fail(2, err)
	}
	if ctx.Err() != nil {
		return result.fail(CancellationCode(ctx), ctx.Err())
	}
	client, err := hostclient.Connect(ctx, opts.workspace)
	if err != nil {
		return result.fail(2, err)
	}
	defer client.Close()
	if !opts.validSource() {
		return result.fail(2, fmt.Errorf("choose --file or --workflow-id/--revision; explicit target and timeout 1..3600000 required"))
	}
	if result.requestID == "" {
		var id [16]byte
		if _, err = rand.Read(id[:]); err != nil {
			return result.fail(125, err)
		}
		result.requestID = fmt.Sprintf("%x", id)
	}
	// 先找原请求，再读本地文件；文件后来损坏也不能遮蔽已知的 run_id。
	previous, err := lookupRequest(ctx, client, result.requestID)
	if err != nil {
		return result.fail(125, err)
	}
	result.runID = previous
	input, err := opts.submission(result.requestID)
	if err != nil {
		return result.fail(2, err)
	}
	if code, done := result.submit(ctx, client, input); done {
		return code
	}
	if opts.detach {
		if !result.submission.Accepted || result.runID == "" {
			return result.fail(125, fmt.Errorf("submission acceptance is uncertain; inspect the original request_id"))
		}
		accepted := true
		return result.write(runtimeResult{Status: "accepted", Accepted: &accepted, RunID: result.runID, RequestID: result.requestID, Revision: input.Revision, TaskResult: "unknown", Cleanup: "unknown", IOStatus: "unknown", HistoryIntegrity: "unknown", NextAction: "inspect the run or subscribe to events; exit 0 confirms acceptance only; stop explicitly when needed"}, 0)
	}
	return result.observe(ctx, client, opts.timeout)
}

func lookupRequest(ctx context.Context, client *hostclient.Client, id string) (string, error) {
	var known struct {
		RunID string `json:"run_id"`
	}
	err := client.Do(ctx, "GET", "/api/v1/run-requests/"+url.PathEscape(id), nil, &known)
	var httpErr *hostclient.HTTPError
	if errors.As(err, &httpErr) && httpErr.Status == 404 {
		return "", nil
	}
	return known.RunID, err
}

func (opts runOptions) submission(requestID string) (protocol.RunRequest, error) {
	input := protocol.RunRequest{WorkflowID: opts.workflowID, Revision: opts.revision, RequestID: requestID, Targets: opts.targets, TimeoutMS: int64(opts.timeout), Preset: opts.preset, Retain: opts.retain}
	if opts.file != "" {
		document, err := workflow.Load(opts.workspace, opts.file, opts.ui)
		if err != nil {
			return input, err
		}
		if !document.Valid() {
			return input, fmt.Errorf("invalid native definition; old v2 policies require explicit migration")
		}
		draft := protocol.Draft{Format: document.Format, Source: string(document.Source), Entry: document.Entry, UIPath: document.UIPath}
		if document.UIPath != "" {
			text := string(document.UIRaw)
			draft.UISource = &text
		}
		input.Document, input.Revision = &draft, document.Revision
	}
	if opts.valueFile != "" {
		raw, _, err := workflow.ReadFile(opts.workspace, opts.valueFile, workflow.MaxUI)
		if err != nil {
			return input, err
		}
		if err = json.Unmarshal(raw, &input.Values); err != nil || input.Values == nil {
			return input, fmt.Errorf("--values must contain a JSON object")
		}
	}
	return input, nil
}

// submit 返回 done 时已写出结果；网络错误只查询原请求，不更换 ID 再执行。
func (result *remoteResult) submit(ctx context.Context, client *hostclient.Client, input protocol.RunRequest) (code int, done bool) {
	var sub service.Submission
	err := client.Do(ctx, "POST", "/api/v1/runs", input, &sub)
	if sub.RunID != "" {
		result.runID = sub.RunID
	}
	if err == nil {
		result.submission = sub
		return 0, false
	}
	var httpErr *hostclient.HTTPError
	if errors.As(err, &httpErr) {
		if httpErr.Status == 429 && sub.RunID != "" {
			var known engine.Snapshot
			if readErr := client.Do(context.Background(), "GET", "/api/v1/runs/"+url.PathEscape(sub.RunID), nil, &known); readErr == nil {
				summary := runtimeSummary(known, result.requestID)
				summary.Message = err.Error()
				return result.write(summary, 2), true
			}
		}
		code := 2
		if httpErr.Status >= 500 {
			code = 125
		}
		return result.fail(code, err), true
	}
	op, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var known struct {
		RunID    string          `json:"run_id"`
		Snapshot engine.Snapshot `json:"snapshot"`
	}
	lookupErr := client.Do(op, "GET", "/api/v1/run-requests/"+url.PathEscape(result.requestID), nil, &known)
	if lookupErr != nil {
		return result.fail(125, fmt.Errorf("submission response uncertain: %w", err)), true
	}
	result.runID = known.RunID
	if known.Snapshot.Reason == "not_submitted" {
		return result.write(runtimeSummary(known.Snapshot, result.requestID), 2), true
	}
	result.submission = protocol.Submission{RunID: known.RunID, RequestID: result.requestID, Revision: input.Revision, Accepted: known.Snapshot.Reason != "" && known.Snapshot.Reason != "submission_unknown"}
	return 0, false
}
