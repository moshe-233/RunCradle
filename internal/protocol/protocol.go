// Package protocol owns the transport contract, independently of HTTP handlers.
// It is an internal implementation package; integrations consume CLI/HTTP/OpenAPI.
package protocol

import (
	"encoding/json"
	"runcradle/internal/buildinfo"
)

const (
	APIVersion       = "v1"
	ContractVersion  = "1.0.0"
	MaxRequestBytes  = 8 << 20
	MaxResponseBytes = 32 << 20
	MaxCLIBytes      = 4096
)

type Draft struct {
	Format   string  `json:"format"`
	Source   string  `json:"source"`
	UISource *string `json:"ui_source,omitempty"`
	Entry    string  `json:"entry,omitempty"`
	UIPath   string  `json:"ui_path,omitempty"`
}

type DeleteWorkflowRequest struct {
	ExpectedRevision string `json:"expected_revision"`
}

type RunRequest struct {
	WorkflowID string                     `json:"workflow_id,omitempty"`
	Revision   string                     `json:"revision"`
	Document   *Draft                     `json:"document,omitempty"`
	RequestID  string                     `json:"request_id"`
	Targets    []string                   `json:"target_nodes"`
	Mode       string                     `json:"mode,omitempty"`
	TimeoutMS  int64                      `json:"timeout_ms,omitempty"`
	Preset     string                     `json:"preset_id,omitempty"`
	Values     map[string]json.RawMessage `json:"values,omitempty"`
	Retain     bool                       `json:"retain_evidence,omitempty"`
}

type Submission struct {
	RunID     string `json:"run_id"`
	RequestID string `json:"request_id"`
	Revision  string `json:"revision"`
	Accepted  bool   `json:"accepted"`
}

type Problem struct {
	Message    string `json:"error"`
	Status     int    `json:"status"`
	Code       string `json:"code"`
	NextAction string `json:"next_action"`
}

// ProblemFor preserves HTTP classification without asking clients to parse prose.
// A transport error is never evidence that a submitted operation did not run.
func ProblemFor(status int, message string) Problem {
	code, next := "internal_error", "inspect the original request before retrying a write"
	switch status {
	case 400:
		code, next = "invalid_request", "correct the request using the protocol schema"
	case 401:
		code, next = "unauthorized", "rediscover the live workspace host and its private credentials"
	case 403:
		code, next = "forbidden", "use the authorized workspace host and same-origin browser session"
	case 404:
		code, next = "not_found", "check the resource identity and supported API routes"
	case 405:
		code, next = "method_not_allowed", "use a method from the Allow response header"
	case 409:
		code, next = "conflict", "read the current revision or original request before intentional retry"
	case 413:
		code, next = "budget_exceeded", "reduce the request or use a smaller response page"
	case 415:
		code, next = "unsupported_media_type", "send one application/json object"
	case 429:
		code, next = "capacity_exceeded", "inspect the original request; a recorded not_submitted request needs a new ID after capacity is available"
	case 503:
		code, next = "host_unavailable", "start or rediscover the explicit workspace host"
	case 504:
		code, next = "deadline_exceeded", "inspect the original request; a client deadline does not stop an accepted run"
	}
	return Problem{message, status, code, next}
}

type Limits struct {
	RequestBytes        int `json:"request_bytes"`
	ResponseBytes       int `json:"response_bytes"`
	CLIBytes            int `json:"cli_bytes"`
	PageDefault         int `json:"page_default"`
	PageMax             int `json:"page_max"`
	ActiveRuns          int `json:"active_runs"`
	SSEGlobal           int `json:"sse_global"`
	SSEPerRun           int `json:"sse_per_run"`
	RunTimeoutDefaultMS int `json:"run_timeout_default_ms"`
	RunTimeoutMaxMS     int `json:"run_timeout_max_ms"`
}

type Description struct {
	ProductVersion  string   `json:"product_version"`
	APIVersion      string   `json:"api_version"`
	ContractVersion string   `json:"contract_version"`
	Transports      []string `json:"transports"`
	OpenAPI         string   `json:"openapi"`
	CredentialFile  string   `json:"credential_file"`
	Features        []string `json:"features"`
	Limits          Limits   `json:"limits"`
	NextAction      string   `json:"next_action"`
}

func Describe() Description {
	return Description{
		ProductVersion: buildinfo.Version,
		APIVersion:     APIVersion, ContractVersion: ContractVersion,
		Transports: []string{"cli-json", "http-json", "sse"},
		OpenAPI:    "/api/v1/openapi.json", CredentialFile: ".runcradle/server/connection.json",
		Features:   []string{"workflow_library", "cas_save", "workflow_delete", "structured_edit", "idempotent_run", "detached_submit", "event_resume", "offline_inspect", "explicit_stop"},
		Limits:     Limits{MaxRequestBytes, MaxResponseBytes, MaxCLIBytes, 20, 100, 4, 32, 8, 300000, 3600000},
		NextAction: "use api schema for the complete contract; start serve explicitly for online operations",
	}
}
