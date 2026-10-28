package engine

import (
	"context"
	"encoding/json"
	"runcradle/internal/workflow"
	"time"
)

type Engine interface {
	Capabilities(context.Context) (Capabilities, error)
	Validate(context.Context, WorkflowSource) (Validation, error)
	Start(context.Context, StartRequest) (RunHandle, error)
	Snapshot(context.Context, RunHandle) (Snapshot, error)
	Subscribe(context.Context, RunHandle) (<-chan Event, error)
	Stop(context.Context, RunHandle) (StopResult, error)
}
type Capabilities struct {
	EngineKind    string             `json:"engine_kind"`
	EngineVersion string             `json:"engine_version"`
	SchemaVersion int                `json:"schema_version"`
	Features      map[string]Feature `json:"features"`
}
type Feature struct {
	Observable bool `json:"observable"`
	Editable   bool `json:"editable"`
	Executable bool `json:"executable"`
}
type WorkflowSource struct {
	Managed     bool
	WorkflowID  string
	Revision    string
	WorkspaceID string
	Document    workflow.Document
}
type Validation struct {
	Valid  bool             `json:"valid"`
	Issues []workflow.Issue `json:"issues,omitempty"`
}

// StartRejection is a known configuration/dependency rejection before any instance starts.
// Other startup errors do not carry this proof and retain unknown cleanup.
type StartRejection struct{ Cause error }

func (e *StartRejection) Error() string { return e.Cause.Error() }
func (e *StartRejection) Unwrap() error { return e.Cause }

type StartRequest struct {
	Source           WorkflowSource
	RunID            string
	Mode             string
	Targets          []string
	Values           map[string]json.RawMessage
	InvocationDigest string
	RequestID        string
	Deadline         time.Time
}
type RunHandle struct {
	RunID            string `json:"run_id"`
	EngineKind       string `json:"engine_kind"`
	EngineInstanceID string `json:"engine_instance_id"`
}
type Node struct {
	Signal            *int        `json:"signal,omitempty"`
	NativeIncarnation string      `json:"upstream_incarnation,omitempty"`
	NativeExitCode    *int        `json:"upstream_node_exit_code,omitempty"`
	ID                string      `json:"id"`
	Name              string      `json:"name"`
	State             string      `json:"state"`
	RawState          string      `json:"raw_state,omitempty"`
	Health            string      `json:"health"`
	ExitCode          *int        `json:"exit_code"`
	Source            string      `json:"source"`
	Incarnation       string      `json:"incarnation,omitempty"`
	StartedAt         *time.Time  `json:"started_at,omitempty"`
	EndedAt           *time.Time  `json:"ended_at,omitempty"`
	Failure           string      `json:"failure,omitempty"`
	Diagnostic        *Diagnostic `json:"diagnostic,omitempty"`
}
type Diagnostic struct {
	Status    string     `json:"status"`
	Source    string     `json:"source,omitempty"`
	Text      string     `json:"text,omitempty"`
	Truncated bool       `json:"truncated,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}
type Snapshot struct {
	OperationError   string                     `json:"operation_error,omitempty"`
	Values           map[string]json.RawMessage `json:"values,omitempty"`
	StopObservation  string                     `json:"stop_observation,omitempty"`
	EngineSignal     int                        `json:"engine_signal,omitempty"`
	WorkflowID       string                     `json:"workflow_id,omitempty"`
	WorkspaceID      string                     `json:"workspace_id,omitempty"`
	CancellationCode int                        `json:"cancellation_code,omitempty"`
	EngineVersion    string                     `json:"engine_version,omitempty"`
	EngineSHA256     string                     `json:"engine_binary_sha256,omitempty"`
	ExecutionDigest  string                     `json:"execution_digest,omitempty"`
	InvocationDigest string                     `json:"invocation_digest,omitempty"`
	Version          int                        `json:"schema_version"`
	RunID            string                     `json:"run_id"`
	Revision         string                     `json:"revision"`
	EngineInstanceID string                     `json:"engine_instance_id"`
	Nodes            []Node                     `json:"nodes"`
	Targets          []string                   `json:"target_nodes"`
	EngineExitCode   *int                       `json:"engine_exit_code"`
	Final            bool                       `json:"final"`
	StopRequested    bool                       `json:"stop_requested"`
	Reason           string                     `json:"reason"`
	TaskResult       string                     `json:"task_result"`
	Cleanup          string                     `json:"cleanup"`
	CleanupScope     string                     `json:"cleanup_scope"`
	HistoryIntegrity string                     `json:"history_integrity"`
	IOStatus         string                     `json:"io_status"`
	StartedAt        time.Time                  `json:"started_at"`
	EndedAt          *time.Time                 `json:"ended_at,omitempty"`
	ResidualPath     string                     `json:"residual_path,omitempty"`
}
type Event struct {
	RunID      string    `json:"run_id"`
	Seq        uint64    `json:"seq"`
	Kind       string    `json:"kind"`
	ObservedAt time.Time `json:"observed_at"`
	Source     string    `json:"source"`
	Node       *Node     `json:"node,omitempty"`
}
type StopResult struct {
	Requested bool   `json:"requested"`
	Terminal  bool   `json:"terminal"`
	Cleanup   string `json:"cleanup"`
	Reason    string `json:"reason"`
}
