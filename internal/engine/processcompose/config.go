package processcompose

import (
	"bytes"
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"os/exec"
	"path/filepath"
	"runcradle/internal/workflow"
	"strings"
)

func unsupported(v any, path string) error {
	switch x := v.(type) {
	case string:
		if strings.Contains(x, "$") || strings.Contains(x, "{{") {
			return fmt.Errorf("unsupported expansion at %s", path)
		}
	case []any:
		for i, v := range x {
			if e := unsupported(v, fmt.Sprintf("%s[%d]", path, i)); e != nil {
				return e
			}
		}
	case map[string]any:
		for k, v := range x {
			p := path + "." + k
			switch k {
			case "extends", "env_file", "env_files", "environment_files", "env_cmd", "vars", "watch", "schedule", "mcp", "mcp_server", "log_location", "log_configuration", "send_keys":
				return fmt.Errorf("unsupported runtime field %s", p)
			case "is_daemon", "is_tty", "is_interactive", "is_foreground", "is_elevated", "parent_only":
				if v != false {
					return fmt.Errorf("unsupported runtime field %s", p)
				}
			case "replicas":
				if fmt.Sprint(v) != "1" {
					return fmt.Errorf("unsupported replica expansion at %s", p)
				}
			case "restart":
				if v != "no" && v != "exit_on_failure" {
					return fmt.Errorf("unsupported automatic restart at %s", p)
				}
			}
			if k == "shutdown" {
				if m, ok := v.(map[string]any); ok && m["command"] != nil {
					return fmt.Errorf("unsupported shutdown command at %s", p)
				}
			}
			if e := unsupported(v, p); e != nil {
				return e
			}
		}
	}
	return nil
}

func field(n *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}
func set(n *yaml.Node, key string, value any) error {
	var v yaml.Node
	if e := v.Encode(value); e != nil {
		return e
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			n.Content[i+1] = &v
			return nil
		}
	}
	n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, &v)
	return nil
}
func remove(n *yaml.Node, key string) {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			n.Content = append(n.Content[:i], n.Content[i+2:]...)
			return
		}
	}
}
func workingDir(workspace string, value any) (string, error) {
	name := "."
	if value != nil {
		var ok bool
		name, ok = value.(string)
		if !ok {
			return "", fmt.Errorf("working_dir must be text")
		}
	}
	if name == "" {
		name = "."
	}
	if !filepath.IsAbs(name) {
		name = filepath.Join(workspace, name)
	}
	name, e := filepath.EvalSymlinks(name)
	if e != nil {
		return "", e
	}
	rel, e := filepath.Rel(workspace, name)
	if e != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("working_dir must be inside workspace")
	}
	st, e := os.Stat(name)
	if e != nil {
		return "", e
	}
	if !st.IsDir() {
		return "", fmt.Errorf("working_dir must be a directory")
	}
	return name, nil
}

// Prepare changes only invocation and path fields in an independent syntax tree.
// The original bytes/AST/revision remain authoritative for history and display.
func Prepare(d workflow.Document, helper, runID, receiptDir string) ([]byte, error) {
	for _, part := range filepath.SplitList(os.Getenv("PATH")) {
		if part == "" || !filepath.IsAbs(part) {
			return nil, fmt.Errorf("unsupported inherited relative PATH; use absolute search directories")
		}
	}
	if !d.Valid() || !d.View.Complete || d.View.RawEditOnly {
		return nil, fmt.Errorf("unsupported or invalid runtime definition")
	}
	if e := unsupported(d.Native, "$"); e != nil {
		return nil, e
	}
	var tree yaml.Node
	if e := yaml.Unmarshal(d.Source, &tree); e != nil {
		return nil, e
	}
	root := tree.Content[0]
	processes := field(root, "processes")
	if processes == nil || len(processes.Content) == 0 {
		return nil, fmt.Errorf("workflow must have nodes")
	}
	shell := os.Getenv("COMPOSE_SHELL")
	if shell == "" {
		shell = "bash"
	}
	shellArg := "-c"
	if conf, ok := d.Native["shell"].(map[string]any); ok {
		var valid bool
		shell, valid = conf["shell_command"].(string)
		if !valid || shell == "" {
			return nil, fmt.Errorf("shell_command is required")
		}
		shellArg, valid = conf["shell_argument"].(string)
		if !valid || shellArg == "" {
			return nil, fmt.Errorf("shell_argument is required")
		}
	}
	for i := 0; i < len(processes.Content); i += 2 {
		name := processes.Content[i].Value
		p := processes.Content[i+1]
		cfg := d.Native["processes"].(map[string]any)[name].(map[string]any)
		var argv []string
		if command, ok := cfg["command"].(string); ok && command != "" {
			argv = []string{shell, shellArg, command}
		} else if a, ok := cfg["entrypoint"].([]any); ok {
			for _, v := range a {
				argv = append(argv, v.(string))
			}
		}
		if len(argv) == 0 {
			return nil, fmt.Errorf("process %s has no command", name)
		}
		// Upstream resolves names using the engine environment before applying
		// the process environment; freeze that selection for the direct helper.
		if !strings.ContainsRune(argv[0], '/') {
			path, e := exec.LookPath(argv[0])
			if e != nil {
				return nil, fmt.Errorf("process %s executable unavailable", name)
			}
			argv[0] = path
		}
		wd, e := workingDir(d.Workspace, cfg["working_dir"])
		if e != nil {
			return nil, e
		}
		if e = set(p, "working_dir", wd); e != nil {
			return nil, e
		}
		for _, kind := range []string{"readiness_probe", "liveness_probe"} {
			probe := field(p, kind)
			if probe == nil {
				continue
			}
			ex := field(probe, "exec")
			if ex == nil {
				continue
			}
			if raw := field(ex, "working_dir"); raw != nil && raw.Value != "" {
				resolved, e := workingDir(d.Workspace, raw.Value)
				if e != nil {
					return nil, e
				}
				if e = set(ex, "working_dir", resolved); e != nil {
					return nil, e
				}
			}
		}
		entry := append([]string{helper, "_exec", "--receipt-dir", receiptDir, "--run-id", runID, "--node-id", name, "--"}, argv...)
		remove(p, "command")
		if e = set(p, "entrypoint", entry); e != nil {
			return nil, e
		}
	}
	raw, err := yaml.Marshal(&tree)
	if err == nil && (bytes.Contains(raw, []byte("$")) || bytes.Contains(raw, []byte("{{"))) {
		return nil, fmt.Errorf("generated execution paths contain unsupported native expansion markers")
	}
	return raw, err
}
