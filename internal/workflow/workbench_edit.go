package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

type EnvEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
type DependencyEntry struct {
	From      string `json:"from"`
	Condition string `json:"condition"`
}
type WorkbenchEdit struct {
	Action        string            `json:"action"`
	Process       string            `json:"process,omitempty"`
	NewName       string            `json:"new_name,omitempty"`
	Label         *string           `json:"label,omitempty"`
	Upstream      string            `json:"upstream,omitempty"`
	CommandMode   string            `json:"command_mode,omitempty"`
	Command       string            `json:"command,omitempty"`
	Entrypoint    []string          `json:"entrypoint,omitempty"`
	WorkingDir    *string           `json:"working_dir,omitempty"`
	EnvItems      []EnvEntry        `json:"env_items,omitempty"`
	DependsOn     []DependencyEntry `json:"depends_on,omitempty"`
	Restart       *string           `json:"restart,omitempty"`
	ExitOnEnd     *bool             `json:"exit_on_end,omitempty"`
	ExitOnSkipped *bool             `json:"exit_on_skipped,omitempty"`
	InputSpec     *Input            `json:"input_spec,omitempty"`
	InputName     string            `json:"input_name,omitempty"`
	EnvKey        string            `json:"env_key,omitempty"`
	PresetSpec    *Preset           `json:"preset_spec,omitempty"`
	PresetID      string            `json:"preset_id,omitempty"`
	Title         *string           `json:"title,omitempty"`
	Description   *string           `json:"description,omitempty"`
}

