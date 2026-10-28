package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	eng "runcradle/internal/engine"
	pc "runcradle/internal/engine/processcompose"
	"runcradle/internal/workflow"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestW2NativeManagerKilledLeavesUnknownTaskAndCleanup(t *testing.T) {
	d := engineDocument(t, map[string]any{"processes": map[string]any{"a": map[string]any{"command": "echo $$ > node.pid; exec sleep 30"}}})
	// Dollar expansion is outside the accepted native subset; put the shell
	// program in an ordinary business script instead of the definition.
	os.WriteFile(filepath.Join(d.Workspace, "job.sh"), []byte("#!/bin/sh\necho $$ > node.pid\nexec sleep 30\n"), 0700)
	os.WriteFile(filepath.Join(d.Workspace, "pc.json"), []byte(`{"processes":{"a":{"entrypoint":["/bin/sh","job.sh"]}}}`), 0600)
	d, _ = workflow.Load(d.Workspace, "pc.json", "")
	managerPID := filepath.Join(d.Workspace, "manager.pid")
	wrapper := filepath.Join(d.Workspace, "engine")
	os.WriteFile(wrapper, []byte(fmt.Sprintf("#!/bin/sh\necho $$ > '%s'\nexec '%s' \"$@\"\n", managerPID, engine(t))), 0700)
	a := pc.New(wrapper, executable)
	h, e := a.Start(context.Background(), eng.StartRequest{Source: eng.WorkflowSource{Document: d}, Targets: []string{"a"}, Deadline: time.Now().Add(10 * time.Second)})
	if e != nil {
		t.Fatal(e)
	}
	defer a.Stop(context.Background(), h)
	awaitEngine(t, a, h, func(s eng.Snapshot) bool {
		_, err := os.Stat(filepath.Join(d.Workspace, "node.pid"))
		return s.Nodes[0].StartedAt != nil && err == nil
	})
	raw, e := os.ReadFile(filepath.Join(d.Workspace, "node.pid"))
	if e != nil {
		t.Fatal(e)
	}
	nodePID, e := strconv.Atoi(strings.TrimSpace(string(raw)))
	if e != nil {
		t.Fatal(e)
	}
	group, e := syscall.Getpgid(nodePID)
	if e != nil {
		t.Fatal(e)
	}
	// This test created and observed the group; product cleanup does not get
	// this extra authority. Reap the fixture after inspecting unknown facts.
	defer syscall.Kill(-group, syscall.SIGTERM)
	raw, e = os.ReadFile(managerPID)
	if e != nil {
		t.Fatal(e)
	}
	pid, e := strconv.Atoi(strings.TrimSpace(string(raw)))
	if e != nil {
		t.Fatal(e)
	}
	if e = syscall.Kill(pid, syscall.SIGKILL); e != nil {
		t.Fatal(e)
	}
	s := awaitEngine(t, a, h, func(s eng.Snapshot) bool { return s.Final })
	defer os.RemoveAll(s.ResidualPath)
	encoded, _ := json.Marshal(s)
	if s.TaskResult != "unknown" || s.Nodes[0].ExitCode != nil || s.Cleanup != "unknown" || s.ResidualPath == "" || s.EngineExitCode != nil || !strings.Contains(string(encoded), `"engine_signal":9`) {
		t.Fatalf("lost manager facts or invented completion: %s", encoded)
	}
}

func TestW2EngineFastNodeExitAndEarlyPolicy(t *testing.T) {
	for _, code := range []int{0, 7, 8} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "pc.yaml"), []byte(fmt.Sprintf("processes:\n  prepare:\n    entrypoint: [/bin/true]\n  check:\n    entrypoint: [/bin/sh, '-c', 'exit %d']\n    depends_on: {prepare: {condition: process_completed_successfully}}\n    availability: {exit_on_end: true}\n", code)), 0600)
			d, e := workflow.Load(dir, "pc.yaml", "")
			if e != nil {
				t.Fatal(e)
			}
			a := pc.New(engine(t), executable)
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			h, e := a.Start(ctx, eng.StartRequest{Source: eng.WorkflowSource{Document: d}, Targets: []string{"check"}, Deadline: time.Now().Add(5 * time.Second)})
			if e != nil {
				t.Fatal(e)
			}
			defer a.Stop(context.Background(), h)
			for {
				s, e := a.Snapshot(ctx, h)
				if e != nil {
					t.Fatal(e)
				}
				if s.Final {
					var seen bool
					for _, n := range s.Nodes {
						if n.Name == "check" {
							seen = n.ExitCode != nil && *n.ExitCode == code
						}
					}
					if !seen || s.EngineExitCode == nil {
						t.Fatalf("real exits missing: %+v", s)
					}
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("unbounded run")
				case <-time.After(10 * time.Millisecond):
				}
			}
		})
	}
}

