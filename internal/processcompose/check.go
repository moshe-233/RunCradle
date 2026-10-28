package processcompose

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"runcradle/internal/workflow"
)

const Version = "v1.122.0"

// Probe observes the installed version without loading the caller's .pc_env.
func Probe(ctx context.Context, path string) (version string, err error) {
	if path == "" {
		path = "process-compose"
	}
	binary, e := exec.LookPath(path)
	if e != nil {
		return "", e
	}
	binary, e = filepath.Abs(binary)
	if e != nil {
		return "", e
	}
	temp, e := os.MkdirTemp("", "runcradle-version-")
	if e != nil {
		return "", fmt.Errorf("%w: cannot create private version probe directory: %v", ErrInternal, e)
	}
	defer func() {
		if e := os.RemoveAll(temp); e != nil {
			err = errors.Join(err, fmt.Errorf("%w: cannot remove version probe directory: %v", ErrInternal, e))
		}
	}()
	op, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, out, _, e := child(op, binary, temp, "--log-file", "/dev/null", "version", "--short")
	if e != nil {
		return "", e
	}
	version = strings.TrimSpace(out.String())
	if version != Version {
		return "", fmt.Errorf("require Process Compose %s", Version)
	}
	return version, nil
}

type Result struct {
	Status     string
	Code       int
	Message    string
	EngineExit *int
}
type tail struct {
	mu   sync.Mutex
	data []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := len(p)
	if len(p) >= 65536 {
		t.data = append(t.data[:0], p[len(p)-65536:]...)
	} else {
		if len(t.data)+len(p) > 65536 {
			t.data = append([]byte(nil), t.data[len(t.data)+len(p)-65536:]...)
		}
		t.data = append(t.data, p...)
	}
	return n, nil
}
func (t *tail) String() string { t.mu.Lock(); defer t.mu.Unlock(); return string(t.data) }
func unsupported(v any) bool {
	switch x := v.(type) {
	case string:
		return strings.Contains(x, "$") || strings.Contains(x, "{{")
	case map[string]any:
		for k, v := range x {
			if strings.Contains(k, "$") || strings.Contains(k, "{{") {
				return true
			}
			switch k {
			case "extends", "env_file", "env_files", "environment_files", "vars", "env_cmd":
				return true
			case "replicas":
				if fmt.Sprint(v) != "1" {
					return true
				}
			}
			if unsupported(v) {
				return true
			}
		}
	case []any:
		for _, v := range x {
			if unsupported(v) {
				return true
			}
		}
	}
	return false
}
func child(ctx context.Context, binary, temp string, args ...string) (*exec.Cmd, *tail, *tail, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = temp // v1.122.0 loads .pc_env before flags; never use the workspace cwd.
	cmd.Env = []string{"PATH=/usr/bin:/bin", "TERM=dumb", "NO_COLOR=1", "XDG_CONFIG_HOME=" + temp, "XDG_CONFIG_DIRS=" + temp}
	cmd.WaitDelay = 100 * time.Millisecond
	stdout, stderr := &tail{}, &tail{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	return cmd, stdout, stderr, err
}
func Check(ctx context.Context, d workflow.Document, path string) (result Result) {
	result = Result{Status: "unsupported", Code: 2, Message: "engine check requires a self-contained file without templates, environment expansion, external file loading or replica expansion"}
	if !d.Valid() {
		result.Message = "static validation must pass first"
		return
	}
	if unsupported(d.Native) || bytes.Contains(d.Source, []byte("$")) || bytes.Contains(d.Source, []byte("{{")) {
		return
	}
	if ctx.Err() != nil {
		return Result{Status: "cancelled", Code: 130, Message: "engine check cancelled"}
	}
	if path == "" {
		path = "process-compose"
	}
	binary, err := exec.LookPath(path)
	if err != nil {
		return Result{Status: "unavailable", Code: 2, Message: "Process Compose v1.122.0 is required for explicit engine checks"}
	}
	binary, err = filepath.Abs(binary)
	if err != nil {
		return Result{Status: "unavailable", Code: 2, Message: "cannot resolve engine path"}
	}
	temp, err := os.MkdirTemp("", "runcradle-check-")
	if err != nil {
		return Result{Status: "error", Code: 125, Message: "cannot create private validation directory"}
	}
	defer func() {
		if err := os.RemoveAll(temp); err != nil {
			result = Result{Status: "error", Code: 125, Message: "validation temporary files could not be removed"}
		}
	}()
	probe, cancel := context.WithTimeout(ctx, 2*time.Second)
	_, stdout, _, err := child(probe, binary, temp, "--log-file", "/dev/null", "version", "--short")
	cancel()
	if ctx.Err() != nil {
		return Result{Status: "cancelled", Code: 130, Message: "engine check cancelled"}
	}
	if err != nil || strings.TrimSpace(stdout.String()) != Version {
		return Result{Status: "unavailable", Code: 2, Message: "engine version check failed; require v1.122.0"}
	}
	config := filepath.Join(temp, "process-compose."+d.Format)
	if err = os.WriteFile(config, d.Source, 0600); err != nil {
		return Result{Status: "error", Code: 125, Message: "cannot write private validation input"}
	}
	check, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd, _, stderr, runErr := child(check, binary, temp, "up", "--config", config, "--dry-run", "--tui=false", "--no-server", "--no-watch", "--disable-dotenv", "--log-file", "/dev/stderr", "--log-no-color")
	if ctx.Err() != nil {
		return Result{Status: "cancelled", Code: 130, Message: "engine check cancelled"}
	}
	if check.Err() != nil {
		return Result{Status: "timeout", Code: 124, Message: "engine validation exceeded five seconds"}
	}
	var exit *int
	if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() >= 0 {
		n := cmd.ProcessState.ExitCode()
		exit = &n
	}
	if runErr != nil {
		if exit == nil || *exit == 0 {
			return Result{Status: "error", Code: 125, Message: "engine output or process completion could not be verified", EngineExit: exit}
		}
		message := fmt.Sprintf("upstream rejected the declaration (engine exit %d)", *exit)
		if hint := reportedHint(stderr.String()); hint != "" {
			message += "; reported: " + hint
		}
		return Result{Status: "rejected", Code: 2, Message: message, EngineExit: exit}
	}
	return Result{Status: "passed", Code: 0, Message: "upstream dry-run accepted this declaration; no task execution was tested", EngineExit: exit}
}

func reportedHint(text string) string {
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		var entry struct {
			Level   string `json:"level"`
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		if json.Unmarshal([]byte(lines[i]), &entry) != nil || (entry.Level != "error" && entry.Level != "fatal") {
			continue
		}
		hint := entry.Error
		if hint == "" {
			hint = entry.Message
		}
		hint = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return ' '
			}
			return r
		}, hint)
		hint = strings.Join(strings.Fields(hint), " ")
		if len(hint) > 256 {
			hint = hint[:256]
			for !utf8.ValidString(hint) {
				hint = hint[:len(hint)-1]
			}
			hint += "…"
		}
		return hint
	}
	return ""
}
