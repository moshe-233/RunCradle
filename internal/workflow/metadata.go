package workflow

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"unicode/utf8"
)

type Binding struct {
	Process string `json:"process"`
	Field   string `json:"field"`
	Key     string `json:"key"`
}
type Input struct {
	Name     string            `json:"name"`
	Label    string            `json:"label"`
	Type     string            `json:"type"`
	Default  json.RawMessage   `json:"default,omitempty"`
	Enum     []json.RawMessage `json:"enum,omitempty"`
	Bindings []Binding         `json:"bindings"`
}
type Preset struct {
	ID     string                     `json:"id"`
	Label  string                     `json:"label"`
	Values map[string]json.RawMessage `json:"values"`
}
type Metadata struct {
	Version     int    `json:"version"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Nodes       map[string]struct {
		Label string `json:"label"`
	} `json:"nodes,omitempty"`
	Inputs  []Input  `json:"inputs,omitempty"`
	Presets []Preset `json:"presets,omitempty"`
}

func (d *Document) parseMetadata(raw []byte) {
	if len(raw) > MaxUI {
		d.problem("limit", "ui", "UI file exceeds 256 KiB", nil)
		return
	}
	if !utf8.Valid(raw) {
		d.problem("encoding", "ui", "UI input must be UTF-8", nil)
		return
	}
	if !json.Valid(raw) {
		d.jsonProblem(raw, "ui")
		return
	}
	tree := d.parseTree(raw, "ui")
	if !d.Valid() {
		return
	}
	d.metadataShape(tree.Content[0], "metadata", "ui")
	if !d.Valid() {
		return
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var meta Metadata
	if err := dec.Decode(&meta); err != nil {
		d.problem("metadata", "ui", err.Error(), nil)
		return
	}
	d.Metadata = &meta
	bad := func(path, msg string) { d.problem("metadata", path, msg, nil) }
	if meta.Version != 1 {
		bad("ui.version", "expected UI version 1")
	}
	nodes := map[string]bool{}
	for _, n := range d.View.Nodes {
		nodes[n.Name] = true
	}
	for name := range meta.Nodes {
		if !nodes[name] {
			bad("ui.nodes."+name, "unknown process")
		}
	}
	inputs := map[string]Input{}
	bound := map[string]bool{}
	for _, in := range meta.Inputs {
		path := "ui.inputs." + in.Name
		if in.Name == "" || inputs[in.Name].Name != "" {
			bad(path, "input names must be nonempty and unique")
		}
		inputs[in.Name] = in
		if in.Type != "string" && in.Type != "integer" && in.Type != "boolean" {
			bad(path, "expected string, integer or boolean")
		}
		if len(in.Default) > 0 && !validValue(in, in.Default) {
			bad(path+".default", "default violates type/enum")
		}
		for _, v := range in.Enum {
			if !typed(in.Type, v) {
				bad(path+".enum", "enum value violates type")
			}
		}
		if len(in.Bindings) == 0 {
			bad(path+".bindings", "at least one binding is required")
		}
		for _, b := range in.Bindings {
			key := b.Process + "\x00" + b.Key
			if !nodes[b.Process] || b.Field != "environment" || b.Key == "" || strings.ContainsAny(b.Key, "=\x00") {
				bad(path+".bindings", "binding must name a declared process and environment key")
			}
			if bound[key] {
				bad(path+".bindings", "duplicate environment binding")
			}
			bound[key] = true
		}
	}
	presets := map[string]bool{}
	for _, p := range meta.Presets {
		path := "ui.presets." + p.ID
		if p.ID == "" || presets[p.ID] {
			bad(path, "preset IDs must be nonempty and unique")
		}
		presets[p.ID] = true
		for name, v := range p.Values {
			in, ok := inputs[name]
			if !ok || !validValue(in, v) {
				bad(path+".values."+name, "unknown input or invalid preset value")
			}
		}
	}
	if meta.Title != "" {
		d.View.Title = meta.Title
	}
	d.View.Description = meta.Description
	for i := range d.View.Nodes {
		if label, ok := meta.Nodes[d.View.Nodes[i].Name]; ok && label.Label != "" {
			d.View.Nodes[i].Label = label.Label
		}
	}
}
func typed(kind string, raw []byte) bool {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil {
		return false
	}
	switch kind {
	case "string":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "integer":
		n, ok := v.(json.Number)
		if !ok {
			return false
		}
		_, err := n.Int64()
		return err == nil
	}
	return false
}
func validValue(in Input, raw []byte) bool {
	if !typed(in.Type, raw) {
		return false
	}
	if len(in.Enum) == 0 {
		return true
	}
	value := func(raw []byte) any {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var v any
		_ = dec.Decode(&v)
		if in.Type == "integer" {
			if n, ok := v.(json.Number); ok {
				i, _ := n.Int64()
				return i
			}
		}
		return v
	}
	actual := value(raw)
	for _, v := range in.Enum {
		if reflect.DeepEqual(actual, value(v)) {
			return true
		}
	}
	return false
}
