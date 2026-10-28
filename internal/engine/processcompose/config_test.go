package processcompose

import (
	"context"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"runcradle/internal/engine"
	"runcradle/internal/workflow"
	"testing"
)

func document(t *testing.T, source string) workflow.Document {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "native.yaml"), []byte(source), 0600)
	d, e := workflow.Load(dir, "native.yaml", "")
	if e != nil || !d.Valid() {
		t.Fatalf("fixture: %v %+v", e, d.Issues)
	}
	return d
}

func TestGeneratedPathsCannotIntroduceNativeExpansion(t *testing.T) {
	d := document(t, "processes: {a: {entrypoint: [/bin/true]}}")
	for _, helper := range []string{"/tmp/helper$HOME", "/tmp/helper{{.x}}"} {
		if _, e := Prepare(d, helper, "run", t.TempDir()); e == nil {
			t.Fatalf("generated helper path expanded: %s", helper)
		}
	}
}

func TestSourceRevisionMismatchRejectedBeforeEngineProbe(t *testing.T) {
	d := document(t, "processes: {a: {entrypoint: [/bin/true]}}")
	marker := filepath.Join(d.Workspace, "called")
	binary := filepath.Join(d.Workspace, "engine")
	os.WriteFile(binary, []byte("#!/bin/sh\ntouch '"+marker+"'\necho v1.122.0\n"), 0700)
	validation, e := New(binary, "/bin/true").Validate(context.Background(), engine.WorkflowSource{Document: d, Revision: "wrong-revision"})
	if e != nil {
		t.Fatal(e)
	}
	if validation.Valid {
		t.Fatal("source revision ignored")
	}
	if _, e = os.Stat(marker); !os.IsNotExist(e) {
		t.Fatal("engine invoked before source identity check")
	}
}
func TestPreparePreservesNativeSchedulingAndOriginal(t *testing.T) {
	d := document(t, "processes:\n  prepare:\n    command: printf 'a b'\n  check:\n    entrypoint: [echo, 'literal spaces']\n    depends_on: {prepare: {condition: process_completed_successfully}}\n    availability: {exit_on_end: true}\n    success_exit_codes: [7]\n")
	before := string(d.Source)
	raw, e := Prepare(d, "/usr/bin/runcradle", "run", t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	var native map[string]any
	if yaml.Unmarshal(raw, &native) != nil {
		t.Fatal("not native yaml")
	}
	ps := native["processes"].(map[string]any)
	check := ps["check"].(map[string]any)
	if check["depends_on"] == nil || check["availability"] == nil || check["success_exit_codes"] == nil {
		t.Fatal("native scheduling lost")
	}
	args := check["entrypoint"].([]any)
	if args[0] != "/usr/bin/runcradle" || args[len(args)-1] != "literal spaces" {
		t.Fatalf("argv changed: %v", args)
	}
	if check["working_dir"] != d.Workspace || string(d.Source) != before {
		t.Fatal("cwd/source changed")
	}
	if d.Native["processes"].(map[string]any)["check"].(map[string]any)["working_dir"] != nil {
		t.Fatal("document mutated")
	}
}
func TestPrepareRejectsUnverifiedRuntimeBeforeAnyExecution(t *testing.T) {
	for _, field := range []string{"is_daemon: true", "is_interactive: true", "is_tty: true", "is_foreground: true", "replicas: 2", "availability: {restart: always}", "shutdown: {parent_only: true}", "shutdown: {command: echo cleanup}", "watch: {paths: [.]}"} {
		t.Run(field, func(t *testing.T) {
			d := document(t, "processes:\n  a:\n    command: echo ok\n    "+field+"\n")
			if _, e := Prepare(d, "/bin/true", "run", t.TempDir()); e == nil {
				t.Fatal("unverified feature accepted")
			}
		})
	}
}

func TestPrepareRejectsAmbiguousRelativeSearchPath(t *testing.T) {
	t.Setenv("PATH", "./tools:/usr/bin:/bin")
	d := document(t, "processes: {a: {entrypoint: [/bin/true]}}")
	if _, e := Prepare(d, "/bin/true", "run", t.TempDir()); e == nil {
		t.Fatal("relative inherited PATH could resolve differently in private engine cwd")
	}
}
