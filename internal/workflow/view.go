package workflow

import (
	"crypto/sha256"
	"fmt"
	"sort"
)

func (d *Document) buildView() {
	processes, ok := d.Native["processes"].(map[string]any)
	if !ok {
		d.problem("structure", "$.processes", "processes must be a mapping", findNode(d.AST, "processes"))
		return
	}
	if len(processes) > 128 {
		d.problem("limit", "$.processes", "at most 128 processes", findNode(d.AST, "processes"))
		return
	}
	external := d.Native["extends"] != nil
	if external {
		d.deferred("$.extends", "external configuration is preserved but not loaded")
	}
	if dynamic(d.Native) {
		d.deferred("$", "templates/environment expansion are not evaluated")
	}
	names := make([]string, 0, len(processes))
	for name := range processes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		path := "$.processes." + name
		n := findNode(d.AST, "processes", name)
		cfg, ok := processes[name].(map[string]any)
		if !ok || name == "" {
			d.problem("structure", path, "process must be a named mapping", n)
			continue
		}
		if cfg["extends"] != nil {
			external = true
			d.deferred(path+".extends", "process extension is not resolved")
		}
		if r, has := cfg["replicas"]; has && fmt.Sprint(r) != "1" {
			d.deferred(path+".replicas", "replicas are not expanded")
		}
		if c, has := cfg["command"]; has {
			if _, ok := c.(string); !ok {
				d.problem("type", path+".command", "command must be a string", findNode(n, "command"))
			}
		}
		if a, has := cfg["entrypoint"]; has {
			seq, ok := a.([]any)
			valid := ok && len(seq) > 0
			for _, v := range seq {
				if _, ok := v.(string); !ok {
					valid = false
				}
			}
			if !valid {
				d.problem("type", path+".entrypoint", "entrypoint must be a nonempty string array", findNode(n, "entrypoint"))
			}
		}
		id := fmt.Sprintf("node-%x", sha256.Sum256([]byte(name)))[:21]
		d.View.Nodes = append(d.View.Nodes, Node{ID: id, Name: name, Label: name, Declaration: cfg})
	}
	for _, name := range names {
		cfg, ok := processes[name].(map[string]any)
		if !ok {
			continue
		}
		raw, has := cfg["depends_on"]
		if !has {
			continue
		}
		deps, ok := raw.(map[string]any)
		if !ok {
			d.problem("type", "$.processes."+name+".depends_on", "depends_on must be a mapping", findNode(d.AST, "processes", name, "depends_on"))
			continue
		}
		sources := make([]string, 0, len(deps))
		for dep := range deps {
			sources = append(sources, dep)
		}
		sort.Strings(sources)
		for _, dep := range sources {
			path := "$.processes." + name + ".depends_on." + dep
			n := findNode(d.AST, "processes", name, "depends_on", dep)
			rule, ok := deps[dep].(map[string]any)
			if !ok {
				d.problem("type", path, "dependency rule must be a mapping", n)
				continue
			}
			condition := "未显式指定"
			if c, has := rule["condition"]; has {
				s, ok := c.(string)
				if !ok {
					d.problem("type", path+".condition", "condition must be a string", n)
					continue
				}
				condition = s
			}
			switch condition {
			case "未显式指定", "process_started", "process_completed", "process_completed_successfully", "process_healthy", "process_log_ready":
			default:
				d.problem("condition", path+".condition", "unknown dependency condition", n)
			}
			target, exists := processes[dep]
			if !exists {
				if external {
					d.deferred(path, "dependency may belong to unresolved extension")
				} else {
					d.problem("missing_dependency", path, "dependency does not exist", n)
				}
				continue
			}
			if condition == "process_healthy" {
				if m, ok := target.(map[string]any); ok && m["readiness_probe"] == nil {
					d.problem("health_dependency", path, "healthy dependency requires readiness_probe", n)
				}
			}
			d.View.Edges = append(d.View.Edges, Edge{From: dep, To: name, Condition: condition})
		}
	}
	order := []string{}
	state := map[string]int{}
	parents := map[string][]string{}
	for _, e := range d.View.Edges {
		parents[e.To] = append(parents[e.To], e.From)
	}
	var visit func(string) bool
	visit = func(n string) bool {
		if state[n] == 1 {
			return false
		}
		if state[n] == 2 {
			return true
		}
		state[n] = 1
		for _, p := range parents[n] {
			if !visit(p) {
				return false
			}
		}
		state[n] = 2
		order = append(order, n)
		return true
	}
	for _, n := range names {
		if !visit(n) {
			d.problem("dependency_cycle", "$.processes", "cyclic dependency graph", findNode(d.AST, "processes"))
			break
		}
	}
	if len(order) == len(d.View.Nodes) {
		indices := map[string]int{}
		for i, n := range order {
			indices[n] = i
		}
		sort.SliceStable(d.View.Nodes, func(i, j int) bool { return indices[d.View.Nodes[i].Name] < indices[d.View.Nodes[j].Name] })
	}

}