func engineDocument(t *testing.T, source any) workflow.Document {
	t.Helper()
	dir := t.TempDir()
	raw, e := json.Marshal(source)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, "pc.json"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	d, e := workflow.Load(dir, "pc.json", "")
	if e != nil || !d.Valid() {
		t.Fatalf("fixture %v %+v", e, d.Issues)
	}
	return d
}
func awaitEngine(t *testing.T, a *pc.Adapter, h eng.RunHandle, predicate func(eng.Snapshot) bool) eng.Snapshot {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	for {
		s, e := a.Snapshot(ctx, h)
		if e != nil {
			t.Fatal(e)
		}
		if predicate(s) {
			return s
		}
		select {
		case <-ctx.Done():
			t.Fatalf("state never arrived: %+v", s)
		case <-time.After(10 * time.Millisecond):
		}
	}
}
func TestW2NativeDependencyConditions(t *testing.T) {
	for _, condition := range []string{"process_started", "process_completed", "process_completed_successfully", "process_healthy", "process_log_ready"} {
		t.Run(condition, func(t *testing.T) {
			source := map[string]any{"entrypoint": []string{"/bin/true"}}
			switch condition {
			case "process_started":
				source["entrypoint"] = []string{"/bin/sleep", "30"}
			case "process_completed":
				source["entrypoint"] = []string{"/bin/sh", "-c", "exit 7"}
			case "process_healthy":
				source["entrypoint"] = []string{"/bin/sh", "-c", "touch ready; exec sleep 30"}
				source["readiness_probe"] = map[string]any{"exec": map[string]any{"command": "test -f ready"}, "period_seconds": 1}
			case "process_log_ready":
				source["entrypoint"] = []string{"/bin/sh", "-c", "printf 'READY\\n'; exec sleep 30"}
				source["ready_log_line"] = "READY"
			}
			d := engineDocument(t, map[string]any{"processes": map[string]any{"source": source, "target": map[string]any{"entrypoint": []string{"/bin/true"}, "depends_on": map[string]any{"source": map[string]any{"condition": condition}}}}})
			a := pc.New(engine(t), executable)
			h, e := a.Start(context.Background(), eng.StartRequest{Source: eng.WorkflowSource{Document: d}, Targets: []string{"target"}, Deadline: time.Now().Add(6 * time.Second)})
			if e != nil {
				t.Fatal(e)
			}
			defer a.Stop(context.Background(), h)
			events, e := a.Subscribe(context.Background(), h)
			if e != nil {
				t.Fatal(e)
			}
			s := awaitEngine(t, a, h, func(s eng.Snapshot) bool { return s.Final })
			if s.TaskResult != "succeeded" {
				t.Fatalf("dependency failed: %+v", s)
			}
			if condition == "process_healthy" {
				found := false
				for ev := range events {
					if ev.Node != nil && ev.Node.Name == "source" && ev.Node.Health == "ready" && ev.Node.RawState != "" {
						found = true
					}
				}
				if !found {
					t.Fatalf("native API health not observed: %+v", s)
				}
			}
		})
	}
}

func TestW2NativeFailureAndSkipPolicies(t *testing.T) {
	for _, policy := range []string{"exit_on_failure", "exit_on_skipped", "success_exit_codes"} {
		t.Run(policy, func(t *testing.T) {
			p := map[string]any{"entrypoint": []string{"/bin/sh", "-c", "exit 7"}}
			target := map[string]any{"entrypoint": []string{"/bin/true"}, "depends_on": map[string]any{"source": map[string]any{"condition": "process_completed_successfully"}}}
			if policy == "exit_on_failure" {
				p["availability"] = map[string]any{"restart": "exit_on_failure"}
			}
			if policy == "exit_on_skipped" {
				target["availability"] = map[string]any{"exit_on_skipped": true}
			}
			if policy == "success_exit_codes" {
				p["success_exit_codes"] = []int{7}
			}
			d := engineDocument(t, map[string]any{"processes": map[string]any{"source": p, "target": target}})
			a := pc.New(engine(t), executable)
			h, e := a.Start(context.Background(), eng.StartRequest{Source: eng.WorkflowSource{Document: d}, Targets: []string{"target"}, Deadline: time.Now().Add(4 * time.Second)})
			if e != nil {
				t.Fatal(e)
			}
			defer a.Stop(context.Background(), h)
			s := awaitEngine(t, a, h, func(s eng.Snapshot) bool { return s.Final })
			if policy == "success_exit_codes" && s.TaskResult != "succeeded" {
				t.Fatalf("allowed code lost: %+v", s)
			}
			if policy != "success_exit_codes" && s.TaskResult == "succeeded" {
				t.Fatalf("unexecuted target reported successful: %+v", s)
			}
			for _, n := range s.Nodes {
				if n.Name == "source" && (n.ExitCode == nil || *n.ExitCode != 7) {
					t.Fatalf("source exit lost: %+v", n)
				}
			}
		})
	}
}

