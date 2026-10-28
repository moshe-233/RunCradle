package processcompose

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxBinarySize int64 = 256 << 20

var ErrInternal = errors.New("internal engine resolution error")

type ResolveOptions struct {
	Explicit       string
	ExplicitSet    bool
	Executable     string
	WorkingDir     string
	SearchPath     string
	RequireBundle  bool
	ProductVersion string
	ProductCommit  string
}
type ResolvedBinary struct {
	Path    string
	Source  string
	Version string
	SHA256  string
}
type BundleEngine struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}
type BundleFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type BundleManifest struct {
	SchemaVersion  int          `json:"schema_version"`
	PackageVersion string       `json:"package_version"`
	SourceCommit   string       `json:"source_commit"`
	SourceDirty    bool         `json:"source_dirty"`
	OS             string       `json:"os"`
	Arch           string       `json:"arch"`
	Engine         BundleEngine `json:"engine"`
	Files          []BundleFile `json:"files"`
}

// ResolveBinary selects exactly one source. An invalid higher-priority source
// fails closed; notably, no bundled bytes are executed before checking the pin.
func ResolveBinary(ctx context.Context, opts ResolveOptions) (ResolvedBinary, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return ResolvedBinary{}, err
	}
	source, path, root := "explicit", opts.Explicit, ""
	if opts.ExplicitSet {
		if path == "" {
			return ResolvedBinary{}, fmt.Errorf("--process-compose must not be empty; check the explicit engine path")
		}
		var err error
		path, err = lookupBinary(path, opts.WorkingDir, opts.SearchPath)
		if err != nil {
			return ResolvedBinary{}, fmt.Errorf("check --process-compose: %w", err)
		}
	} else {
		exe, err := filepath.EvalSymlinks(opts.Executable)
		if err != nil {
			return ResolvedBinary{}, fmt.Errorf("cannot locate RunCradle installation: %w", err)
		}
		root, err = filepath.Abs(filepath.Dir(exe))
		if err != nil {
			return ResolvedBinary{}, err
		}
		bundled := opts.RequireBundle
		for _, marker := range []string{"bundle.json", "libexec/process-compose"} {
			_, err := os.Lstat(filepath.Join(root, marker))
			if err == nil {
				bundled = true
			} else if !os.IsNotExist(err) {
				return ResolvedBinary{}, fmt.Errorf("check installation: %w", err)
			}
		}
		if bundled {
			source = "bundled"
			path = filepath.Join(root, "libexec", "process-compose")
			if err := validateBundle(root, opts); err != nil {
				return ResolvedBinary{}, fmt.Errorf("re-extract the complete RunCradle bundle: %w", err)
			}
		} else {
			source = "path"
			root = ""
			path, err = lookupBinary("process-compose", opts.WorkingDir, opts.SearchPath)
			if err != nil {
				return ResolvedBinary{}, fmt.Errorf("source development requires make prepare or --process-compose PATH: %w", err)
			}
		}
	}
	path, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ResolvedBinary{}, enginePathError(source, err)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return ResolvedBinary{}, err
	}
	if root != "" && !within(root, path) {
		return ResolvedBinary{}, enginePathError(source, fmt.Errorf("engine path escapes installation"))
	}
	sum, err := binaryHash(ctx, path)
	if err != nil {
		return ResolvedBinary{}, enginePathError(source, err)
	}
	if source == "bundled" {
		pin, err := PinnedEngine()
		if err != nil {
			return ResolvedBinary{}, err
		}
		if sum != pin.BinarySHA256 {
			return ResolvedBinary{}, enginePathError(source, fmt.Errorf("engine SHA-256 mismatch; refusing to execute"))
		}
	}
	version, err := Probe(ctx, path)
	if ctx.Err() != nil {
		return ResolvedBinary{}, ctx.Err()
	}
	if err != nil {
		return ResolvedBinary{}, enginePathError(source, err)
	}
	return ResolvedBinary{Path: path, Source: source, Version: version, SHA256: sum}, nil
}
func enginePathError(source string, err error) error {
	if errors.Is(err, ErrInternal) {
		return err
	}
	if source == "bundled" {
		return fmt.Errorf("re-extract the complete RunCradle bundle: %w", err)
	}
	return fmt.Errorf("check %s Process Compose path: %w", source, err)
}
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
func lookupBinary(name, cwd, search string) (string, error) {
	if strings.ContainsRune(name, filepath.Separator) {
		if !filepath.IsAbs(name) {
			name = filepath.Join(cwd, name)
		}
		return filepath.Abs(name)
	}
	for _, dir := range filepath.SplitList(search) {
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(cwd, dir)
		}
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
			return filepath.Abs(path)
		}
	}
	return "", fmt.Errorf("%s not found on PATH", name)
}
func binaryHash(ctx context.Context, path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Size() > maxBinarySize {
		return "", fmt.Errorf("engine must be an executable regular file <= 256 MiB")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, 128<<10)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, err := f.Read(buf)
		total += int64(n)
		if total > maxBinarySize {
			return "", fmt.Errorf("engine exceeds 256 MiB")
		}
		h.Write(buf[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func validateBundle(root string, opts ResolveOptions) error {
	path, err := filepath.EvalSymlinks(filepath.Join(root, "bundle.json"))
	if err != nil {
		return err
	}
	if !within(root, path) {
		return fmt.Errorf("manifest path escapes installation")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return fmt.Errorf("bundle.json must be a regular file <= 64 KiB")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if err != nil {
		return err
	}
	if len(raw) > 64<<10 {
		return fmt.Errorf("bundle.json exceeds 64 KiB")
	}
	var manifest BundleManifest
	if err = json.Unmarshal(raw, &manifest); err != nil {
		return fmt.Errorf("invalid bundle.json: %w", err)
	}
	pin, err := PinnedEngine()
	if err != nil {
		return err
	}
	if manifest.SchemaVersion != 1 || manifest.OS != pin.OS || manifest.Arch != pin.Arch || manifest.PackageVersion != opts.ProductVersion || manifest.SourceCommit != opts.ProductCommit || manifest.Engine.Path != "libexec/process-compose" || manifest.Engine.Version != pin.Version || manifest.Engine.SHA256 != pin.BinarySHA256 {
		return fmt.Errorf("bundle identity does not match this executable and pinned engine")
	}
	return nil
}
