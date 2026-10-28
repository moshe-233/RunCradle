package workflow

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func wbApply(t *testing.T, d Document, r WorkbenchEdit) Document {
	t.Helper()
	n, e := ApplyWorkbenchEdit(d, r)
	if e != nil {
		t.Fatal(e)
	}
	if !n.Valid() {
		t.Fatal(n.Issues)
	}
	return n
}
func TestWorkbenchLifecycleAndAtomicity(t *testing.T) {
	d, _ := FromBytes(t.TempDir(), "process-compose.yaml", "", "yaml", []byte("# keep\nprocesses:\n  a:\n    command: echo old\n    custom: 9007199254740993\n"), nil)
	original := string(d.Source)
	d = wbApply(t, d, WorkbenchEdit{Action: "add_process", Process: "b", Upstream: "a"})
	d = wbApply(t, d, WorkbenchEdit{Action: "update_process", Process: "a", EnvItems: []EnvEntry{{"N", "1"}}, CommandMode: "entrypoint", Entrypoint: []string{"echo", "hello"}})
	d = wbApply(t, d, WorkbenchEdit{Action: "bind_input", InputSpec: &Input{Name: "n", Type: "integer", Default: json.RawMessage(`9223372036854775807`), Bindings: []Binding{{Process: "a", Field: "environment", Key: "N"}}}})
	if d.UIPath != "ui.json" {
		t.Fatal("UI path", d.UIPath)
	}
	d = wbApply(t, d, WorkbenchEdit{Action: "save_preset", PresetSpec: &Preset{ID: "p", Values: map[string]json.RawMessage{"n": json.RawMessage(`9223372036854775807`)}}})
	before := d
	revision := d.Revision
	bad, e := ApplyWorkbenchEdit(d, WorkbenchEdit{Action: "rename_process", Process: "a", NewName: "b"})
	if e == nil || !bytes.Equal(bad.Source, before.Source) || d.Revision != revision {
		t.Fatal("not atomic")
	}
	d = wbApply(t, d, WorkbenchEdit{Action: "rename_process", Process: "a", NewName: "renamed"})
	if d.Metadata.Inputs[0].Bindings[0].Process != "renamed" || d.View.Edges[0].From != "renamed" {
		t.Fatal("rename references")
	}
	d = wbApply(t, d, WorkbenchEdit{Action: "delete_process", Process: "renamed"})
	if len(d.Metadata.Inputs) != 0 || len(d.Metadata.Presets) != 1 || len(d.Metadata.Presets[0].Values) != 0 || len(d.View.Edges) != 0 {
		t.Fatal("dangling references", string(d.UIRaw))
	}
	if !strings.Contains(original, "# keep") || !strings.Contains(string(before.Source), "9007199254740993") || !strings.Contains(string(before.Source), "# keep") {
		t.Fatal("fidelity")
	}
}
func TestWorkbenchClearRollbackAndShared(t *testing.T) {
	d := valueDocument()
	cleared := wbApply(t, d, WorkbenchEdit{Action: "update_process", Process: "a", EnvItems: []EnvEntry{}, DependsOn: []DependencyEntry{}})
	if strings.Contains(string(cleared.Source), "SCOPE=old") {
		t.Fatal("empty collection ignored")
	}
	for _, r := range []WorkbenchEdit{{Action: "update_process", Process: "a", DependsOn: []DependencyEntry{{"missing", "process_started"}}}, {Action: "update_process", Process: "a", EnvItems: []EnvEntry{{"X=Y", "1"}}}, {Action: "bind_input", InputSpec: &Input{Name: "bad", Type: "integer", Default: json.RawMessage(`9223372036854775808`), Bindings: []Binding{{"a", "environment", "X"}}}}} {
		src, ui := string(d.Source), string(d.UIRaw)
		_, e := ApplyWorkbenchEdit(d, r)
		if e == nil || src != string(d.Source) || ui != string(d.UIRaw) {
			t.Fatal("invalid edit mutated input", r, e)
		}
	}
	shared := ParseDocument([]byte("processes: {a: &n {command: echo ok}, b: *n}"), "yaml", nil)
	if _, e := ApplyWorkbenchEdit(shared, WorkbenchEdit{Action: "delete_process", Process: "a"}); e == nil {
		t.Fatal("shared edit accepted")
	}
	title := "Shared"
	shared = wbApply(t, shared, WorkbenchEdit{Action: "update_meta", Title: &title})
	if shared.Metadata.Title != title {
		t.Fatal("metadata")
	}
}
func TestWorkbenchJSONPrecisionAndUnbind(t *testing.T) {
	d := ParseDocument([]byte(`{"processes":{"a":{"command":"echo a","x":0.1234567890123456789},"b":{"command":"echo b"}},"big":9007199254740993}`), "json", nil)
	d = wbApply(t, d, WorkbenchEdit{Action: "update_process", Process: "a", CommandMode: "command", Command: "echo changed"})
	if !strings.Contains(string(d.Source), "0.1234567890123456789") || !strings.Contains(string(d.Source), "9007199254740993") {
		t.Fatal(string(d.Source))
	}
	d = valueDocument()
	d = wbApply(t, d, WorkbenchEdit{Action: "unbind_input", InputName: "scope", Process: "a", EnvKey: "SCOPE"})
	if len(d.Metadata.Inputs) != 1 || len(d.Metadata.Presets[0].Values) != 0 {
		t.Fatal("unbind references")
	}
}

func TestWorkbenchPreservesNestedFieldsAndRollbackObjects(t *testing.T) {
	d := ParseDocument([]byte("# root\nprocesses:\n  a:\n    command: echo a\n    environment: [A=one]\n    availability: {restart: no, custom: 9007199254740993}\n  b:\n    command: echo b\n    depends_on:\n      a: # dependency comment\n        condition: process_started\n        custom: 9007199254740993\n"), "yaml", nil)
	originalNative, _ := json.Marshal(d.Native)
	originalMeta, _ := json.Marshal(d.Metadata)
	originalView, _ := json.Marshal(d.View)
	_, err := ApplyWorkbenchEdit(d, WorkbenchEdit{Action: "update_process", Process: "a", DependsOn: []DependencyEntry{{From: "b", Condition: "process_started"}}})
	if err == nil {
		t.Fatal("cycle accepted")
	}
	native, _ := json.Marshal(d.Native)
	meta, _ := json.Marshal(d.Metadata)
	view, _ := json.Marshal(d.View)
	if !bytes.Equal(native, originalNative) || !bytes.Equal(meta, originalMeta) || !bytes.Equal(view, originalView) {
		t.Fatal("caller objects mutated")
	}
	end := false
	n := wbApply(t, d, WorkbenchEdit{Action: "update_process", Process: "b", DependsOn: []DependencyEntry{{From: "a", Condition: "process_completed"}}, ExitOnSkipped: &end})
	if !strings.Contains(string(n.Source), "# dependency comment") || strings.Count(string(n.Source), "9007199254740993") != 2 {
		t.Fatal(string(n.Source))
	}
	n = wbApply(t, n, WorkbenchEdit{Action: "update_process", Process: "b", DependsOn: []DependencyEntry{}})
	if len(n.View.Edges) != 0 {
		t.Fatal("explicit empty did not clear edges")
	}
}
