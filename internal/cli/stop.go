package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/url"
	"runcradle/internal/engine"
	"runcradle/internal/hostclient"
	"runcradle/internal/output"
	"time"
)

type stopOptions struct{ workspace, runID string }

func stopWorkflow(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	sinks, err := output.New(stdout, stderr, 2*time.Second)
	if err != nil {
		return 125
	}
	defer sinks.Close()
	result := remoteResult{out: sinks.Out}
	var opts stopOptions
	flags := flag.NewFlagSet("workflow stop", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&opts.workspace, "workspace", "", "")
	flags.StringVar(&opts.runID, "run-id", "", "")
	err = flags.Parse(args)
	result.runID = opts.runID
	if err != nil {
		return result.fail(2, err)
	}
	if opts.workspace == "" || flags.NArg() != 0 {
		return result.fail(2, fmt.Errorf("--workspace required; no positional arguments"))
	}
	if ctx.Err() != nil {
		return result.fail(CancellationCode(ctx), ctx.Err())
	}
	client, err := hostclient.Connect(ctx, opts.workspace)
	if err != nil {
		return result.fail(2, err)
	}
	defer client.Close()
	if opts.runID == "" {
		return result.fail(2, fmt.Errorf("--run-id required"))
	}
	var stopped engine.StopResult
	err = client.Do(ctx, "POST", "/api/v1/runs/"+url.PathEscape(opts.runID)+"/stop", map[string]int{"cancellation_code": 130}, &stopped)
	if err != nil {
		return result.fail(2, err)
	}
	return result.write(runtimeResult{Status: "stop_requested", RunID: opts.runID, Final: stopped.Terminal, Cleanup: stopped.Cleanup, Reason: stopped.Reason, TaskResult: "unknown", IOStatus: "unknown", HistoryIntegrity: "unknown", NextAction: "inspect run until terminal; stop acceptance does not prove cleanup"}, 0)
}