// ApplyWorkbenchEdit edits an independent syntax tree and commits only a valid pair of documents.
func ApplyWorkbenchEdit(d Document, req WorkbenchEdit) (Document, error) {
	if !d.Valid() {
		return d, fmt.Errorf("invalid definition; correct the source first")
	}
	native := req.Action == "add_process" || req.Action == "delete_process" || req.Action == "rename_process" || req.Action == "update_process"
	if native && d.View.RawEditOnly {
		return d, fmt.Errorf("shared YAML syntax requires raw source editing")
	}
	next := rebuild(d, d.Source, d.UIRaw) // metadata and AST must not alias the caller
	tree := next.AST
	processes := findNode(tree, "processes")
	process := findNode(tree, "processes", req.Process)
	if native && req.Action != "add_process" && (process == nil || process.Kind != yaml.MappingNode) {
		return d, fmt.Errorf("unknown or unsupported process %q", req.Process)
	}
	metaChanged := !native || req.Label != nil || (next.Metadata != nil && (req.Action == "delete_process" || req.Action == "rename_process"))
	if metaChanged && next.Metadata == nil {
		next.Metadata = &Metadata{Version: 1}
	}
	m := next.Metadata
	switch req.Action {
	case "add_process":
		if !workbenchName(req.Process) || findNode(processes, req.Process) != nil {
			return d, fmt.Errorf("process name is invalid or already exists")
		}
		process = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		wbSet(process, "command", wbNode("echo ready"))
		wbSet(process, "availability", wbNode(map[string]any{"exit_on_end": true}))
		wbSet(processes, req.Process, process)
		if req.Upstream != "" {
			if findNode(processes, req.Upstream) == nil || req.Upstream == req.Process {
				return d, fmt.Errorf("invalid upstream process")
			}
			wbSet(process, "depends_on", wbNode(map[string]any{req.Upstream: map[string]string{"condition": "process_completed_successfully"}}))
		}
	case "delete_process":
		wbSet(processes, req.Process, nil)
		for i := 1; i < len(processes.Content); i += 2 {
			if deps := findNode(processes.Content[i], "depends_on"); deps != nil {
				wbSet(deps, req.Process, nil)
			}
		}
		if m != nil {
			delete(m.Nodes, req.Process)
			wbRemoveBindings(m, func(b Binding) bool { return b.Process == req.Process })
		}
	case "rename_process":
		if !workbenchName(req.NewName) || (req.NewName != req.Process && findNode(processes, req.NewName) != nil) {
			return d, fmt.Errorf("process name is invalid or already exists")
		}
		wbRename(processes, req.Process, req.NewName)
		for i := 1; i < len(processes.Content); i += 2 {
			if deps := findNode(processes.Content[i], "depends_on"); deps != nil {
				wbRename(deps, req.Process, req.NewName)
			}
		}
		if m != nil {
			if v, ok := m.Nodes[req.Process]; ok {
				delete(m.Nodes, req.Process)
				m.Nodes[req.NewName] = v
			}
			for i := range m.Inputs {
				for j := range m.Inputs[i].Bindings {
					if m.Inputs[i].Bindings[j].Process == req.Process {
						m.Inputs[i].Bindings[j].Process = req.NewName
					}
				}
			}
		}
	case "update_process":
		if e := wbUpdate(process, req); e != nil {
			return d, e
		}
	case "bind_input":
		if req.InputSpec == nil {
			return d, fmt.Errorf("input_spec required")
		}
		input := *req.InputSpec
		for _, old := range m.Inputs {
			if old.Name == input.Name {
				return d, fmt.Errorf("input name already exists; unbind before replacing")
			}
		}
		for _, v := range append(append([]json.RawMessage{}, input.Enum...), input.Default) {
			if len(v) > 0 && !wbSafeValue(v) {
				return d, fmt.Errorf("input contains expansion or NUL")
			}
		}
		m.Inputs = append(m.Inputs, input)
	case "unbind_input":
		if req.InputName == "" || req.Process == "" || req.EnvKey == "" {
			return d, fmt.Errorf("input_name, process and env_key required")
		}
		found := false
		for i := range m.Inputs {
			if m.Inputs[i].Name == req.InputName {
				b := m.Inputs[i].Bindings[:0]
				for _, v := range m.Inputs[i].Bindings {
					if v.Process == req.Process && v.Key == req.EnvKey {
						found = true
					} else {
						b = append(b, v)
					}
				}
				m.Inputs[i].Bindings = b
			}
		}
		if !found {
			return d, fmt.Errorf("binding not found")
		}
		wbRemoveBindings(m, func(Binding) bool { return false })
	case "save_preset":
		if req.PresetSpec == nil {
			return d, fmt.Errorf("preset_spec required")
		}
		for _, v := range req.PresetSpec.Values {
			if !wbSafeValue(v) {
				return d, fmt.Errorf("preset contains expansion or NUL")
			}
		}
		found := false
		for i := range m.Presets {
			if m.Presets[i].ID == req.PresetSpec.ID {
				m.Presets[i] = *req.PresetSpec
				found = true
			}
		}
		if !found {
			m.Presets = append(m.Presets, *req.PresetSpec)
		}
	case "delete_preset":
		found := false
		out := m.Presets[:0]
		for _, p := range m.Presets {
			if p.ID == req.PresetID {
				found = true
			} else {
				out = append(out, p)
			}
		}
		if !found {
			return d, fmt.Errorf("preset not found")
		}
		m.Presets = out
	case "update_meta":
		if req.Title != nil {
			m.Title = *req.Title
		}
		if req.Description != nil {
			m.Description = *req.Description
		}
	default:
		return d, fmt.Errorf("unknown workbench action %q", req.Action)
	}
	if req.Label != nil {
		name := req.Process
		if req.Action == "rename_process" {
			name = req.NewName
		}
		if findNode(processes, name) == nil {
			return d, fmt.Errorf("unknown label process")
		}
		if m.Nodes == nil {
			m.Nodes = map[string]struct {
				Label string `json:"label"`
			}{}
		}
		m.Nodes[name] = struct {
			Label string `json:"label"`
		}{*req.Label}
	}
	source, ui := d.Source, d.UIRaw
	var err error
	if native {
		if d.Format == "json" {
			source, err = json.MarshalIndent(nodeValue(tree.Content[0]), "", "  ")
		} else {
			source, err = yaml.Marshal(tree)
		}
		if err != nil {
			return d, err
		}
	}
	if metaChanged {
		ui, err = json.MarshalIndent(m, "", "  ")
		if err != nil {
			return d, err
		}
		if next.UIPath == "" {
			next.UIPath = "ui.json"
		}
	}
	next = rebuild(next, source, ui)
	if !next.Valid() {
		return d, fmt.Errorf("edit rejected: %v", next.Issues)
	}
	return next, nil
}
func workbenchName(s string) bool {
	return s != "" && !strings.ContainsAny(s, "$\x00/\\") && !strings.Contains(s, "{{") && strings.IndexFunc(s, unicode.IsControl) < 0
}
func wbSafeValue(raw json.RawMessage) bool {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return true
	}
	return !strings.ContainsAny(s, "$\x00") && !strings.Contains(s, "{{")
}
func wbNode(v any) *yaml.Node { var n yaml.Node; _ = n.Encode(v); return &n }
func wbSet(m *yaml.Node, key string, v *yaml.Node) {
	for i := 0; i < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			if v == nil {
				m.Content = append(m.Content[:i], m.Content[i+2:]...)
			} else {
				old := m.Content[i+1]
				v.HeadComment, v.LineComment, v.FootComment = old.HeadComment, old.LineComment, old.FootComment
				m.Content[i+1] = v
			}
			return
		}
	}
	if v != nil {
		m.Content = append(m.Content, wbNode(key), v)
	}
}
func wbRename(m *yaml.Node, old, new string) {
	for i := 0; i < len(m.Content); i += 2 {
		if m.Content[i].Value == old {
			m.Content[i].Value = new
			return
		}
	}
}
func wbRemoveBindings(m *Metadata, remove func(Binding) bool) {
	inputs := m.Inputs[:0]
	for _, in := range m.Inputs {
		bindings := in.Bindings[:0]
		for _, b := range in.Bindings {
			if !remove(b) {
				bindings = append(bindings, b)
			}
		}
		in.Bindings = bindings
		if len(bindings) > 0 {
			inputs = append(inputs, in)
		} else {
			for i := range m.Presets {
				delete(m.Presets[i].Values, in.Name)
			}
		}
	}
	m.Inputs = inputs
}
