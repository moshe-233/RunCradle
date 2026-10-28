package cli

import (
	"encoding/json"
	"io"
	"runcradle/internal/workflow"
)

type Issue = workflow.Issue
type Result struct {
	EngineExitCode  *int    `json:"engine_exit_code,omitempty"`
	Status          string  `json:"status"`
	Revision        string  `json:"revision,omitempty"`
	NodeCount       int     `json:"node_count"`
	EdgeCount       int     `json:"edge_count"`
	ViewComplete    bool    `json:"view_complete"`
	EngineCheck     string  `json:"engine_check"`
	Output          string  `json:"output,omitempty"`
	Issues          []Issue `json:"issues,omitempty"`
	IssuesTruncated bool    `json:"issues_truncated,omitempty"`
	NextAction      string  `json:"next_action"`
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
func writeResult(out io.Writer, r Result, code int) int {
	if len(r.Issues) > 12 {
		r.Issues = r.Issues[:12]
		r.IssuesTruncated = true
	}
	r.Issues = append([]Issue(nil), r.Issues...)
	for i := range r.Issues {
		old := r.Issues[i]
		r.Issues[i].Message = clip(old.Message, 256)
		r.Issues[i].Path = clip(old.Path, 128)
		if r.Issues[i] != old {
			r.IssuesTruncated = true
		}
	}
	r.Output = clip(r.Output, 256)
	r.NextAction = clip(r.NextAction, 256)
	for {
		raw, err := json.Marshal(r)
		if err != nil {
			return 125
		}
		if len(raw)+1 <= 4096 {
			n, err := out.Write(append(raw, '\n'))
			if err != nil || n != len(raw)+1 {
				return 125
			}
			return code
		}
		r.IssuesTruncated = true
		if len(r.Issues) > 0 {
			r.Issues = r.Issues[:len(r.Issues)-1]
		} else {
			return 125
		}
	}
}
func failure(out io.Writer, code int, kind, message string) int {
	return writeResult(out, Result{Status: "invalid", EngineCheck: "not_requested", Issues: []Issue{{Severity: "error", Code: kind, Message: message}}, NextAction: "fix the reported issue; see runcradle help"}, code)
}
