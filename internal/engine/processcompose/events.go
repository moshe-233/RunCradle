package processcompose

import (
	"context"
	"encoding/json"
	"fmt"
	"runcradle/internal/engine"
	"runcradle/internal/nodeexec"
	"time"
)

func (r *run) stream(ctx context.Context) {
	for ctx.Err() == nil {
		conn, e := r.client.subscribe(ctx)
		if e == nil {
			closed := make(chan struct{})
			go func() {
				select {
				case <-ctx.Done():
					conn.Close()
				case <-closed:
				}
			}()
			for {
				var v nativeEvent
				if e = conn.ReadJSON(&v); e != nil {
					break
				}
				select {
				case r.native <- v:
				case <-ctx.Done():
					conn.Close()
					close(closed)
					return
				default:
					e = fmt.Errorf("native event consumer overflow")
				}
				if e != nil {
					break
				}
			}
			conn.Close()
			close(closed)
			select {
			case r.gap <- struct{}{}:
			default:
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// Called under r.mu; a slow product consumer cannot block the engine reader.
func (r *run) publish(kind, source string, node *engine.Node) {
	if r.eventsClosed {
		return
	}
	ev := engine.Event{RunID: r.state.RunID, Kind: kind, Source: source, ObservedAt: time.Now().UTC(), Node: node}
	select {
	case r.events <- ev:
	default:
		r.state.HistoryIntegrity = "gap"
		close(r.events)
		r.eventsClosed = true
	}
}
func (r *run) nativeState(v nativeState) {
	for i := range r.state.Nodes {
		n := &r.state.Nodes[i]
		if n.Name != v.Name {
			continue
		}
		old, _ := json.Marshal(n)
		n.RawState = v.Status
		n.NativeIncarnation = fmt.Sprintf("native-%d-%d", v.PID, v.Restarts)
		if v.Status != "Completed" && v.Status != "Error" {
			n.NativeExitCode = nil
		}
		if (v.Status == "Completed" || v.Status == "Error") && v.PID > 0 && v.End != nil {
			code := v.ExitCode
			n.NativeExitCode = &code
		}
		if n.Source != "direct_receipt" {
			n.Source = "native_snapshot"
			n.Incarnation = fmt.Sprintf("native-%d-%d", v.PID, v.Restarts)
			switch v.Status {
			case "Pending", "Launching":
				n.State = "pending"
			case "Running", "Launched", "Foreground":
				n.State = "running"
			case "Completed":
				n.State = "completed"
			case "Skipped", "Disabled":
				n.State = "skipped"
			case "Terminating":
				n.State = "stopped"
			case "Error":
				n.State = "failed"
			default:
				n.State = "unknown"
			}
			n.StartedAt = v.Start
			n.EndedAt = v.End
		}
		if v.HasProbe {
			switch v.Health {
			case "Ready":
				n.Health = "ready"
			case "Not Ready":
				n.Health = "not_ready"
			default:
				n.Health = "unknown"
			}
		}
		now, _ := json.Marshal(n)
		if string(old) != string(now) {
			copy := *n
			r.publish("node_state", "native_api", &copy)
		}
		return
	}
}
func (r *run) direct(v nodeexec.Receipt) {
	for i := range r.state.Nodes {
		n := &r.state.Nodes[i]
		if n.Name != v.NodeID {
			continue
		}
		if n.Source == "direct_receipt" && n.StartedAt != nil && n.StartedAt.After(v.StartedAt) {
			return
		}
		old, _ := json.Marshal(n)
		if n.Incarnation != v.Incarnation {
			n.Diagnostic = nil
			n.NativeExitCode = nil
		}
		n.Source = "direct_receipt"
		n.Incarnation = v.Incarnation
		n.StartedAt = &v.StartedAt
		n.EndedAt = v.EndedAt
		n.Failure = v.Failure
		n.ExitCode = v.ExitCode
		n.Signal = v.Signal
		n.State = "pending"
		if v.Started {
			n.State = "running"
		}
		if v.Final {
			n.State = "unknown"
			if v.ExitCode != nil {
				n.State = "completed"
				cfg := r.source.Document.Native["processes"].(map[string]any)[n.Name].(map[string]any)
				if v.Signal != nil || !engine.AcceptsExitCode(*v.ExitCode, cfg) {
					n.State = "failed"
				}
				if v.Signal != nil && r.state.StopRequested && v.EndedAt != nil && !v.EndedAt.Before(r.stopAt) {
					n.State = "stopped"
				}
			}
		}
		now, _ := json.Marshal(n)
		if string(old) != string(now) {
			copy := *n
			r.publish("node_result", "direct_receipt", &copy)
		}
		return
	}
}
func (r *run) targetsTerminal() bool {
	for _, target := range r.state.Targets {
		found := false
		for _, n := range r.state.Nodes {
			if n.Name == target {
				found = n.ExitCode != nil || n.RawState == "Skipped" || n.RawState == "Completed" || n.RawState == "Error" || n.Failure != ""
			}
		}
		if !found {
			return false
		}
	}
	return true
}
func (r *run) taskResult() string {
	return engine.TargetResult(r.source.Document, r.state.Targets, r.state.Nodes)
}
