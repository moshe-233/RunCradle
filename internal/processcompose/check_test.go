package processcompose

import (
	"context"
	"os"
	"path/filepath"
	"runcradle/internal/workflow"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, version, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "process compose 'fixture'")
	script := "#!/bin/sh\ncase \"$*\" in *'version --short'*) printf '%s\\n' '" + version + "'; exit 0;; esac\n" + body + "\n"
	if err := os.WriteFile(p, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return p
}
func document(t *testing.T, source string) workflow.Document {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "input.yaml"), []byte(source), 0600)
	d, e := workflow.Load(dir, "input.yaml", "")
	if e != nil {
		t.Fatal(e)
	}
	return d
}
func TestPinnedVersionAndMissingDependency(t *testing.T) {
	d := document(t, "processes: {a: {command: echo ok}}")
	for _, bin := range []string{filepath.Join(t.TempDir(), "missing"), fixture(t, "v9.0.0", "exit 0")} {
		r := Check(context.Background(), d, bin)
		if r.Code != 2 || r.Status != "unavailable" {
			t.Fatalf("accepted wrong dependency: %+v", r)
		}
	}
	r := Check(context.Background(), d, fixture(t, "v1.122.0", "exit 0"))
	if r.Code != 0 || r.Status != "passed" {
		t.Fatalf("rejected fixture: %+v", r)
	}
}
func TestUnsafeLoadingFeaturesRejectedBeforeDependency(t *testing.T) {
	for _, source := range []string{
		"extends: external.yaml\nprocesses: {a: {command: echo ok}}",
		"processes: {a: {command: 'echo $HOME'}}",
		"processes: {a: {command: 'echo {{.name}}'}}",
		"processes: {a: {command: echo ok, env_file: secret.env}}",
		"processes: {a: {command: echo ok, replicas: 999999}}",
	} {
		d := document(t, source)
		r := Check(context.Background(), d, "must-not-be-looked-up")
		if r.Code != 2 || r.Status != "unsupported" {
			t.Fatalf("unsafe check: %+v", r)
		}
	}
}
func TestCancellationAndCheckTimeout(t *testing.T) {
	d := document(t, "processes: {a: {command: echo ok}}")
	binary := fixture(t, "v1.122.0", "exec sleep 30")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if r := Check(ctx, d, binary); r.Status != "cancelled" {
		t.Fatalf("cancel lost: %+v", r)
	}
	start := time.Now()
	r := Check(context.Background(), d, binary)
	if r.Code != 124 || r.Status != "timeout" || time.Since(start) > 7*time.Second {
		t.Fatalf("unbounded timeout: %+v", r)
	}
}
func TestLargeEngineOutputRemainsBounded(t *testing.T) {
	d := document(t, "processes: {a: {command: echo ok}}")
	binary := fixture(t, "v1.122.0", "head -c 1000000 /dev/zero; exit 7")
	r := Check(context.Background(), d, binary)
	if r.Code != 2 || r.Status != "rejected" || len(r.Message) > 512 || strings.Contains(r.Message, "\x00") {
		t.Fatalf("unsafe feedback: %+v", r)
	}
}

func TestEscapedExpansionIsRejected(t *testing.T) {
	d := document(t, `{"processes":{"a":{"command":"echo \u0024HOME"}}}`)
	if r := Check(context.Background(), d, "must-not-be-looked-up"); r.Status != "unsupported" {
		t.Fatalf("escaped expansion bypass: %+v", r)
	}
}

func TestEngineRejectionIncludesBoundedReportedHint(t *testing.T) {
	d := document(t, "processes: {a: {command: echo ok}}")
	binary := fixture(t, "v1.122.0", `printf '%s\n' '{"level":"error","error":"invalid shutdown signal","message":"validation failed"}' >&2; exit 7`)
	r := Check(context.Background(), d, binary)
	if r.Code != 2 || !strings.Contains(r.Message, "reported: invalid shutdown signal") || r.EngineExit == nil || *r.EngineExit != 7 {
		t.Fatalf("lost engine observation: %+v", r)
	}
}
