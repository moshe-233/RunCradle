package workflow

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

var metadataFields = map[string]map[string]string{
	"metadata": {"version": "integer", "title": "string", "description": "string", "nodes": "{}node", "inputs": "[]input", "presets": "[]preset"},
	"node":     {"label": "string"},
	"input":    {"name": "string", "label": "string", "type": "string", "default": "value", "enum": "[]value", "bindings": "[]binding"},
	"binding":  {"process": "string", "field": "string", "key": "string"},
	"preset":   {"id": "string", "label": "string", "values": "{}value"},
}

// Check exact JSON member names and shapes before encoding/json's case folding.
func (d *Document) metadataShape(n *yaml.Node, shape, path string) {
	bad := func(message string) { d.problem("metadata", shortPath(path), message, n) }
	if shape == "value" {
		return // Input-specific type and enum validation follows.
	}
	if shape == "string" || shape == "integer" {
		tag := "!!str"
		if shape == "integer" {
			tag = "!!int"
		}
		if n.Kind != yaml.ScalarNode || n.Tag != tag {
			bad("expected " + shape)
		}
		return
	}
	if strings.HasPrefix(shape, "[]") {
		if n.Kind != yaml.SequenceNode {
			bad("expected an array")
			return
		}
		for i, item := range n.Content {
			d.metadataShape(item, shape[2:], fmt.Sprintf("%s[%d]", path, i))
		}
		return
	}
	if n.Kind != yaml.MappingNode {
		bad("expected an object")
		return
	}
	for i := 0; i < len(n.Content); i += 2 {
		key, item := n.Content[i], n.Content[i+1]
		next, ok := metadataFields[shape][key.Value]
		if strings.HasPrefix(shape, "{}") {
			next, ok = shape[2:], true
		}
		if !ok {
			d.problem("metadata", shortPath(path+"."+key.Value), "unknown UI member; names are case-sensitive", key)
			continue
		}
		d.metadataShape(item, next, shortPath(path+"."+key.Value))
	}
}
