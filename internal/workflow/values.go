package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"gopkg.in/yaml.v3"
	"sort"
	"strconv"
	"strings"
)

func ResolveValues(d Document, preset string, provided map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	if !d.Valid() {
		return nil, fmt.Errorf("invalid definition")
	}
	declared := map[string]Input{}
	result := map[string]json.RawMessage{}
	if d.Metadata != nil {
		for _, input := range d.Metadata.Inputs {
			declared[input.Name] = input
			if len(input.Default) > 0 {
				result[input.Name] = input.Default
			}
		}
	}
	if preset != "" {
		found := false
		if d.Metadata != nil {
			for _, p := range d.Metadata.Presets {
				if p.ID == preset {
					found = true
					for k, v := range p.Values {
						result[k] = v
					}
				}
			}
		}
		if !found {
			return nil, fmt.Errorf("unknown preset")
		}
	}
	for name, value := range provided {
		if _, ok := declared[name]; !ok {
			return nil, fmt.Errorf("unknown input %s", name)
		}
		result[name] = value
	}
	for name, input := range declared {
		value, ok := result[name]
		if !ok {
			return nil, fmt.Errorf("required input %s is missing", name)
		}
		if !validValue(input, value) {
			return nil, fmt.Errorf("input %s violates type/enum", name)
		}
		var text string
		if input.Type == "string" {
			if err := json.Unmarshal(value, &text); err != nil {
				return nil, err
			}
			if strings.ContainsAny(text, "\x00$") || strings.Contains(text, "{{") {
				return nil, fmt.Errorf("input %s contains unsupported expansion or NUL", name)
			}
			raw, _ := json.Marshal(text)
			result[name] = raw
		} else if input.Type == "integer" {
			dec := json.NewDecoder(bytes.NewReader(value))
			dec.UseNumber()
			var n json.Number
			if err := dec.Decode(&n); err != nil {
				return nil, err
			}
			i, err := n.Int64()
			if err != nil {
				return nil, err
			}
			result[name] = json.RawMessage(strconv.FormatInt(i, 10))
		} else {
			var b bool
			if err := json.Unmarshal(value, &b); err != nil {
				return nil, err
			}
			result[name] = json.RawMessage(strconv.FormatBool(b))
		}
	}
	return result, nil
}

// ApplyValues creates an independent execution document, leaving base bytes/revision unchanged.
func ApplyValues(d Document, values map[string]json.RawMessage) (Document, error) {
	return ApplyValuesContext(context.Background(), d, values)
}

// ApplyValuesContext batches all environment changes and reparses the document once.
func ApplyValuesContext(ctx context.Context, d Document, values map[string]json.RawMessage) (Document, error) {
	if e := ctx.Err(); e != nil {
		return d, e
	}
	resolved, e := ResolveValues(d, "", values)
	if e != nil {
		return d, e
	}
	if len(resolved) == 0 {
		return d, nil
	}
	if d.View.RawEditOnly {
		return d, fmt.Errorf("this syntax requires raw source editing")
	}
	bindings := map[string]map[string]string{}
	for _, input := range d.Metadata.Inputs {
		if e = ctx.Err(); e != nil {
			return d, e
		}
		value := string(resolved[input.Name])
		if input.Type == "string" {
			if e = json.Unmarshal(resolved[input.Name], &value); e != nil {
				return d, e
			}
		}
		for _, b := range input.Bindings {
			if bindings[b.Process] == nil {
				bindings[b.Process] = map[string]string{}
			}
			bindings[b.Process][b.Key] = value
		}
	}
	updates := map[string][]string{}
	for process, bound := range bindings {
		if e = ctx.Err(); e != nil {
			return d, e
		}
		env := findNode(d.AST, "processes", process, "environment")
		entries := []string{}
		seen := map[string]bool{}
		if env != nil {
			if env.Kind != yaml.SequenceNode {
				return d, fmt.Errorf("environment binding requires an explicit KEY=VALUE sequence")
			}
			for _, v := range env.Content {
				if v.Kind != yaml.ScalarNode || v.Tag != "!!str" {
					return d, fmt.Errorf("environment entries must be strings")
				}
				key, _, ok := strings.Cut(v.Value, "=")
				if !ok || key == "" || seen[key] {
					return d, fmt.Errorf("ambiguous environment binding")
				}
				seen[key] = true
				entry := v.Value
				if value, ok := bound[key]; ok {
					entry = key + "=" + value
				}
				entries = append(entries, entry)
			}
		}
		extra := []string{}
		for key := range bound {
			if !seen[key] {
				extra = append(extra, key)
			}
		}
		sort.Strings(extra)
		for _, key := range extra {
			entries = append(entries, key+"="+bound[key])
		}
		updates[process] = entries
	}
	source, e := environmentSource(ctx, d, updates)
	if e != nil {
		return d, e
	}
	if e = ctx.Err(); e != nil {
		return d, e
	}
	next := rebuild(d, source, d.UIRaw)
	if e = ctx.Err(); e != nil {
		return d, e
	}
	if !next.Valid() {
		return d, fmt.Errorf("environment application produced an invalid definition")
	}
	return next, nil
}

func environmentSource(ctx context.Context, d Document, updates map[string][]string) ([]byte, error) {
	if d.Format == "json" {
		var root, processes map[string]json.RawMessage
		if e := json.Unmarshal(d.Source, &root); e != nil {
			return nil, e
		}
		if e := json.Unmarshal(root["processes"], &processes); e != nil {
			return nil, e
		}
		for name, entries := range updates {
			if e := ctx.Err(); e != nil {
				return nil, e
			}
			var process map[string]json.RawMessage
			if e := json.Unmarshal(processes[name], &process); e != nil {
				return nil, e
			}
			raw, e := json.Marshal(entries)
			if e != nil {
				return nil, e
			}
			process["environment"] = raw
			raw, e = json.Marshal(process)
			if e != nil {
				return nil, e
			}
			processes[name] = raw
		}
		raw, e := json.Marshal(processes)
		if e != nil {
			return nil, e
		}
		root["processes"] = raw
		raw, e = json.Marshal(root)
		return raw, e
	}
	var tree yaml.Node
	if e := yaml.Unmarshal(d.Source, &tree); e != nil {
		return nil, e
	}
	for name, entries := range updates {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		process := findNode(&tree, "processes", name)
		var value yaml.Node
		if e := value.Encode(entries); e != nil {
			return nil, e
		}
		found := false
		for i := 0; i < len(process.Content); i += 2 {
			if process.Content[i].Value == "environment" {
				old := process.Content[i+1]
				value.HeadComment, value.LineComment, value.FootComment = old.HeadComment, old.LineComment, old.FootComment
				for j, entry := range old.Content {
					value.Content[j].HeadComment, value.Content[j].LineComment, value.Content[j].FootComment = entry.HeadComment, entry.LineComment, entry.FootComment
				}
				process.Content[i+1] = &value
				found = true
				break
			}
		}
		if !found {
			process.Content = append(process.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "environment"}, &value)
		}
	}
	return yaml.Marshal(&tree)
}
