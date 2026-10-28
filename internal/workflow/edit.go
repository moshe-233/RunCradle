package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"gopkg.in/yaml.v3"
	"path/filepath"
	"reflect"
	"strings"
)

type EditRequest struct {
	Process string          `json:"process"`
	Field   string          `json:"field"`
	Value   json.RawMessage `json:"value"`
	Remove  bool            `json:"remove,omitempty"`
}

// FromBytes binds exact bytes to logical workspace entries without reading or writing them.
func FromBytes(workspace, entry, uiPath, format string, source, ui []byte) (Document, error) {
	if _, err := Relative(workspace, entry); err != nil {
		return Document{}, err
	}
	if uiPath != "" {
		if _, err := Relative(workspace, uiPath); err != nil {
			return Document{}, err
		}
	} else if ui != nil {
		return Document{}, fmt.Errorf("UI path required when UI bytes are supplied")
	}
	d := ParseDocument(source, format, ui)
	d.Workspace = workspace
	d.Entry = entry
	d.UIPath = uiPath
	return rebuild(d, source, ui), nil
}

func rebuild(d Document, source, ui []byte) Document {
	next := ParseDocument(source, d.Format, ui)
	next.Workspace, next.Entry, next.UIPath = d.Workspace, d.Entry, d.UIPath
	if d.Entry != "" {
		files := map[string][]byte{"format": []byte(d.Format), "native/" + filepath.ToSlash(d.Entry): source}
		if d.UIPath != "" {
			files["ui/"+filepath.ToSlash(d.UIPath)] = ui
		}
		next.Revision = ComputeRevision(files)
	}
	return next
}

func Edit(d Document, req EditRequest) (Document, error) {
	if !d.Valid() || d.View.RawEditOnly {
		return d, fmt.Errorf("this syntax requires raw source editing")
	}
	switch req.Field {
	case "command", "entrypoint", "working_dir", "environment", "depends_on":
	default:
		return d, fmt.Errorf("field requires raw source editing")
	}
	process := findNode(d.AST, "processes", req.Process)
	if process == nil || process.Kind != yaml.MappingNode {
		return d, fmt.Errorf("unknown or unsupported process shape")
	}
	if req.Field == "command" && findNode(process, "entrypoint") != nil || req.Field == "entrypoint" && findNode(process, "command") != nil {
		return d, fmt.Errorf("changing command representation requires raw source editing")
	}
	if old := findNode(process, req.Field); old != nil {
		kind := yaml.ScalarNode
		if req.Field == "entrypoint" || req.Field == "environment" {
			kind = yaml.SequenceNode
		}
		if req.Field == "depends_on" {
			kind = yaml.MappingNode
		}
		if old.Kind != kind || kind == yaml.ScalarNode && old.Tag != "!!str" {
			return d, fmt.Errorf("existing field shape requires raw source editing")
		}
	}
	if !req.Remove {
		var value any
		dec := json.NewDecoder(bytes.NewReader(req.Value))
		dec.UseNumber()
		if err := dec.Decode(&value); err != nil {
			return d, err
		}
		switch req.Field {
		case "command", "working_dir":
			if _, ok := value.(string); !ok {
				return d, fmt.Errorf("field must be text")
			}
		case "entrypoint", "environment":
			a, ok := value.([]any)
			if !ok || req.Field == "entrypoint" && len(a) == 0 {
				return d, fmt.Errorf("field must be a string array")
			}
			for _, v := range a {
				if _, ok = v.(string); !ok {
					return d, fmt.Errorf("field must be a string array")
				}
			}
		case "depends_on":
			if _, ok := value.(map[string]any); !ok {
				return d, fmt.Errorf("dependencies must be a mapping")
			}
		}
		if old := findNode(process, req.Field); old != nil && reflect.DeepEqual(nodeValue(old), value) {
			return d, nil
		}
	}
	var source []byte
	if d.Format == "json" {
		var root, processes, node map[string]json.RawMessage
		if err := json.Unmarshal(d.Source, &root); err != nil {
			return d, err
		}
		if err := json.Unmarshal(root["processes"], &processes); err != nil {
			return d, err
		}
		if err := json.Unmarshal(processes[req.Process], &node); err != nil {
			return d, err
		}
		if req.Remove {
			if _, ok := node[req.Field]; !ok {
				return d, nil
			}
			delete(node, req.Field)
		} else {
			node[req.Field] = req.Value
		}
		raw, err := json.Marshal(node)
		if err != nil {
			return d, err
		}
		processes[req.Process] = raw
		raw, err = json.Marshal(processes)
		if err != nil {
			return d, err
		}
		root["processes"] = raw
		source, err = json.MarshalIndent(root, "", "  ")
		if err != nil {
			return d, err
		}
		source = append(source, '\n')
	} else {
		var tree yaml.Node
		if err := yaml.Unmarshal(d.Source, &tree); err != nil {
			return d, err
		}
		target := findNode(&tree, "processes", req.Process)
		index := -1
		for i := 0; i < len(target.Content); i += 2 {
			if target.Content[i].Value == req.Field {
				index = i
				break
			}
		}
		if req.Remove {
			if index < 0 {
				return d, nil
			}
			target.Content = append(target.Content[:index], target.Content[index+2:]...)
		} else {
			var node yaml.Node
			if err := yaml.Unmarshal(req.Value, &node); err != nil {
				return d, err
			}
			v := node.Content[0]
			if index >= 0 {
				old := target.Content[index+1]
				v.HeadComment, v.LineComment, v.FootComment = old.HeadComment, old.LineComment, old.FootComment
				target.Content[index+1] = v
			} else {
				target.Content = append(target.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: req.Field}, v)
			}
		}
		var err error
		source, err = yaml.Marshal(&tree)
		if err != nil {
			return d, err
		}
	}
	next := rebuild(d, source, d.UIRaw)
	if !next.Valid() {
		messages := []string{}
		for _, issue := range next.Issues {
			if issue.Severity == "error" {
				messages = append(messages, issue.Path+": "+issue.Message)
			}
		}
		return d, fmt.Errorf("edit rejected: %s", strings.Join(messages, "; "))
	}
	return next, nil
}
