package processcompose

import (
	"context"
	"os"
	"runcradle/internal/nodeexec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func (r *run) collect() {
	records, e := nodeexec.ReadReceipts(r.receipts, r.state.RunID)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	states, nativeErr := r.client.states(ctx)
	cancel()
	r.mu.Lock()
	if nativeErr == nil {
		for _, v := range states {
			r.nativeState(v)
		}
	}
	if e != nil {
		r.receiptError = true
		r.state.HistoryIntegrity = "gap"
		r.state.IOStatus = "incomplete"
		r.publish("receipt_error", "recorder", nil)
	} else {
		r.records = records
		for _, v := range records {
			r.direct(v)
		}
	}
	var pending []string
	for _, n := range r.state.Nodes {
		if n.State == "failed" && n.Diagnostic == nil {
			pending = append(pending, n.Name)
		}
	}
	r.mu.Unlock()
	logCtx, cancelLogs := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancelLogs()
	for _, name := range pending {
		d := r.client.diagnostic(logCtx, name)
		r.mu.Lock()
		for i := range r.state.Nodes {
			n := &r.state.Nodes[i]
			if n.Name == name && n.State == "failed" && n.Diagnostic == nil {
				n.Diagnostic = d
				copy := *n
				r.publish("node_diagnostic", "native_logs", &copy)
			}
		}
		r.mu.Unlock()
	}
}
func (r *run) shutdown(reason string) {
	r.mu.Lock()
	if r.state.StopRequested {
		r.mu.Unlock()
		return
	}
	r.state.StopRequested = true
	r.state.Reason = reason
	r.stopAt = time.Now()
	r.publish("stop_requested", "service", nil)
	r.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	e := r.client.stop(ctx)
	cancel()
	if e != nil {
		_ = r.cmd.Process.Signal(syscall.SIGTERM)
	} // directly owned manager only
}
func (r *run) monitor() {
	defer close(r.done)
	defer r.streamCancel()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-r.wait:
			r.collect()
			r.finish()
			return
		case reason := <-r.stop:
			r.shutdown(reason)
		case v := <-r.native:
			r.mu.Lock()
			r.nativeState(v.State)
			r.mu.Unlock()
		case <-r.gap:
			r.mu.Lock()
			r.state.HistoryIntegrity = "gap"
			r.publish("state_stream_gap", "native_ws", nil)
			r.mu.Unlock()
		case <-tick.C:
			r.collect()
			r.mu.Lock()
			terminal := r.targetsTerminal()
			stopping := r.state.StopRequested
			stopAt := r.stopAt
			r.mu.Unlock()
			if !stopping && !time.Now().Before(r.deadline) {
				r.shutdown("deadline")
			} else if !stopping && terminal {
				r.shutdown("targets_terminal")
			}
			if stopping && time.Since(stopAt) >= 8*time.Second {
				_ = r.cmd.Process.Kill()
			}
		}
	}
}
func (r *run) finish() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state.Final = true
	now := time.Now().UTC()
	r.state.EndedAt = &now
	if r.state.StopRequested {
		r.state.StopObservation = "manager_terminal"
	}
	r.state.TaskResult = r.taskResult()
	if r.state.Reason == "running" {
		r.state.Reason = "engine_exited"
	}
	complete := len(r.records) > 0 && r.state.EngineExitCode != nil && !r.receiptError && groupsEmpty(r.records)
	if r.receiptError {
		r.state.IOStatus = "incomplete"
	}
	for _, v := range r.records {
		if v.Failure != "" {
			r.state.IOStatus = "incomplete"
			r.state.HistoryIntegrity = "gap"
			complete = false
		}
		if !v.Final {
			complete = false
		}
	}
	if complete {
		r.state.Cleanup = "complete"
		if e := os.RemoveAll(r.dir); e != nil {
			r.state.Cleanup = "unknown"
			r.state.ResidualPath = r.dir
		} else {
			r.state.ResidualPath = ""
		}
	} else {
		r.state.Cleanup = "unknown"
		r.state.ResidualPath = r.dir
	}
	r.publish("run_final", "recorder", nil)
	if !r.eventsClosed {
		close(r.events)
		r.eventsClosed = true
	}
}

// Observation only: never signal a group obtained from a record or /proc.
// A positive result is explicitly scoped to registered native process groups.
func groupsEmpty(records []nodeexec.Receipt) bool {
	if len(records) == 0 {
		return false
	}
	r := records[0]
	if r.GroupID != r.HelperPID || r.GroupID < 1 || r.PIDNamespace == 0 || r.BootID == "" {
		return false
	}
	boot, e := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if e != nil || string(boot) != r.BootID {
		return false
	}
	ns, e := os.Stat("/proc/self/ns/pid")
	if e != nil {
		return false
	}
	st, ok := ns.Sys().(*syscall.Stat_t)
	if !ok || st.Ino != r.PIDNamespace {
		return false
	}
	groups := map[int]bool{}
	for _, record := range records {
		if record.GroupID != record.HelperPID || record.GroupID < 1 || record.BootID != r.BootID || record.PIDNamespace != r.PIDNamespace {
			return false
		}
		groups[record.GroupID] = true
	}
	entries, e := os.ReadDir("/proc")
	if e != nil {
		return false
	}
	deadline := time.Now().Add(200 * time.Millisecond)
	for _, entry := range entries {
		if time.Now().After(deadline) {
			return false
		}
		if _, e := strconv.Atoi(entry.Name()); e != nil {
			continue
		}
		raw, e := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return false
		}
		i := strings.LastIndexByte(string(raw), ')')
		if i < 0 {
			return false
		}
		f := strings.Fields(string(raw[i+1:]))
		if len(f) < 3 {
			return false
		}
		group, e := strconv.Atoi(f[2])
		if e != nil {
			return false
		}
		if groups[group] && f[0] != "Z" && f[0] != "X" && f[0] != "x" {
			return false
		}
	}
	return true
}
