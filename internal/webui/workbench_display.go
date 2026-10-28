package webui

import (
	"fmt"
	"runcradle/internal/workflow"
	"sort"
	"strings"
)

type envDisplay struct {
	Key        string `json:"key"`
	Value      string `json:"value"`
	BoundInput string `json:"bound_input,omitempty"`
	InputType  string `json:"input_type,omitempty"`
}
type depDisplay struct {
	From      string `json:"from"`
	Condition string `json:"condition"`
	Label     string `json:"label"`
}
type workbenchNode struct {
	Role              string       `json:"role"`
	CommandMode       string       `json:"command_mode"`
	CommandPreview    string       `json:"command_preview"`
	EntrypointItems   []string     `json:"entrypoint_items"`
	WorkingDir        string       `json:"working_dir"`
	Restart           string       `json:"restart"`
	ExitOnEnd         bool         `json:"exit_on_end"`
	ExitOnSkipped     bool         `json:"exit_on_skipped"`
	HasReadinessProbe bool         `json:"has_readiness_probe"`
	HasLivenessProbe  bool         `json:"has_liveness_probe"`
	EnvItems          []envDisplay `json:"env_items"`
	Dependencies      []depDisplay `json:"dependencies"`
	UpstreamIDs       []string     `json:"upstream_ids"`
	DownstreamIDs     []string     `json:"downstream_ids"`
	SuccessExitCodes  []string     `json:"success_exit_codes"`
}

func projectNode(d workflow.Document, n workflow.Node) workbenchNode {
	c := n.Declaration
	w := workbenchNode{Role: "task", CommandMode: "none", Restart: "no", EnvItems: []envDisplay{}, Dependencies: []depDisplay{}, EntrypointItems: []string{}, SuccessExitCodes: []string{"0"}}
	w.HasReadinessProbe = c["readiness_probe"] != nil
	w.HasLivenessProbe = c["liveness_probe"] != nil
	if w.HasReadinessProbe || w.HasLivenessProbe || c["is_daemon"] == true {
		w.Role = "service"
	}
	if v, ok := c["command"].(string); ok {
		w.CommandMode = "command"
		w.CommandPreview = strings.Join(strings.Fields(v), " ")
	}
	if v, ok := c["entrypoint"].([]any); ok {
		w.CommandMode = "entrypoint"
		for _, s := range v {
			w.EntrypointItems = append(w.EntrypointItems, fmt.Sprint(s))
		}
		w.CommandPreview = strings.Join(w.EntrypointItems, " ")
	}
	w.WorkingDir, _ = c["working_dir"].(string)
	if a, ok := c["availability"].(map[string]any); ok {
		if s, ok := a["restart"].(string); ok {
			w.Restart = s
		}
		w.ExitOnEnd, _ = a["exit_on_end"].(bool)
		w.ExitOnSkipped, _ = a["exit_on_skipped"].(bool)
	}
	if a, ok := c["success_exit_codes"].([]any); ok {
		w.SuccessExitCodes = []string{}
		for _, v := range a {
			w.SuccessExitCodes = append(w.SuccessExitCodes, fmt.Sprint(v))
		}
	}
	if env, ok := c["environment"].([]any); ok {
		for _, v := range env {
			s, ok := v.(string)
			if !ok {
				continue
			}
			k, value, _ := strings.Cut(s, "=")
			w.EnvItems = append(w.EnvItems, envDisplay{Key: k, Value: value})
		}
	}
	if d.Metadata != nil {
		for _, in := range d.Metadata.Inputs {
			for _, b := range in.Bindings {
				if b.Process != n.Name {
					continue
				}
				found := false
				for i := range w.EnvItems {
					if w.EnvItems[i].Key == b.Key {
						w.EnvItems[i].BoundInput = in.Name
						w.EnvItems[i].InputType = in.Type
						found = true
					}
				}
				if !found {
					w.EnvItems = append(w.EnvItems, envDisplay{Key: b.Key, BoundInput: in.Name, InputType: in.Type})
				}
			}
		}
	}
	for _, e := range d.View.Edges {
		if e.To == n.Name {
			w.Dependencies = append(w.Dependencies, depDisplay{e.From, e.Condition, conditionLabel(e.Condition)})
		}
	}
	closure := func(reverse bool) []string {
		seen := map[string]bool{n.Name: true}
		queue := []string{n.Name}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			for _, e := range d.View.Edges {
				a, b := e.From, e.To
				if reverse {
					a, b = b, a
				}
				if a == cur && !seen[b] {
					seen[b] = true
					queue = append(queue, b)
				}
			}
		}
		ids := []string{}
		for _, v := range d.View.Nodes {
			if v.Name != n.Name && seen[v.Name] {
				ids = append(ids, v.ID)
			}
		}
		sort.Strings(ids)
		return ids
	}
	w.UpstreamIDs = closure(true)
	w.DownstreamIDs = closure(false)
	return w
}
