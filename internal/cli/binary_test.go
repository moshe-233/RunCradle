package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runcradle/internal/buildinfo"
	"strings"
	"testing"
)

func bundledIdentity(t *testing.T) {
	t.Helper()
	old := buildinfo.Distribution
	buildinfo.Distribution = "bundle"
	t.Cleanup(func() { buildinfo.Distribution = old })
}
func TestServeEngineFailureBeforeOpen(t *testing.T) {
	bundledIdentity(t)
	dir := t.TempDir()
	var out bytes.Buffer
	code := Run(context.Background(), []string{"serve", "--workspace", dir}, &out, &bytes.Buffer{})
	if code != 2 || strings.Contains(out.String(), `"status":"serving"`) {
		t.Fatalf("%d %s", code, &out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".runcradle")); !os.IsNotExist(err) {
		t.Fatal("installation failure modified workspace")
	}
}
func TestExplicitEmptyEngineRejected(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	code := Run(context.Background(), []string{"serve", "--workspace", dir, "--process-compose", ""}, &out, &bytes.Buffer{})
	if code != 2 || !strings.Contains(out.String(), "must not be empty") {
		t.Fatalf("%d %s", code, &out)
	}
}
func TestOfflineCommandsDoNotResolveEngine(t *testing.T) {
	bundledIdentity(t)
	t.Setenv("PATH", "")
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ok.yaml"), []byte("processes: {a: {command: 'exit 0'}}"), 0600)
	for _, args := range [][]string{{"help"}, {"version"}, {"workflow", "validate", "--workspace", dir, "--file", "ok.yaml"}, {"workflow", "render", "--workspace", dir, "--file", "ok.yaml", "--out", "ok.html"}} {
		var out bytes.Buffer
		if code := Run(context.Background(), args, &out, &bytes.Buffer{}); code != 0 {
			t.Fatalf("offline %v: %d %s", args, code, &out)
		}
	}
	old := buildinfo.Version
	buildinfo.Version = "1.2.3"
	defer func() { buildinfo.Version = old }()
	var out bytes.Buffer
	if code := Run(context.Background(), []string{"version"}, &out, &bytes.Buffer{}); code != 0 || out.String() != "runcradle 1.2.3\n" {
		t.Fatalf("version: %d %s", code, &out)
	}
}
func TestValidateEngineUsesSharedResolver(t *testing.T) {
	bundledIdentity(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ok.yaml"), []byte("processes: {a: {command: 'exit 0'}}"), 0600)
	for _, explicit := range [][]string{nil, {"--process-compose", ""}, {"--process-compose", "missing"}} {
		var out bytes.Buffer
		args := append([]string{"workflow", "validate", "--workspace", dir, "--file", "ok.yaml", "--check-engine"}, explicit...)
		if code := Run(context.Background(), args, &out, &bytes.Buffer{}); code != 2 || !strings.Contains(out.String(), `"engine_check":"unavailable"`) {
			t.Fatalf("%d %s", code, &out)
		}
	}
}
func TestServeCancelledBeforeOpen(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(SignalCause(15))
	dir := t.TempDir()
	var out bytes.Buffer
	if code := Run(ctx, []string{"serve", "--workspace", dir}, &out, &bytes.Buffer{}); code != 143 {
		t.Fatalf("%d %s", code, &out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".runcradle")); !os.IsNotExist(err) {
		t.Fatal("cancelled serve modified workspace")
	}
}

func TestEngineInternalIOFailureExit(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "pc")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf 'v1.122.0\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ok.yaml"), []byte("processes: {a: {command: 'exit 0'}}"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", filepath.Join(dir, "missing"))
	for _, args := range [][]string{{"serve", "--workspace", dir, "--process-compose", bin}, {"workflow", "validate", "--workspace", dir, "--file", "ok.yaml", "--check-engine", "--process-compose", bin}} {
		var out bytes.Buffer
		if code := Run(context.Background(), args, &out, &bytes.Buffer{}); code != 125 {
			t.Fatalf("internal IO returned %d: %s", code, &out)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".runcradle")); !os.IsNotExist(err) {
		t.Fatal("IO failure opened workspace")
	}
}
