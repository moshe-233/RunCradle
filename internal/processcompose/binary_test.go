package processcompose

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeBinary(t *testing.T, dir, name, script string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}
func bundleFixture(t *testing.T) (ResolveOptions, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "中文 安装")
	if err := os.MkdirAll(filepath.Join(root, "libexec"), 0755); err != nil {
		t.Fatal(err)
	}
	exe := fakeBinary(t, root, "runcradle", "exit 0")
	pin, _ := PinnedEngine()
	manifest := BundleManifest{SchemaVersion: 1, PackageVersion: "1.2.3", SourceCommit: "test-commit", OS: "linux", Arch: "amd64", Engine: BundleEngine{Path: "libexec/process-compose", Version: pin.Version, SHA256: pin.BinarySHA256}}
	raw, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(root, "bundle.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	return ResolveOptions{Executable: exe, WorkingDir: root, ProductVersion: "1.2.3", ProductCommit: "test-commit"}, root
}
func TestResolveBinarySelection(t *testing.T) {
	dir := t.TempDir()
	bin := fakeBinary(t, dir, "pc", "printf 'v1.122.0\\n'")
	fakeBinary(t, dir, "process-compose", "printf 'v1.122.0\\n'")
	exe := fakeBinary(t, dir, "runcradle", "exit 0")
	for _, tc := range []struct {
		name, value, source string
		set                 bool
		wantErr             bool
	}{
		{"absolute", bin, "explicit", true, false}, {"relative", "./pc", "explicit", true, false}, {"name", "pc", "explicit", true, false},
		{"empty", "", "", true, true}, {"missing", "./absent", "", true, true}, {"source", "", "path", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveBinary(context.Background(), ResolveOptions{Explicit: tc.value, ExplicitSet: tc.set, Executable: exe, WorkingDir: dir, SearchPath: dir})
			if (err != nil) != tc.wantErr {
				t.Fatalf("got %+v, err %v", got, err)
			}
			if err == nil && (got.Source != tc.source || got.Version != "v1.122.0" || len(got.SHA256) != 64 || !filepath.IsAbs(got.Path)) {
				t.Fatalf("bad selection %+v", got)
			}
		})
	}
}
func TestBundledValidationBeforeExec(t *testing.T) {
	opts, root := bundleFixture(t)
	marker := filepath.Join(root, "executed")
	fakeBinary(t, filepath.Join(root, "libexec"), "process-compose", "echo bad > '"+marker+"'; printf 'v1.122.0\\n'")
	if _, err := ResolveBinary(context.Background(), opts); err == nil {
		t.Fatal("accepted changed engine")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("untrusted engine executed")
	}
}
func TestBundleNeverFallsBack(t *testing.T) {
	for _, kind := range []string{"required", "manifest", "engine", "dangling"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			marker := filepath.Join(dir, "executed")
			exe := fakeBinary(t, dir, "runcradle", "exit 0")
			fakeBinary(t, dir, "process-compose", "echo bad > '"+marker+"'; printf 'v1.122.0\\n'")
			opts := ResolveOptions{Executable: exe, WorkingDir: dir, SearchPath: dir, RequireBundle: kind == "required"}
			switch kind {
			case "manifest":
				os.WriteFile(filepath.Join(dir, "bundle.json"), []byte("{}"), 0600)
			case "engine":
				os.Mkdir(filepath.Join(dir, "libexec"), 0700)
				fakeBinary(t, filepath.Join(dir, "libexec"), "process-compose", "exit 0")
			case "dangling":
				os.Symlink("missing", filepath.Join(dir, "bundle.json"))
			}
			if _, err := ResolveBinary(context.Background(), opts); err == nil {
				t.Fatal("accepted incomplete bundle")
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("PATH fallback executed")
			}
		})
	}
}
func TestExecutableSymlinkLocation(t *testing.T) {
	opts, root := bundleFixture(t)
	engine := os.Getenv("RUNCRADLE_PROCESS_COMPOSE")
	if engine == "" {
		engine = filepath.Join("..", "..", ".cache", "process-compose", "v1.122.0", "process-compose")
	}
	raw, err := os.ReadFile(engine)
	if os.IsNotExist(err) {
		t.Skip("make prepare for official bundled binary test")
	}
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "libexec", "process-compose"), raw, 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "rc")
	if err = os.Symlink(opts.Executable, link); err != nil {
		t.Fatal(err)
	}
	opts.Executable = link
	opts.WorkingDir = t.TempDir()
	got, err := ResolveBinary(context.Background(), opts)
	if err != nil || got.Source != "bundled" || got.Path != filepath.Join(root, "libexec", "process-compose") {
		t.Fatalf("%+v %v", got, err)
	}
	for _, kind := range []string{"version", "commit", "platform", "path", "hash", "oversize", "escape", "permissions"} {
		t.Run(kind, func(t *testing.T) {
			changed := opts
			raw, _ := os.ReadFile(filepath.Join(root, "bundle.json"))
			defer os.WriteFile(filepath.Join(root, "bundle.json"), raw, 0600)
			var m BundleManifest
			json.Unmarshal(raw, &m)
			switch kind {
			case "version":
				changed.ProductVersion = "other"
			case "commit":
				changed.ProductCommit = "other"
			case "platform":
				m.Arch = "arm64"
			case "path":
				m.Engine.Path = "../pc"
			case "hash":
				m.Engine.SHA256 = strings.Repeat("0", 64)
			case "oversize":
				os.WriteFile(filepath.Join(root, "bundle.json"), []byte(strings.Repeat(" ", 65537)), 0600)
			case "escape":
				os.Rename(filepath.Join(root, "libexec", "process-compose"), filepath.Join(root, "pc"))
				os.Symlink(filepath.Join(root, "..", "outside"), filepath.Join(root, "libexec", "process-compose"))
				defer func() {
					os.Remove(filepath.Join(root, "libexec", "process-compose"))
					os.Rename(filepath.Join(root, "pc"), filepath.Join(root, "libexec", "process-compose"))
				}()
			case "permissions":
				os.Chmod(filepath.Join(root, "libexec", "process-compose"), 0600)
				defer os.Chmod(filepath.Join(root, "libexec", "process-compose"), 0755)
			}
			if kind != "oversize" {
				b, _ := json.Marshal(m)
				os.WriteFile(filepath.Join(root, "bundle.json"), b, 0600)
			}
			if _, err := ResolveBinary(context.Background(), changed); err == nil {
				t.Fatal("accepted invalid bundle")
			}
		})
	}
}
func TestResolveBinaryCancellation(t *testing.T) {
	dir := t.TempDir()
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	bin := fakeBinary(t, dir, "pc", "exec /bin/sleep 20")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := ResolveBinary(ctx, ResolveOptions{Explicit: bin, ExplicitSet: true})
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("not bounded: %v", err)
	}
	entries, _ := os.ReadDir(tmp)
	if len(entries) != 0 {
		t.Fatal("probe temporary files leaked")
	}
}

func TestResolveBinaryInternalIOFailure(t *testing.T) {
	dir := t.TempDir()
	bin := fakeBinary(t, dir, "pc", "printf 'v1.122.0\\n'")
	t.Setenv("TMPDIR", filepath.Join(dir, "missing"))
	_, err := ResolveBinary(context.Background(), ResolveOptions{Explicit: bin, ExplicitSet: true})
	if !errors.Is(err, ErrInternal) {
		t.Fatalf("probe internal IO misclassified: %v", err)
	}
}
