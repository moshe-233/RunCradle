package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"runcradle/internal/engine"
	"runcradle/internal/output"
	"runcradle/internal/runs"
	"runcradle/internal/service"
	"strconv"
	"strings"
	"time"
)

type targetsFlag []string

func (v *targetsFlag) String() string     { return strings.Join(*v, ",") }
func (v *targetsFlag) Set(s string) error { *v = append(*v, s); return nil }

type runtimeResult struct {
	StopObservation  string         `json:"stop_observation,omitempty"`
	ResidualPath     string         `json:"residual_path,omitempty"`
	EngineSignal     int            `json:"engine_signal,omitempty"`
	TargetCount      int            `json:"target_count"`
	TargetNodes      []string       `json:"target_nodes,omitempty"`
	NextNodeOffset   int            `json:"next_node_offset,omitempty"`
	PagingNodes      bool           `json:"-"`
	NodeOffset       int            `json:"-"`
	CancellationCode int            `json:"cancellation_code,omitempty"`
	Status           string         `json:"status"`
	RunID            string         `json:"run_id,omitempty"`
	RequestID        string         `json:"request_id,omitempty"`
	Revision         string         `json:"revision,omitempty"`
	Final            bool           `json:"final"`
	TaskResult       string         `json:"task_result"`
	EngineExitCode   *int           `json:"engine_exit_code"`
	Cleanup          string         `json:"cleanup"`
	CleanupScope     string         `json:"cleanup_scope,omitempty"`
	HistoryIntegrity string         `json:"history_integrity"`
	IOStatus         string         `json:"io_status"`
	Reason           string         `json:"reason"`
	Message          string         `json:"message,omitempty"`
	NodeCount        int            `json:"node_count"`
	Nodes            []engine.Node  `json:"nodes,omitempty"`
	NodesTruncated   bool           `json:"details_truncated,omitempty"`
	Declaration      map[string]any `json:"declaration,omitempty"`
	Events           []engine.Event `json:"events,omitempty"`
	NextSeq          uint64         `json:"next_seq,omitempty"`
	NextAction       string         `json:"next_action"`
}

