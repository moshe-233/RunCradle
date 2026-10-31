package httpapi

import (
	"runcradle/internal/engine"
	"runcradle/internal/protocol"
	"runcradle/internal/runs"
	"runcradle/internal/service"
	"runcradle/internal/workflow"
	"strconv"
	"strings"
)

// OpenAPI is derived from the same wire types used by the handlers. It is
// available offline in the binary and never reads ignored documentation files.
func OpenAPI() map[string]any {
	text := schema{"type": "string"}
	boolean := schema{"type": "boolean"}
	integer := schema{"type": "integer", "minimum": 0}
	draft := strict(wireSchema(protocol.Draft{}))
	field(draft, "format", schema{"type": "string", "enum": []string{"yaml", "json"}})
	field(draft, "source", schema{"type": "string", "description": "Exact UTF-8 native source, at most 1 MiB; preserves int64 numbers and YAML comments."})
	draftRequestSchema := strict(wireSchema(draftRequest{}))
	field(draftRequestSchema, "format", schema{"type": "string", "enum": []string{"yaml", "json"}})
	field(draftRequestSchema, "edit", ref("Edit"))
	field(draftRequestSchema, "workbench_edit", ref("WorkbenchEdit"))
	draftRequestSchema["not"] = schema{"required": []string{"edit", "workbench_edit"}}
	save := strict(wireSchema(saveRequest{}))
	field(save, "format", schema{"type": "string", "enum": []string{"yaml", "json"}})
	save["required"] = []string{"format", "source", "expected_revision"}
	field(save, "expected_revision", schema{"type": "string", "minLength": 1})
	// Save consumes a finished draft; edits are applied by validate first.
	save["description"] = "Save the finished draft with CAS. Apply edits through validate first."
	run := strict(wireSchema(protocol.RunRequest{}))
	field(run, "request_id", schema{"type": "string", "minLength": 1, "maxLength": 128, "description": "At most 128 UTF-8 bytes. Reuse only for identical invocation; never rotate automatically after transport failure."})
	field(run, "revision", schema{"type": "string", "minLength": 1})
	field(run, "workflow_id", schema{"type": "string", "minLength": 1})
	field(run, "document", ref("Draft"))
	field(run, "target_nodes", schema{"type": "array", "minItems": 1, "uniqueItems": true, "items": text})
	field(run, "mode", schema{"type": "string", "enum": []string{"", "task"}, "default": "task"})
	field(run, "timeout_ms", schema{"type": "integer", "minimum": 0, "maximum": 3600000, "default": 300000, "description": "Zero or omitted uses the default; otherwise 1..3600000."})
	run["oneOf"] = []any{schema{"required": []string{"workflow_id"}, "not": schema{"required": []string{"document"}}}, schema{"required": []string{"document"}, "not": schema{"required": []string{"workflow_id"}}}}
	snapshot := wireSchema(engine.Snapshot{})
	field(snapshot, "owned", boolean)
	field(snapshot, "values_text", text)
	document := wireSchema(protocol.Draft{})
	for name, value := range (schema{"revision": text, "valid": boolean, "issues": array(wireSchema(workflow.Issue{})), "display": schema{"description": "WebUI presentation data; integrations use source, revision and issues."}, "workflow": wireSchema(runs.WorkflowRecord{}), "run_supported": boolean, "execution_note": text}) {
		field(document, name, value.(schema))
	}
	document["required"] = []string{"format", "source", "revision", "valid", "issues", "display", "run_supported", "execution_note"}
	edit := strict(wireSchema(workflow.EditRequest{}))
	field(edit, "field", schema{"type": "string", "enum": []string{"command", "entrypoint", "working_dir", "environment", "depends_on"}})
	edit["description"] = "Edit one declared process field while preserving source syntax. command/working_dir use string values; entrypoint/environment use arrays of strings; depends_on uses a dependency object. remove=true removes the field; set value=null when removing. Command representation changes require workbench_edit or raw source."
	workbench := strict(wireSchema(workflow.WorkbenchEdit{}))
	field(workbench, "action", schema{"type": "string", "enum": []string{"add_process", "delete_process", "rename_process", "update_process", "bind_input", "unbind_input", "save_preset", "delete_preset", "update_meta"}})
	workbench["description"] = "Process actions require process; rename_process also needs new_name. bind_input needs input_spec. unbind_input needs input_name/process/env_key. save_preset needs preset_spec, delete_preset needs preset_id. update_meta accepts title/description. All operations produce a draft; persist with a separate CAS save."
	schemas := schema{
		"Protocol": wireSchema(protocol.Describe()), "Problem": wireSchema(protocol.Problem{}),
		"Draft": draft, "ValidateRequest": draftRequestSchema, "SaveRequest": save, "Document": document,
		"Edit": edit, "WorkbenchEdit": workbench,
		"RunRequest": run, "Submission": wireSchema(protocol.Submission{}), "Snapshot": snapshot,
		"Event": wireSchema(engine.Event{}), "Invocation": wireSchema(service.Invocation{}),
		"StopResult": wireSchema(engine.StopResult{}), "WorkflowRecord": wireSchema(runs.WorkflowRecord{}),
		"Health":         object(schema{"status": text, "instance_id": text}, "status", "instance_id"),
		"Capabilities":   object(schema{"engine_available": boolean, "engine": wireSchema(engine.Capabilities{}), "reason": text, "mode": text, "max_active_runs": integer, "active_runs": integer}, "engine_available", "mode"),
		"WorkspaceFiles": wireSchema(workspaceFiles{}),
		"WorkflowPage":   object(schema{"workflows": array(ref("WorkflowRecord")), "has_more": boolean, "next_offset": integer}, "workflows", "has_more", "next_offset"),
		"RunPage":        object(schema{"runs": array(ref("Snapshot")), "has_more": boolean, "next_offset": integer}, "runs", "has_more", "next_offset"),
		"EventPage":      object(schema{"events": array(ref("Event")), "next_seq": integer, "gap": boolean, "error": text}, "events", "next_seq", "gap"),
		"RequestLookup":  object(schema{"run_id": text, "request_id": text, "revision": text, "snapshot": ref("Snapshot")}, "run_id", "request_id", "revision", "snapshot"),
		"ImportRequest":  strict(object(schema{"file": text, "ui": text}, "file")),
		"CopyRequest":    strict(object(schema{"revision": text}, "revision")),
		"StopRequest":    strict(object(schema{"cancellation_code": schema{"type": "integer", "enum": []int{0, 130, 143}, "default": 130}})),
		"PairRequest":    strict(object(schema{"token": text}, "token")),
		"Paired":         object(schema{"status": text}, "status"),
		"OpenAPI":        schema{"type": "object", "description": "OpenAPI 3.1.1 document for this contract version."},
	}
	submissionProblem := wireSchema(protocol.Problem{})
	for name, value := range wireSchema(protocol.Submission{})["properties"].(schema) {
		field(submissionProblem, name, value.(schema))
	}
	schemas["SubmissionProblem"] = submissionProblem
	paths := schema{}
	add := func(method, path, id, input, output string, status string, params ...schema) {
		media := "application/json"
		responseSchema := ref(output)
		if output == "zip" || output == "html" {
			media = "application/zip"
			if output == "html" {
				media = "text/html"
			}
			responseSchema = schema{"type": "string", "format": "binary"}
		}
		op := schema{"operationId": id, "responses": schema{
			status:    schema{"description": "Operation result; acceptance and task success are separate facts.", "content": schema{media: schema{"schema": responseSchema}}},
			"default": schema{"description": "Structured failure. Run submission failures may also include run_id, request_id, revision and accepted; retain these when reconciling.", "content": schema{"application/json": schema{"schema": ref("Problem")}}},
		}}
		if strings.Contains(path, "{id}") {
			params = append(params, parameter("id", "path", text))
		}
		if len(params) > 0 {
			op["parameters"] = params
		}
		if input != "" {
			op["requestBody"] = schema{"required": true, "content": schema{"application/json": schema{"schema": ref(input)}}}
		}
		if paths[path] == nil {
			paths[path] = schema{}
		}
		paths[path].(schema)[strings.ToLower(method)] = op
		problem := ref("Problem")
		if id == "run.submit" {
			problem = ref("SubmissionProblem")
		}
		for _, code := range []int{400, 401, 403, 404, 405, 409, 413, 415, 429, 500} {
			op["responses"].(schema)[strconv.Itoa(code)] = schema{"description": protocol.ProblemFor(code, "").Code, "content": schema{"application/json": schema{"schema": problem}}}
		}
	}
	offset, limit := parameter("offset", "query", integer), parameter("limit", "query", schema{"type": "integer", "minimum": 1, "maximum": 100, "default": 20})
	revision := parameter("revision", "query", text)
	add("GET", "/api/v1/protocol", "protocol.describe", "", "Protocol", "200")
	add("GET", "/api/v1/openapi.json", "protocol.schema", "", "OpenAPI", "200")
	add("GET", "/api/v1/health", "host.health", "", "Health", "200")
	add("GET", "/api/v1/capabilities", "host.capabilities", "", "Capabilities", "200")
	add("GET", "/api/v1/workspace-files", "workspace.files", "", "WorkspaceFiles", "200")
	add("POST", "/api/v1/session", "browser.pair", "PairRequest", "Paired", "200", parameter("Origin", "header", text))
	paths["/api/v1/session"].(schema)["post"].(schema)["security"] = []any{}
	add("POST", "/api/v1/imports", "workflow.import", "ImportRequest", "Document", "200")
	add("POST", "/api/v1/validate", "workflow.validate", "ValidateRequest", "Document", "200")
	add("POST", "/api/v1/render", "workflow.render", "Draft", "html", "200")
	add("GET", "/api/v1/workflows", "workflow.list", "", "WorkflowPage", "200", offset, limit)
	add("POST", "/api/v1/workflows", "workflow.create", "Draft", "Document", "201")
	add("GET", "/api/v1/workflows/{id}", "workflow.get", "", "Document", "200", revision)
	add("PUT", "/api/v1/workflows/{id}", "workflow.save", "SaveRequest", "Document", "200")
	add("POST", "/api/v1/workflows/{id}/copy", "workflow.copy", "CopyRequest", "Document", "201")
	add("GET", "/api/v1/workflows/{id}/export", "workflow.export", "", "zip", "200", revision)
	add("POST", "/api/v1/runs", "run.submit", "RunRequest", "Submission", "202")
	add("GET", "/api/v1/runs", "run.list", "", "RunPage", "200", offset, limit, parameter("workflow_id", "query", text))
	add("GET", "/api/v1/run-requests/{id}", "run.lookup", "", "RequestLookup", "200")
	add("GET", "/api/v1/runs/{id}", "run.inspect", "", "Snapshot", "200")
	add("GET", "/api/v1/runs/{id}/definition", "run.definition", "", "Document", "200")
	add("GET", "/api/v1/runs/{id}/invocation", "run.invocation", "", "Invocation", "200")
	add("POST", "/api/v1/runs/{id}/stop", "run.stop", "StopRequest", "StopResult", "202")
	add("GET", "/api/v1/runs/{id}/events", "run.events", "", "EventPage", "200", limit, parameter("after_seq", "query", integer), parameter("Last-Event-ID", "header", text), parameter("Accept", "header", text))
	events := paths["/api/v1/runs/{id}/events"].(schema)["get"].(schema)
	events["description"] = "Accept: text/event-stream selects SSE; otherwise finite JSON pages. after_seq takes precedence over Last-Event-ID. Persist numbered event IDs; snapshot/final/gap/interrupted are unnumbered. A gap requires reconciliation, not an assumption of complete history. Disconnecting never stops the run. Slow subscribers are disconnected after a bounded write deadline."
	events["responses"].(schema)["200"].(schema)["content"].(schema)["text/event-stream"] = schema{"schema": schema{"type": "string"}, "x-event-schemas": schema{"message": ref("Event"), "snapshot": ref("Snapshot"), "final": ref("Snapshot"), "interrupted": ref("Snapshot"), "gap": object(schema{"history_integrity": text, "reason": text}, "history_integrity")}}
	return schema{"openapi": "3.1.1", "info": schema{"title": "RunCradle local API", "version": protocol.ContractVersion, "description": "Explicit loopback host, native task mode, immutable revisions. API errors are operation failures; task exit, engine exit, cleanup, IO and history remain separate facts. Unknown request fields are rejected; clients must tolerate additive response fields."}, "servers": []any{schema{"url": "/"}}, "security": []any{schema{"localBearer": []string{}}, schema{"browserSession": []string{}}}, "paths": paths, "components": schema{"schemas": schemas, "securitySchemes": schema{"localBearer": schema{"type": "http", "scheme": "bearer", "description": "Read only from the authorized workspace's private connection.json; verify instance_id and X-RunCradle-Instance."}, "browserSession": schema{"type": "apiKey", "in": "cookie", "name": "runcradle_{instance_prefix}", "description": "Actual cookie is runcradle_ plus first 12 instance_id characters, issued by same-origin one-time pairing. Browser writes require matching Origin."}}}}
}
