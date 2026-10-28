package engine

import (
	"fmt"
	"runcradle/internal/workflow"
)

func AcceptsExitCode(code int, declaration map[string]any) bool {
	if code == 0 {
		return true
	}
	if values, ok := declaration["success_exit_codes"].([]any); ok {
		for _, value := range values {
			if fmt.Sprint(value) == fmt.Sprint(code) {
				return true
			}
		}
	}
	return false
}

// TargetResult interprets observed exits only; it never schedules work.
func TargetResult(d workflow.Document, targets []string, nodes []Node) string {
	if len(targets) == 0 {
		return "unknown"
	}
	declarations, _ := d.Native["processes"].(map[string]any)
	byName := map[string]Node{}
	for _, node := range nodes {
		byName[node.Name] = node
	}
	unknown := false
	for _, target := range targets {
		node, found := byName[target]
		declaration, valid := declarations[target].(map[string]any)
		if !found || !valid || node.ExitCode == nil {
			unknown = true
		} else if node.Signal != nil || !AcceptsExitCode(*node.ExitCode, declaration) {
			return "failed"
		}
	}
	if unknown {
		return "unknown"
	}
	return "succeeded"
}