func runtimeSummary(state engine.Snapshot, request string) runtimeResult {
	status := "incomplete"
	if state.CancellationCode >= 128 && state.CancellationCode <= 192 {
		status = "cancelled"
	} else if !state.Final {
		status = "in_progress"
	} else if state.Reason == "not_submitted" {
		status = "not_started"
	} else if state.Reason == "cancelled" {
		status = "cancelled"
	} else if state.Reason == "deadline" {
		status = "timed_out"
	} else if state.TaskResult == "failed" {
		status = "failed"
	} else if service.ExitCode(state) == 0 {
		status = "succeeded"
	}
	r := runtimeResult{Status: status, RunID: state.RunID, RequestID: request, Revision: state.Revision, Final: state.Final, TaskResult: state.TaskResult, EngineExitCode: state.EngineExitCode, Cleanup: state.Cleanup, CleanupScope: state.CleanupScope, HistoryIntegrity: state.HistoryIntegrity, IOStatus: state.IOStatus, Reason: state.Reason, NodeCount: len(state.Nodes), NextAction: "inspect the run; task exits, engine exit, history and cleanup are independent"}
	r.TargetCount = len(state.Targets)
	if state.Reason == "not_submitted" {
		r.NextAction = "use a new request ID after capacity is available; this request did not start"
	}
	r.EngineSignal = state.EngineSignal
	r.Message = clip(state.OperationError, 256)
	r.StopObservation = state.StopObservation
	r.ResidualPath = clip(state.ResidualPath, 256)
	r.CancellationCode = state.CancellationCode
	selected := map[string]bool{}
	for _, target := range state.Targets {
		selected[target] = true
		if len(r.TargetNodes) < 4 {
			r.TargetNodes = append(r.TargetNodes, clip(target, 128))
		}
	}
	for _, n := range state.Nodes {
		if selected[n.Name] {
			r.Nodes = append(r.Nodes, n)
		}
	}
	for _, n := range state.Nodes {
		if !selected[n.Name] {
			r.Nodes = append(r.Nodes, n)
		}
	}
	return r
}
func reducedNode(n engine.Node) engine.Node {
	n.ID = clip(n.ID, 64)
	n.State = clip(n.State, 32)
	n.Source = clip(n.Source, 64)
	n.Health = clip(n.Health, 32)
	n.Incarnation = clip(n.Incarnation, 64)
	n.Name = clip(n.Name, 128)
	n.RawState = clip(n.RawState, 128)
	n.Failure = clip(n.Failure, 128)
	if n.Diagnostic != nil {
		d := *n.Diagnostic
		d.Text = clip(d.Text, 128)
		n.Diagnostic = &d
	}
	return n
}
func runtimeWrite(ctx context.Context, out *output.Sink, r runtimeResult, code int) int {
	r.Message = clip(r.Message, 256)
	r.Reason = clip(r.Reason, 128)
	for i, n := range r.Nodes {
		reduced := reducedNode(n)
		if reduced.Name != n.Name || reduced.RawState != n.RawState {
			r.NodesTruncated = true
		}
		r.Nodes[i] = reduced
	}
	for i := range r.Events {
		if r.Events[i].Node != nil {
			n := reducedNode(*r.Events[i].Node)
			r.Events[i].Node = &n
		}
		r.Events[i].Kind = clip(r.Events[i].Kind, 64)
		r.Events[i].Source = clip(r.Events[i].Source, 64)
	}
	for {
		if r.PagingNodes {
			r.NextNodeOffset = r.NodeOffset + len(r.Nodes)
		}
		raw, e := json.Marshal(r)
		if e != nil {
			return 125
		}
		if len(raw)+1 <= 4096 {
			n, e := out.WriteContext(ctx, append(raw, '\n'))
			if e != nil || n != len(raw)+1 {
				return 125
			}
			return code
		}
		r.NodesTruncated = true
		if r.Declaration != nil {
			r.Declaration = nil
		} else if len(r.TargetNodes) > 0 {
			r.TargetNodes = r.TargetNodes[:len(r.TargetNodes)-1]
		} else if len(r.Events) > 1 {
			r.Events = r.Events[:len(r.Events)-1]
			r.NextSeq = r.Events[len(r.Events)-1].Seq
		} else if len(r.Nodes) > 0 {
			r.Nodes = r.Nodes[:len(r.Nodes)-1]
		} else if len(r.Events) == 1 && r.Events[0].Node != nil {
			r.Events[0].Node = nil
		} else {
			return 125
		}
	}
}
func inspectWorkflow(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	sinks, e := output.New(stdout, stderr, 2*time.Second)
	if e != nil {
		return 125
	}
	defer sinks.Close()
	write := func(r runtimeResult, code int) int { return runtimeWrite(context.Background(), sinks.Out, r, code) }
	bad := func(code int, e error) int {
		return write(runtimeResult{Status: "invalid", Message: e.Error(), TaskResult: "unknown", Cleanup: "unknown", HistoryIntegrity: "unknown", IOStatus: "unknown", NextAction: "review the arguments or reported evidence; see runcradle help"}, code)
	}
	f := flag.NewFlagSet("workflow "+args[1], flag.ContinueOnError)
	f.SetOutput(io.Discard)
	workspace := f.String("workspace", "", "")
	requestID, runID, node := "", "", ""
	limit := 20
	afterText := ""
	nodesAfter := 0
	f.StringVar(&runID, "run-id", "", "")
	f.StringVar(&requestID, "request-id", "", "")
	f.StringVar(&node, "node", "", "")
	f.StringVar(&afterText, "events-after", "", "")
	f.IntVar(&limit, "limit", 20, "")
	f.IntVar(&nodesAfter, "nodes-after", 0, "")
	if e = f.Parse(args[2:]); e != nil {
		return bad(2, e)
	}
	if *workspace == "" || f.NArg() != 0 {
		return bad(2, fmt.Errorf("--workspace is required; no positional arguments"))
	}
	if ctx.Err() != nil {
		return bad(CancellationCode(ctx), ctx.Err())
	}
	if (runID == "") == (requestID == "") || (node != "" && afterText != "") || limit < 1 || limit > 100 || nodesAfter < 0 || (nodesAfter > 0 && (node != "" || afterText != "")) {
		return bad(2, fmt.Errorf("choose run-id or request-id; node and events-after are exclusive; limit 1..100"))
	}
	store, e := runs.Open(*workspace, false)
	if e != nil {
		return bad(2, e)
	}
	defer store.Close()
	if requestID != "" {
		intent, e := store.Request(requestID)
		if e != nil {
			return bad(2, e)
		}
		runID = intent.RunID
	}
	state, e := store.Load(runID, time.Now())
	if e != nil {
		return bad(125, e)
	}
	r := runtimeSummary(state, requestID)
	if node == "" && afterText == "" {
		if nodesAfter > len(state.Nodes) {
			return bad(2, fmt.Errorf("node offset outside snapshot"))
		}
		end := nodesAfter + limit
		if end > len(state.Nodes) {
			end = len(state.Nodes)
		}
		r.Nodes = append([]engine.Node(nil), state.Nodes[nodesAfter:end]...)
		r.PagingNodes = true
		r.NodeOffset = nodesAfter
		r.NodesTruncated = end < len(state.Nodes)
	}
	if node != "" {
		r.Nodes = nil
		for _, n := range state.Nodes {
			if n.Name == node || n.ID == node {
				r.Nodes = []engine.Node{n}
				d, e := store.Definition(state.Revision)
				if e != nil {
					return bad(125, e)
				}
				r.Declaration = d.Native["processes"].(map[string]any)[n.Name].(map[string]any)
				break
			}
		}
		if len(r.Nodes) == 0 {
			return bad(2, fmt.Errorf("node not found"))
		}
	}
	if afterText != "" {
		after, e := strconv.ParseUint(afterText, 10, 64)
		if e != nil {
			return bad(2, e)
		}
		r.Nodes = nil
		events, next, gap, e := store.Events(runID, after, limit)
		r.Events = events
		r.NextSeq = next
		if gap {
			r.HistoryIntegrity = "gap"
		}
		if e != nil {
			r.Message = e.Error()
			return write(r, 125)
		}
	}
	return write(r, 0) // success of a read, not success of the historical task
}
