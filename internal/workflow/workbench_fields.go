package workflow

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"strings"
)

func wbUpdate(p *yaml.Node, r WorkbenchEdit) error {
	switch r.CommandMode {
	case "":
	case "command":
		wbSet(p, "entrypoint", nil)
		wbSet(p, "command", wbNode(r.Command))
	case "entrypoint":
		if len(r.Entrypoint) == 0 {
			return fmt.Errorf("entrypoint must not be empty")
		}
		wbSet(p, "command", nil)
		wbSet(p, "entrypoint", wbNode(r.Entrypoint))
	default:
		return fmt.Errorf("unknown command_mode")
	}
	if r.WorkingDir != nil {
		wbSet(p, "working_dir", wbNode(*r.WorkingDir))
	}
	if r.EnvItems != nil {
		old := findNode(p, "environment")
		if old != nil && old.Kind != yaml.SequenceNode {
			return fmt.Errorf("environment shape requires raw source editing")
		}
		seen := map[string]bool{}
		values := []string{}
		for _, e := range r.EnvItems {
			if e.Key == "" || strings.ContainsAny(e.Key, "=\x00") || seen[e.Key] || strings.ContainsRune(e.Value, 0) {
				return fmt.Errorf("invalid or duplicate environment key")
			}
			seen[e.Key] = true
			values = append(values, e.Key+"="+e.Value)
		}
		n := wbNode(values)
		// Retain comments for unchanged environment keys even after reordering.
		if old != nil {
			for _, v := range n.Content {
				key, _, _ := strings.Cut(v.Value, "=")
				for _, o := range old.Content {
					okey, _, _ := strings.Cut(o.Value, "=")
					if key == okey {
						v.HeadComment, v.LineComment, v.FootComment = o.HeadComment, o.LineComment, o.FootComment
					}
				}
			}
		}
		wbSet(p, "environment", n)
	}
	if r.DependsOn != nil {
		deps := findNode(p, "depends_on")
		if deps == nil {
			deps = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		}
		seen := map[string]bool{}
		for _, dep := range r.DependsOn {
			if dep.From == "" || seen[dep.From] {
				return fmt.Errorf("invalid or duplicate dependency")
			}
			seen[dep.From] = true
			switch dep.Condition {
			case "process_started", "process_completed", "process_completed_successfully", "process_healthy", "process_log_ready":
			default:
				return fmt.Errorf("invalid dependency condition")
			}
			rule := findNode(deps, dep.From)
			if rule == nil {
				rule = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			}
			wbSet(rule, "condition", wbNode(dep.Condition))
			wbSet(deps, dep.From, rule)
		}
		for i := len(deps.Content) - 2; i >= 0; i -= 2 {
			if !seen[deps.Content[i].Value] {
				wbSet(deps, deps.Content[i].Value, nil)
			}
		}
		wbSet(p, "depends_on", deps)
	}
	if r.Restart != nil || r.ExitOnEnd != nil || r.ExitOnSkipped != nil {
		a := findNode(p, "availability")
		if a == nil {
			a = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		}
		if a.Kind != yaml.MappingNode {
			return fmt.Errorf("availability requires raw source editing")
		}
		if r.Restart != nil {
			if *r.Restart != "no" && *r.Restart != "exit_on_failure" {
				return fmt.Errorf("unsupported restart")
			}
			wbSet(a, "restart", wbNode(*r.Restart))
		}
		if r.ExitOnEnd != nil {
			wbSet(a, "exit_on_end", wbNode(*r.ExitOnEnd))
		}
		if r.ExitOnSkipped != nil {
			wbSet(a, "exit_on_skipped", wbNode(*r.ExitOnSkipped))
		}
		wbSet(p, "availability", a)
	}
	return nil
}