func TestW2StopAndConcurrentInstances(t *testing.T) {
	d := engineDocument(t, map[string]any{"processes": map[string]any{"a": map[string]any{"entrypoint": []string{"/bin/sleep", "30"}, "shutdown": map[string]any{"timeout_seconds": 1}}}})
	a := pc.New(engine(t), executable)
	var handles []eng.RunHandle
	for i := 0; i < 2; i++ {
		h, e := a.Start(context.Background(), eng.StartRequest{Source: eng.WorkflowSource{Document: d}, Targets: []string{"a"}, Deadline: time.Now().Add(6 * time.Second)})
		if e != nil {
			t.Fatal(e)
		}
		handles = append(handles, h)
		defer a.Stop(context.Background(), h)
	}
	if handles[0].RunID == handles[1].RunID || handles[0].EngineInstanceID == handles[1].EngineInstanceID {
		t.Fatal("instances collided")
	}
	for _, h := range handles {
		awaitEngine(t, a, h, func(s eng.Snapshot) bool { return len(s.Nodes) == 1 && s.Nodes[0].State == "running" })
		if result, e := a.Stop(context.Background(), h); e != nil || !result.Terminal {
			t.Fatalf("stop: %+v %v", result, e)
		}
	}
}

func TestW2RealRestartKeepsIncarnationsDistinct(t *testing.T) {
	d := engineDocument(t, map[string]any{"processes": map[string]any{"a/name": map[string]any{"entrypoint": []string{"/bin/sleep", "30"}, "shutdown": map[string]any{"timeout_seconds": 1}}}})
	a := pc.New(engine(t), executable)
	h, e := a.Start(context.Background(), eng.StartRequest{Source: eng.WorkflowSource{Document: d}, Targets: []string{"a/name"}, Deadline: time.Now().Add(7 * time.Second)})
	if e != nil {
		t.Fatal(e)
	}
	defer a.Stop(context.Background(), h)
	first := awaitEngine(t, a, h, func(s eng.Snapshot) bool {
		return s.Nodes[0].Source == "direct_receipt" && s.Nodes[0].State == "running"
	})
	socket := filepath.Join(os.TempDir(), h.EngineInstanceID, "pc.sock")
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}}
	req, _ := http.NewRequest("POST", "http://runcradle/process/restart/a%2Fname", nil)
	resp, e := client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("native restart %d", resp.StatusCode)
	}
	second := awaitEngine(t, a, h, func(s eng.Snapshot) bool {
		return s.Nodes[0].Source == "direct_receipt" && s.Nodes[0].State == "running" && s.Nodes[0].Incarnation != first.Nodes[0].Incarnation
	})
	if second.Nodes[0].ExitCode != nil {
		t.Fatal("new incarnation inherited old exit")
	}
}

func TestW2NativeNodeNameCannotBecomeAnEngineFlag(t *testing.T) {
	name := "--config=not-a-config.yaml"
	d := engineDocument(t, map[string]any{"processes": map[string]any{name: map[string]any{"entrypoint": []string{"/bin/true"}, "availability": map[string]any{"exit_on_end": true}}}})
	a := pc.New(engine(t), executable)
	h, e := a.Start(context.Background(), eng.StartRequest{Source: eng.WorkflowSource{Document: d}, Targets: []string{name}, Deadline: time.Now().Add(3 * time.Second)})
	if e != nil {
		t.Fatal(e)
	}
	defer a.Stop(context.Background(), h)
	s := awaitEngine(t, a, h, func(s eng.Snapshot) bool { return s.Final })
	if s.TaskResult != "succeeded" {
		t.Fatalf("node was parsed as a flag: %+v", s)
	}
}
