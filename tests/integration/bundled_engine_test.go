package integration

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	eng "runcradle/internal/engine"
	"runcradle/internal/hostclient"
	"runcradle/internal/httpapi"
	"runcradle/internal/processcompose"
)

func extractedBundle(t *testing.T) string {
	t.Helper()
	if os.Getenv("RUNCRADLE_BUNDLE_TEST") != "1" {
		t.Skip("opt in with make package-test")
	}
	archive := os.Getenv("RUNCRADLE_BUNDLE_ARCHIVE")
	if archive == "" {
		t.Fatal("RUNCRADLE_BUNDLE_ARCHIVE is required when bundle tests are enabled")
	}
	raw, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	checksum, err := os.ReadFile(archive + ".sha256")
	sum := sha256.Sum256(raw)
	if err != nil || strings.Fields(string(checksum))[0] != hex.EncodeToString(sum[:]) {
		t.Fatal("archive checksum mismatch/missing")
	}
	gzipReader, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	directory := filepath.Join(t.TempDir(), "中文 安装目录")
	os.Mkdir(directory, 0755)
	root := ""
	files := map[string]string{}
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		parts := strings.Split(header.Name, "/")
		if root == "" {
			root = parts[0]
		}
		if parts[0] != root || strings.Contains(header.Name, "../") || filepath.IsAbs(header.Name) {
			t.Fatal("archive escapes its single root")
		}
		path := filepath.Join(directory, filepath.FromSlash(header.Name))
		if header.Typeflag == tar.TypeDir {
			if header.Mode != 0755 {
				t.Fatal("directory permission")
			}
			if err = os.MkdirAll(path, 0755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if header.Typeflag != tar.TypeReg {
			t.Fatal("archive contains link or special entry")
		}
		rel := strings.TrimPrefix(header.Name, root+"/")
		mode := int64(0644)
		if rel == "runcradle" || rel == "libexec/process-compose" {
			mode = 0755
		}
		if header.Mode != mode {
			t.Fatalf("permissions %s: %o", rel, header.Mode)
		}
		if _, exists := files[rel]; exists {
			t.Fatal("duplicate archive file")
		}
		if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(tarReader)
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(content)
		files[rel] = hex.EncodeToString(hash[:])
		if err = os.WriteFile(path, content, os.FileMode(mode)); err != nil {
			t.Fatal(err)
		}
	}
	packageRoot := filepath.Join(directory, root)
	manifestRaw, err := os.ReadFile(filepath.Join(packageRoot, "bundle.json"))
	if err != nil || len(manifestRaw) > 64<<10 {
		t.Fatal("manifest missing/oversized")
	}
	var manifest processcompose.BundleManifest
	if err = json.Unmarshal(manifestRaw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != 1 || manifest.Engine.SHA256 != "7127df85c0d0c62f2f79b4d2b3082ca1d85f7c0c74523457617861d32150b0dd" || manifest.OS != "linux" || manifest.Arch != "amd64" {
		t.Fatal("wrong manifest identity")
	}
	delete(files, "bundle.json")
	for _, entry := range manifest.Files {
		if got, ok := files[entry.Path]; !ok || got != entry.SHA256 {
			t.Fatalf("manifest mismatch: %s", entry.Path)
		}
		delete(files, entry.Path)
	}
	if len(files) != 0 {
		t.Fatalf("unlisted package files: %v", files)
	}
	for _, required := range []string{"LICENSE", "THIRD_PARTY_NOTICES.md", "licenses/process-compose/LICENSE", "licenses/process-compose/inventory.json", "README.md", "examples/hello.yaml"} {
		if _, err = os.Stat(filepath.Join(packageRoot, required)); err != nil {
			t.Fatal(err)
		}
	}
	notices, err := os.ReadFile(filepath.Join(packageRoot, "THIRD_PARTY_NOTICES.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, match := range regexp.MustCompile(`\]\(([^)]+)\)`).FindAllSubmatch(notices, -1) {
		link := string(match[1])
		if strings.Contains(link, "://") {
			continue
		}
		if _, err := os.Stat(filepath.Join(packageRoot, link)); err != nil {
			t.Fatalf("broken shipped attribution link %q: %v", link, err)
		}
	}
	return packageRoot
}
func bundleEnv(t *testing.T) ([]string, string) {
	t.Helper()
	dir := t.TempDir()
	marker := filepath.Join(dir, "unexpected-system-engine")
	if err := os.WriteFile(filepath.Join(dir, "process-compose"), []byte("#!/bin/sh\nprintf bad > '"+marker+"'\nprintf 'v1.122.0\\n'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	env := []string{}
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "PATH=") {
			env = append(env, entry)
		}
	}
	return append(env, "PATH="+dir), marker
}
func bundleCall(t *testing.T, binary, dir string, env []string, want int, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if cmd.ProcessState == nil {
			t.Fatal(err)
		}
		code = cmd.ProcessState.ExitCode()
	}
	if ctx.Err() != nil || code != want {
		t.Fatalf("bundle command %v: code=%d want=%d err=%v output=%s", args, code, want, err, out)
	}
	return out
}
func bundleHost(t *testing.T, binary, workspace string, env []string) func() {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	cmd := exec.CommandContext(ctx, binary, "serve", "--workspace", workspace)
	cmd.Dir = t.TempDir()
	cmd.Env = env
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		cmd.Process.Signal(syscall.SIGTERM)
		err := cmd.Wait()
		cancel()
		if cmd.ProcessState.ExitCode() != 143 {
			t.Errorf("bundle host shutdown: %v", err)
		}
	}
	t.Cleanup(stop)
	var ready httpapi.Ready
	if err = json.NewDecoder(pipe).Decode(&ready); err != nil {
		stop()
		t.Fatalf("bundle host did not become ready: %v", err)
	}
	go io.Copy(io.Discard, pipe)
	return stop
}
func bundleWorkspace(t *testing.T, root string) string {
	t.Helper()
	dir := t.TempDir()
	raw, err := os.ReadFile(filepath.Join(root, "examples", "hello.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "hello.yaml"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}
func bundleRun(t *testing.T, binary, workspace string, env []string, target, id string, wantExit int) string {
	t.Helper()
	wantCode := 0
	wantResult := "succeeded"
	if wantExit != 0 {
		wantCode = 1
		wantResult = "failed"
	}
	raw := bundleCall(t, binary, workspace, env, wantCode, "workflow", "run", "--workspace", workspace, "--file", "hello.yaml", "--target", target, "--request-id", id)
	var result struct {
		RunID      string     `json:"run_id"`
		TaskResult string     `json:"task_result"`
		Cleanup    string     `json:"cleanup"`
		Nodes      []eng.Node `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	var targetNode *eng.Node
	for i := range result.Nodes {
		if result.Nodes[i].Name == target {
			targetNode = &result.Nodes[i]
			break
		}
	}
	if result.TaskResult != wantResult || result.Cleanup != "complete" || targetNode == nil || targetNode.ExitCode == nil || *targetNode.ExitCode != wantExit || targetNode.Source != "direct_receipt" {
		t.Fatalf("direct result lost: %s", raw)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client, err := hostclient.Connect(ctx, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var snapshot eng.Snapshot
	if err = client.Do(ctx, "GET", "/api/v1/runs/"+result.RunID, nil, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.EngineSHA256 != "7127df85c0d0c62f2f79b4d2b3082ca1d85f7c0c74523457617861d32150b0dd" || snapshot.CleanupScope != "registered_process_groups" {
		t.Fatalf("engine identity/cleanup missing: %+v", snapshot)
	}
	return result.RunID
}
func assertMarkerAbsent(t *testing.T, marker string) {
	t.Helper()
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("system PATH engine unexpectedly executed")
	}
}
func TestBundledArchiveRunsWithoutSystemEngine(t *testing.T) {
	root := extractedBundle(t)
	env, marker := bundleEnv(t)
	workspace := bundleWorkspace(t, root)
	bin := filepath.Join(root, "runcradle")
	// Read-only package: no runtime extraction or cache writes are permitted here.
	filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		mode := os.FileMode(0444)
		if entry.IsDir() || path == bin || path == filepath.Join(root, "libexec", "process-compose") {
			mode = 0555
		}
		return os.Chmod(path, mode)
	})
	t.Cleanup(func() {
		filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err == nil && entry.IsDir() {
				os.Chmod(path, 0755)
			}
			return nil
		})
	})
	stop := bundleHost(t, bin, workspace, env)
	for _, tc := range []struct {
		target string
		exit   int
	}{{"success", 0}, {"failure", 7}} {
		bundleRun(t, bin, workspace, env, tc.target, "bundle-"+tc.target, tc.exit)
	}
	stop()
	bundleCall(t, bin, workspace, env, 0, "workflow", "inspect", "--workspace", workspace, "--request-id", "bundle-failure", "--node", "failure")
	assertMarkerAbsent(t, marker)
}
func TestBundledRelocationAndExplicitOverride(t *testing.T) {
	root := extractedBundle(t)
	env, marker := bundleEnv(t)
	workspace := bundleWorkspace(t, root)
	bin := filepath.Join(root, "runcradle")
	link := filepath.Join(t.TempDir(), "command-link")
	if err := os.Symlink(bin, link); err != nil {
		t.Fatal(err)
	}
	stop := bundleHost(t, link, workspace, env)
	bundleRun(t, link, workspace, env, "success", "relocated", 0)
	stop()
	// Explicit overrides deliberately take precedence even over damaged bundle metadata.
	os.WriteFile(filepath.Join(root, "bundle.json"), []byte("{}"), 0644)
	for _, path := range []string{filepath.Join(root, "libexec", "process-compose"), "./libexec/process-compose"} {
		bundleCall(t, link, root, env, 0, "workflow", "validate", "--workspace", workspace, "--file", "hello.yaml", "--check-engine", "--process-compose", path)
	}
	for _, path := range []string{"", "./missing"} {
		dir := t.TempDir()
		bundleCall(t, bin, root, env, 2, "serve", "--workspace", dir, "--process-compose", path)
		if _, err := os.Stat(filepath.Join(dir, ".runcradle")); !os.IsNotExist(err) {
			t.Fatal("explicit failure left workspace residue")
		}
	}
	assertMarkerAbsent(t, marker)
}
func TestBundledCorruptionAndOfflineUse(t *testing.T) {
	root := extractedBundle(t)
	env, marker := bundleEnv(t)
	workspace := bundleWorkspace(t, root)
	bin := filepath.Join(root, "runcradle")
	enginePath := filepath.Join(root, "libexec", "process-compose")
	manifestPath := filepath.Join(root, "bundle.json")
	stop := bundleHost(t, bin, workspace, env)
	bundleRun(t, bin, workspace, env, "success", "offline-history", 0)
	stop()
	engineRaw, _ := os.ReadFile(enginePath)
	manifestRaw, _ := os.ReadFile(manifestPath)
	for _, kind := range []string{"missing-engine", "missing-manifest", "invalid-manifest", "wrong-identity", "oversize-manifest", "permissions", "wrong-architecture", "tampered-engine", "escaping-symlink"} {
		t.Run(kind, func(t *testing.T) {
			defer func() {
				os.Remove(enginePath)
				os.WriteFile(enginePath, engineRaw, 0755)
				os.WriteFile(manifestPath, manifestRaw, 0644)
			}()
			switch kind {
			case "missing-engine":
				os.Remove(enginePath)
			case "missing-manifest":
				os.Remove(manifestPath)
			case "invalid-manifest":
				os.WriteFile(manifestPath, []byte("{"), 0644)
			case "wrong-identity":
				var m map[string]any
				json.Unmarshal(manifestRaw, &m)
				m["package_version"] = "9.9.9"
				raw, _ := json.Marshal(m)
				os.WriteFile(manifestPath, raw, 0644)
			case "oversize-manifest":
				os.WriteFile(manifestPath, bytes.Repeat([]byte(" "), 65537), 0644)
			case "permissions":
				os.Chmod(enginePath, 0644)
			case "wrong-architecture":
				changed := append([]byte{}, engineRaw...)
				changed[18] = 183
				os.WriteFile(enginePath, changed, 0755)
			case "tampered-engine":
				os.WriteFile(enginePath, []byte("#!/bin/sh\nprintf bad > '"+marker+"'\nprintf 'v1.122.0\\n'\n"), 0755)
			case "escaping-symlink":
				os.Remove(enginePath)
				os.Symlink(filepath.Join(filepath.Dir(marker), "process-compose"), enginePath)
			}
			dir := t.TempDir()
			bundleCall(t, bin, workspace, env, 2, "serve", "--workspace", dir)
			if _, err := os.Stat(filepath.Join(dir, ".runcradle")); !os.IsNotExist(err) {
				t.Fatal("damaged installation left private workspace files")
			}
			bundleCall(t, bin, workspace, env, 2, "workflow", "validate", "--workspace", workspace, "--file", "hello.yaml", "--check-engine")
			bundleCall(t, bin, workspace, env, 0, "workflow", "validate", "--workspace", workspace, "--file", "hello.yaml")
			bundleCall(t, bin, workspace, env, 0, "workflow", "render", "--workspace", workspace, "--file", "hello.yaml", "--out", "offline.html", "--force")
			bundleCall(t, bin, workspace, env, 0, "workflow", "inspect", "--workspace", workspace, "--request-id", "offline-history")
			bundleCall(t, bin, workspace, env, 0, "version")
			bundleCall(t, bin, workspace, env, 0, "help")
			assertMarkerAbsent(t, marker)
		})
	}
	copied := filepath.Join(t.TempDir(), "runcradle")
	raw, _ := os.ReadFile(bin)
	if err := os.WriteFile(copied, raw, 0755); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bundleCall(t, copied, workspace, env, 2, "serve", "--workspace", dir)
	if _, err := os.Stat(filepath.Join(dir, ".runcradle")); !os.IsNotExist(err) {
		t.Fatal("incomplete copied installation opened workspace")
	}
}
func TestBundledDirectorySwitch(t *testing.T) {
	root1 := extractedBundle(t)
	root2 := extractedBundle(t)
	env, marker := bundleEnv(t)
	workspace := bundleWorkspace(t, root1)
	stop := bundleHost(t, filepath.Join(root1, "runcradle"), workspace, env)
	bundleRun(t, filepath.Join(root1, "runcradle"), workspace, env, "success", "old-install", 0)
	stop()
	stop = bundleHost(t, filepath.Join(root2, "runcradle"), workspace, env)
	defer stop()
	bundleCall(t, filepath.Join(root2, "runcradle"), workspace, env, 0, "workflow", "inspect", "--workspace", workspace, "--request-id", "old-install")
	bundleRun(t, filepath.Join(root2, "runcradle"), workspace, env, "failure", "new-install", 7)
	assertMarkerAbsent(t, marker)
	t.Log(fmt.Sprintf("B-09: stopped old installation and resumed same workspace with a second extracted directory"))
}
