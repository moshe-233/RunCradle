package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOfflineValidateJSONAndRealErrors(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ok.yaml"), []byte("processes: {a: {command: echo ok}}"), 0600)
	for _, tc := range []struct {
		args []string
		want int
	}{
		{[]string{"workflow", "validate", "--workspace", dir, "--file", "ok.yaml"}, 0},
		{[]string{"workflow", "validate", "--workspace", dir, "--file", "missing.yaml"}, 2},
		{[]string{"workflow", "validate", "--workspace", dir, "--file", "ok.yaml", "extra"}, 2},
		{[]string{"workflow", "run"}, 2},
	} {
		var out, errout bytes.Buffer
		code := Run(context.Background(), tc.args, &out, &errout)
		if code != tc.want || errout.Len() != 0 || !json.Valid(out.Bytes()) || out.Len() > 4096 {
			t.Fatalf("exit=%d output=%s err=%s", code, out.String(), errout.String())
		}
	}
}
func TestBoundedJSONEscapedLargeIssue(t *testing.T) {
	r := Result{Status: "invalid", Issues: []Issue{{Severity: "error", Code: "configuration", Message: strings.Repeat("\"\\\x00中文🐈", 10000)}}}
	var out bytes.Buffer
	if writeResult(&out, r, 2) != 2 || out.Len() > 4096 || !json.Valid(out.Bytes()) {
		t.Fatalf("bad summary len=%d", out.Len())
	}
	if !strings.Contains(out.String(), `"issues_truncated":true`) {
		t.Fatal("missing truncation flag")
	}
}
func TestHelpVersionAndOutputFailure(t *testing.T) {
	for _, args := range [][]string{nil, {"help"}, {"version"}} {
		var out bytes.Buffer
		if Run(context.Background(), args, &out, &bytes.Buffer{}) != 0 || out.Len() == 0 {
			t.Fatal("lost bootstrap entry")
		}
	}
	f, e := os.OpenFile("/dev/full", os.O_WRONLY, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if code := Run(context.Background(), []string{"version"}, f, &bytes.Buffer{}); code != 125 {
		t.Fatalf("write error exit=%d", code)
	}
}

func TestRenderWritesOnlyExplicitOutput(t *testing.T) {
	dir := t.TempDir()
	source := []byte("processes: {a: {command: echo ok}}")
	os.WriteFile(filepath.Join(dir, "source.yaml"), source, 0600)
	base := []string{"workflow", "render", "--workspace", dir, "--file", "source.yaml"}
	for _, tc := range []struct {
		tail []string
		want int
	}{
		{[]string{"--out", "preview.html"}, 0},
		{[]string{"--out", "preview.html"}, 2},
		{[]string{"--out", "preview.html", "--force"}, 0},
		{[]string{"--out", "source.yaml", "--force"}, 2},
	} {
		var out bytes.Buffer
		code := Run(context.Background(), append(append([]string{}, base...), tc.tail...), &out, &bytes.Buffer{})
		if code != tc.want {
			t.Fatalf("exit=%d output=%s", code, out.String())
		}
	}
	content, _ := os.ReadFile(filepath.Join(dir, "preview.html"))
	if !bytes.Contains(content, []byte("RunCradle")) {
		t.Fatal("no generated page")
	}
	got, _ := os.ReadFile(filepath.Join(dir, "source.yaml"))
	if !bytes.Equal(source, got) {
		t.Fatal("source changed")
	}
}

func TestExplicitEngineCheckIsOptIn(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ok.yaml"), []byte("processes: {a: {command: echo ok}}"), 0600)
	args := []string{"workflow", "validate", "--workspace", dir, "--file", "ok.yaml"}
	var out bytes.Buffer
	code := Run(context.Background(), append(args, "--check-engine", "--process-compose", filepath.Join(dir, "missing")), &out, &bytes.Buffer{})
	if code != 2 || !strings.Contains(out.String(), `"engine_check":"unavailable"`) {
		t.Fatalf("dependency result lost: exit=%d %s", code, out.String())
	}
	t.Setenv("PATH", "")
	out.Reset()
	if code := Run(context.Background(), args, &out, &bytes.Buffer{}); code != 0 {
		t.Fatalf("offline requires engine: %d %s", code, out.String())
	}
}
func TestCancellationRetainsSignalExit(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(SignalCause(15))
	var out bytes.Buffer
	if code := Run(ctx, []string{"workflow", "validate", "--workspace", t.TempDir(), "--file", "x.yaml"}, &out, &bytes.Buffer{}); code != 143 {
		t.Fatalf("cancel exit=%d", code)
	}
}
