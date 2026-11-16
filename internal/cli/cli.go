package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"runcradle/internal/buildinfo"
	"runcradle/internal/processcompose"
	"runcradle/internal/webui"
	"runcradle/internal/workflow"
	"time"
)

const help = `RunCradle — 把工作流变成可操作的界面。

Usage:
  runcradle help
  runcradle version
  runcradle api describe
  runcradle api schema --workspace DIR --out FILE
  runcradle api status --workspace DIR
  runcradle api request --workspace DIR --path /api/v1/RESOURCE --out FILE
                       [--method GET|POST|PUT|DELETE] [--input JSON_FILE] [--timeout-ms N]
  runcradle workflow validate --workspace DIR --file FILE [--ui FILE]
                              [--check-engine] [--process-compose PATH]

  runcradle workflow render --workspace DIR --file FILE [--ui FILE] --out FILE [--force]

  runcradle serve --workspace DIR [--port PORT] [--process-compose PATH]
  runcradle workflow run --workspace DIR
                         (--file FILE [--ui FILE] | --workflow-id ID --revision REV)
                         --target NAME [--target NAME ...]
                         [--preset ID] [--values FILE] [--timeout-ms N] [--request-id ID]
                         [--retain-evidence] [--detach --request-id ID]
  runcradle workflow stop --workspace DIR --run-id ID
  runcradle workflow inspect --workspace DIR (--run-id ID | --request-id ID)
                             [--node NAME | --events-after SEQ | --nodes-after N] [--limit N]

Validate/render/inspect work offline. Run/stop connect to an explicit workspace host.
API requests save complete responses to new files; stdout is a bounded JSON receipt.
Detached run exit 0 confirms host acceptance only. Use inspect/events for task facts.
Serve provides local browser editing, immutable library revisions and bounded task runs.
Import sources remain unchanged; save writes the private library and export is explicit.
Engine selection belongs to serve. Old v2 policies need explicit migration.
`

type SignalCause int

func (s SignalCause) Error() string     { return fmt.Sprintf("signal %d", s) }
func (s SignalCause) SignalNumber() int { return int(s) }
func CancellationCode(ctx context.Context) int {
	if s, ok := context.Cause(ctx).(SignalCause); ok {
		return 128 + int(s)
	}
	return 130
}
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "api" {
		return apiCommandRun(ctx, args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "serve" {
		return serve(ctx, args[1:], stdout, stderr)
	}
	if len(args) >= 2 && args[0] == "workflow" {
		switch args[1] {
		case "run":
			return runWorkflow(ctx, args[2:], stdout, stderr)
		case "stop":
			return stopWorkflow(ctx, args[2:], stdout, stderr)
		case "inspect":
			return inspectWorkflow(ctx, args, stdout, stderr)
		}
	}
	if len(args) == 0 || (len(args) == 1 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h")) {
		if _, err := io.WriteString(stdout, help); err != nil {
			return 125
		}
		return 0
	}
	if len(args) == 1 && args[0] == "version" {
		if _, err := io.WriteString(stdout, "runcradle "+buildinfo.Version+"\n"); err != nil {
			return 125
		}
		return 0
	}
	if len(args) < 2 || args[0] != "workflow" || (args[1] != "validate" && args[1] != "render") {
		return failure(stdout, 2, "arguments", "unsupported command; run runcradle help")
	}
	rendering := args[1] == "render"
	flags := flag.NewFlagSet("workflow "+args[1], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	workspace := flags.String("workspace", "", "authorized workspace")
	file := flags.String("file", "", "native JSON/YAML file")
	ui := flags.String("ui", "", "optional UI JSON")
	output := ""
	force := false
	checkEngine := false
	binary := ""
	if !rendering {
		flags.BoolVar(&checkEngine, "check-engine", false, "explicit fixed upstream dry-run")
		flags.StringVar(&binary, "process-compose", "", "engine binary path")
	}
	if rendering {
		flags.StringVar(&output, "out", "", "explicit output HTML")
		flags.BoolVar(&force, "force", false, "replace existing output")
	}
	if err := flags.Parse(args[2:]); err != nil {
		return failure(stdout, 2, "arguments", err.Error())
	}
	if *workspace == "" || *file == "" || flags.NArg() != 0 || (rendering && output == "") || (flagSet(flags, "process-compose") && !checkEngine) {
		return failure(stdout, 2, "arguments", "--workspace and --file are required; positional arguments are not accepted")
	}
	if ctx.Err() != nil {
		return failure(stdout, CancellationCode(ctx), "cancelled", "operation cancelled")
	}
	d, err := workflow.Load(*workspace, *file, *ui)
	if err != nil {
		return failure(stdout, 2, "input", err.Error())
	}
	r := Result{Status: "static_valid", Revision: d.Revision, NodeCount: len(d.View.Nodes), EdgeCount: len(d.View.Edges), ViewComplete: d.View.Complete, EngineCheck: "not_requested", Issues: d.Issues, NextAction: "static checks only; upstream acceptance and execution are not established"}
	if !d.Valid() {
		r.Status = "invalid"
		r.ViewComplete = false
		r.NextAction = "fix configuration issues before rendering"
		return writeResult(stdout, r, 2)
	}
	if ctx.Err() != nil {
		return failure(stdout, CancellationCode(ctx), "cancelled", "operation cancelled")
	}
	if checkEngine {
		resolved, err := resolveEngine(ctx, binary, flagSet(flags, "process-compose"))
		if err != nil {
			r.Status = "engine_check_failed"
			r.EngineCheck = "unavailable"
			r.Issues = append(r.Issues, Issue{Severity: "error", Code: "engine_unavailable", Message: err.Error()})
			r.NextAction = "check engine installation or --process-compose; no workflow execution occurred"
			return writeResult(stdout, r, engineResolutionCode(ctx, err))
		}
		checked := processcompose.Check(ctx, d, resolved.Path)
		r.EngineCheck = checked.Status
		r.EngineExitCode = checked.EngineExit
		if checked.Code != 0 {
			r.Status = "engine_check_failed"
			r.Issues = append(r.Issues, Issue{Severity: "error", Code: "engine_" + checked.Status, Message: checked.Message})
			r.NextAction = "review engine_check and the fixed dependency; no workflow execution occurred"
			code := checked.Code
			if ctx.Err() != nil {
				code = CancellationCode(ctx)
			}
			return writeResult(stdout, r, code)
		}
		r.Status = "validated"
		r.NextAction = checked.Message
	}
	if rendering {
		page, err := webui.Render(d, time.Now())
		if err != nil {
			return failure(stdout, 125, "render", err.Error())
		}
		if ctx.Err() != nil {
			return failure(stdout, CancellationCode(ctx), "cancelled", "operation cancelled")
		}
		r.Output, err = workflow.WriteOutput(d, output, page, force)
		if err != nil {
			code := 125
			if errors.Is(err, workflow.ErrOutputPath) {
				code = 2
			}
			return failure(stdout, code, "output", err.Error())
		}
		r.Status = "rendered"
		r.NextAction = "open the exported HTML; it displays declarations and has no execution controls"
	}
	return writeResult(stdout, r, 0)
}
