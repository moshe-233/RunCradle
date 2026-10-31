package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

func ParseDocument(source []byte, format string, ui []byte) Document {
	d := Document{Source: bytes.Clone(source), UIRaw: bytes.Clone(ui), Format: format, View: View{Title: "工作流", Complete: true, Nodes: []Node{}, Edges: []Edge{}}}
	files := map[string][]byte{"source." + format: source}
	if ui != nil {
		files["ui.json"] = ui
	}
	d.Revision = ComputeRevision(files)
	if len(source) > MaxSource {
		d.problem("limit", "$", "native file exceeds 1 MiB", nil)
		return d
	}
	if !utf8.Valid(source) {
		d.problem("encoding", "$", "native input must be UTF-8", nil)
		return d
	}
	if format != "yaml" && format != "json" {
		d.problem("format", "$", "expected yaml or json", nil)
		return d
	}
	if format == "json" && !json.Valid(source) {
		d.jsonProblem(source, "$")
		return d
	}
	d.AST = d.parseTree(source, "$")
	if !d.Valid() {
		return d
	}
	if d.AST == nil || len(d.AST.Content) == 0 {
		d.problem("structure", "$", "expected a configuration mapping", nil)
		return d
	}
	native, ok := nodeValue(d.AST.Content[0]).(map[string]any)
	if !ok {
		d.problem("structure", "$", "expected a configuration mapping", d.AST.Content[0])
		return d
	}
	d.Native = native
	if native["processes"] == nil && fmt.Sprint(native["version"]) == "2" {
		if _, legacy := native["tasks"].(map[string]any); legacy {
			d.problem("legacy_v2", "$.tasks", "legacy RuntimeHealer v2 policy: explicitly map service/task to native processes; preserve old state and use its original CLI to query it; automatic migration is not supported", findNode(d.AST, "tasks"))
			return d
		}
	}
	d.buildView()
	if ui != nil {
		d.parseMetadata(ui)
	}
	return d
}
func (d *Document) parseTree(raw []byte, path string) *yaml.Node {
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	var root, extra yaml.Node
	if err := dec.Decode(&root); err != nil {
		d.problem("syntax", path, err.Error(), nil)
		return nil
	}
	if err := dec.Decode(&extra); err != io.EOF {
		d.problem("syntax", path, "only one document is allowed", &extra)
		return nil
	}
	visits, expandedBytes := 0, 0
	limited := false
	var walk func(*yaml.Node, int, map[*yaml.Node]bool, string)
	walk = func(n *yaml.Node, depth int, stack map[*yaml.Node]bool, p string) {
		if limited {
			return
		}
		visits++
		if n.Kind == yaml.ScalarNode {
			expandedBytes += len(n.Value)
		}
		if visits > 100000 || depth > 64 || expandedBytes > MaxExpanded {
			limited = true
			d.problem("limit", p, "AST depth/alias node or 8 MiB expanded byte budget exceeded", n)
			return
		}
		if stack[n] {
			d.problem("alias_cycle", p, "cyclic YAML alias", n)
			return
		}
		stack[n] = true
		defer delete(stack, n)
		if n.Kind == yaml.AliasNode {
			d.View.RawEditOnly = true
			walk(n.Alias, depth+1, stack, p)
			return
		}
		if n.Kind == yaml.MappingNode {
			keys := map[string]bool{}
			for i := 0; i < len(n.Content); i += 2 {
				k, v := n.Content[i], n.Content[i+1]
				if k.Kind != yaml.ScalarNode || (k.Tag != "!!str" && k.Tag != "!!merge") {
					d.problem("key", p, "mapping keys must be strings", k)
					continue
				}
				if keys[k.Value] {
					d.problem("duplicate_key", shortPath(p+"."+k.Value), "duplicate mapping key", k)
				}
				keys[k.Value] = true
				if isMerge(k) {
					d.View.RawEditOnly = true
					target := v
					for target.Kind == yaml.AliasNode && target.Alias != target {
						target = target.Alias
					}
					valid := target.Kind == yaml.MappingNode
					if target.Kind == yaml.SequenceNode {
						valid = true
						for _, entry := range target.Content {
							if entry.Kind == yaml.AliasNode {
								entry = entry.Alias
							}
							if entry.Kind != yaml.MappingNode {
								valid = false
							}
						}
					}
					if !valid {
						d.problem("merge", p, "merge must reference a mapping or sequence of mappings", v)
					}
				}
				walk(k, depth+1, stack, p)
				walk(v, depth+1, stack, shortPath(p+"."+k.Value))
				if limited {
					return
				}
			}
		} else {
			for i, c := range n.Content {
				walk(c, depth+1, stack, shortPath(fmt.Sprintf("%s[%d]", p, i)))
				if limited {
					return
				}
			}
		}
	}
	walk(&root, 0, map[*yaml.Node]bool{}, path)
	return &root
}
func isMerge(n *yaml.Node) bool { return n.Kind == yaml.ScalarNode && n.Tag == "!!merge" }
func nodeValue(n *yaml.Node) any {
	if n.Kind == yaml.AliasNode {
		return nodeValue(n.Alias)
	}
	switch n.Kind {
	case yaml.MappingNode:
		m := map[string]any{}
		// Merge keys use first-map-wins, then explicit values override.
		for i := 0; i < len(n.Content); i += 2 {
			if isMerge(n.Content[i]) {
				v := nodeValue(n.Content[i+1])
				maps := []any{v}
				if seq, ok := v.([]any); ok {
					maps = seq
				}
				for _, item := range maps {
					if base, ok := item.(map[string]any); ok {
						for k, v := range base {
							if _, has := m[k]; !has {
								m[k] = v
							}
						}
					}
				}
			}
		}
		for i := 0; i < len(n.Content); i += 2 {
			if !isMerge(n.Content[i]) {
				m[n.Content[i].Value] = nodeValue(n.Content[i+1])
			}
		}
		return m
	case yaml.SequenceNode:
		a := make([]any, len(n.Content))
		for i, c := range n.Content {
			a[i] = nodeValue(c)
		}
		return a
	default:
		if n.Tag == "!!str" {
			return n.Value
		}
		if (n.Tag == "!!int" || n.Tag == "!!float") && json.Valid([]byte(n.Value)) {
			return json.Number(n.Value)
		}
		var v any
		if err := n.Decode(&v); err == nil {
			if f, ok := v.(float64); ok && (math.IsNaN(f) || math.IsInf(f, 0)) {
				return n.Value
			}
			return v
		}
		return n.Value
	}
}
func findNode(n *yaml.Node, path ...string) *yaml.Node {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}
	for _, k := range path {
		if n.Kind == yaml.AliasNode {
			n = n.Alias
		}
		if n.Kind != yaml.MappingNode {
			return nil
		}
		var found *yaml.Node
		for i := 0; i < len(n.Content); i += 2 {
			if n.Content[i].Value == k {
				found = n.Content[i+1]
				break
			}
		}
		if found == nil {
			return nil
		}
		n = found
	}
	return n
}
func dynamic(v any) bool {
	switch x := v.(type) {
	case string:
		return strings.Contains(x, "$") || strings.Contains(x, "{{")
	case map[string]any:
		for k, v := range x {
			if dynamic(k) || dynamic(v) {
				return true
			}
		}
	case []any:
		for _, v := range x {
			if dynamic(v) {
				return true
			}
		}
	}
	return false
}

func shortPath(s string) string {
	r := []rune(s)
	if len(r) > 256 {
		return string(r[:256]) + "…"
	}
	return s
}

func (d *Document) jsonProblem(raw []byte, path string) {
	var value any
	err := json.Unmarshal(raw, &value)
	issue := Issue{Severity: "error", Code: "syntax", Path: path, Message: "invalid JSON"}
	if err != nil {
		issue.Message = err.Error()
		var syntax *json.SyntaxError
		if errors.As(err, &syntax) {
			offset := int(syntax.Offset)
			if strings.Contains(err.Error(), "unexpected end") {
				offset = len(raw) + 1
			}
			if offset < 1 {
				offset = 1
			}
			if offset > len(raw)+1 {
				offset = len(raw) + 1
			}
			prefix := raw[:offset-1]
			issue.Line = 1 + bytes.Count(prefix, []byte("\n"))
			last := bytes.LastIndexByte(prefix, '\n')
			issue.Column = utf8.RuneCount(prefix[last+1:]) + 1
		}
	}
	d.Issues = append(d.Issues, issue)
}
