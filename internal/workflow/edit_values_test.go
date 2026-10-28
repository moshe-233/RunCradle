package workflow

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestEditPreservesUnknownNumbersAndRejectsSharedNodes(t *testing.T) {
	for _, tc := range []struct{ format, raw string }{{"yaml", "# retained\nprocesses:\n  a:\n    command: echo old\n    custom: 9007199254740993\n"}, {"json", `{"processes":{"a":{"command":"echo old","custom":9007199254740993}},"unowned":{"n":0.1234567890123456789}}`}} {
		d := ParseDocument([]byte(tc.raw), tc.format, nil)
		edited, err := Edit(d, EditRequest{Process: "a", Field: "command", Value: json.RawMessage(`"echo new"`)})
		if err != nil || !edited.Valid() {
			t.Fatalf("edit: %v %+v", err, edited.Issues)
		}
		if !bytes.Contains(edited.Source, []byte("9007199254740993")) || tc.format == "json" && !bytes.Contains(edited.Source, []byte("0.1234567890123456789")) || tc.format == "yaml" && !bytes.Contains(edited.Source, []byte("# retained")) {
			t.Fatalf("unowned data lost: %s", edited.Source)
		}
		again, err := Edit(edited, EditRequest{Process: "a", Field: "command", Value: json.RawMessage(`"echo new"`)})
		if err != nil || !bytes.Equal(again.Source, edited.Source) {
			t.Fatal("no-op changed bytes")
		}
	}
	shared := ParseDocument([]byte("processes: {a: &node {command: echo old}, b: *node}"), "yaml", nil)
	if _, err := Edit(shared, EditRequest{Process: "a", Field: "command", Value: json.RawMessage(`"echo new"`)}); err == nil {
		t.Fatal("shared node edited")
	}
}
func TestCommonEditsRevalidateDependencies(t *testing.T) {
	d := ParseDocument([]byte(`{"processes":{"a":{"entrypoint":["/bin/true"]},"b":{"command":"echo b"}}}`), "json", nil)
	for field, value := range map[string]string{"entrypoint": `["/bin/echo","a b"]`, "working_dir": `"sub"`, "environment": `["SCOPE=smoke"]`} {
		next, err := Edit(d, EditRequest{Process: "a", Field: field, Value: json.RawMessage(value)})
		if err != nil || !next.Valid() {
			t.Fatalf("%s: %v", field, err)
		}
		d = next
	}
	_, err := Edit(d, EditRequest{Process: "b", Field: "depends_on", Value: json.RawMessage(`{"missing":{"condition":"process_completed"}}`)})
	if err == nil {
		t.Fatal("missing dependency accepted")
	}
	if _, err = Edit(d, EditRequest{Process: "a", Field: "replicas", Value: json.RawMessage(`2`)}); err == nil {
		t.Fatal("unsupported form field accepted")
	}
}
func valueDocument() Document {
	return ParseDocument([]byte("processes: {a: {command: echo ok, environment: [SCOPE=old, OTHER=keep]}}"), "yaml", []byte(`{"version":1,"inputs":[{"name":"scope","type":"string","default":"default","bindings":[{"process":"a","field":"environment","key":"SCOPE"}]},{"name":"number","type":"integer","default":9223372036854775807,"bindings":[{"process":"a","field":"environment","key":"NUMBER"}]}],"presets":[{"id":"quick","values":{"scope":"preset"}}]}`))
}
func TestValuesPriorityExactIntegersAndIsolatedExecution(t *testing.T) {
	d := valueDocument()
	original := append([]byte(nil), d.Source...)
	values, err := ResolveValues(d, "quick", map[string]json.RawMessage{"scope": json.RawMessage(`"explicit; not shell"`)})
	if err != nil {
		t.Fatal(err)
	}
	if string(values["scope"]) != `"explicit; not shell"` || string(values["number"]) != "9223372036854775807" {
		t.Fatalf("values %+v", values)
	}
	effective, err := ApplyValues(d, values)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(effective.Source), "SCOPE=explicit; not shell") || !strings.Contains(string(effective.Source), "NUMBER=9223372036854775807") || !strings.Contains(string(effective.Source), "OTHER=keep") {
		t.Fatalf("binding lost: %s", effective.Source)
	}
	if !bytes.Equal(original, d.Source) {
		t.Fatal("base mutated")
	}
}
func TestValuesRejectMissingInvalidAndExpansion(t *testing.T) {
	d := valueDocument()
	for _, values := range []map[string]json.RawMessage{{"unknown": json.RawMessage(`0`)}, {"scope": json.RawMessage(`"$HOME"`)}, {"scope": json.RawMessage(`"{{.x}}"`)}, {"scope": json.RawMessage(`"\u0000"`)}, {"number": json.RawMessage(`9223372036854775808`)}, {"number": json.RawMessage(`1.5`)}} {
		if _, err := ResolveValues(d, "", values); err == nil {
			t.Fatalf("invalid values accepted: %+v", values)
		}
	}
	missing := ParseDocument(d.Source, "yaml", []byte(`{"version":1,"inputs":[{"name":"required","type":"boolean","bindings":[{"process":"a","field":"environment","key":"FLAG"}]}]}`))
	if _, err := ResolveValues(missing, "", nil); err == nil {
		t.Fatal("missing required accepted")
	}
	dup := ParseDocument([]byte("processes: {a: {command: echo ok, environment: [SCOPE=a, SCOPE=b]}}"), "yaml", d.UIRaw)
	values, err := ResolveValues(dup, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ApplyValues(dup, values); err == nil {
		t.Fatal("ambiguous environment accepted")
	}
}

func TestFormsRefuseAmbiguousNativeShapes(t *testing.T) {
	for _, tc := range []struct{ source, field, value string }{
		{"processes: {a: {command: echo ok}}", "entrypoint", `["/bin/true"]`},
		{"processes: {a: {entrypoint: [/bin/true]}}", "command", `"echo changed"`},
		{"processes: {a: {command: echo ok, environment: {A: old}}}", "environment", `["A=new"]`},
	} {
		d := ParseDocument([]byte(tc.source), "yaml", nil)
		if _, e := Edit(d, EditRequest{Process: "a", Field: tc.field, Value: json.RawMessage(tc.value)}); e == nil {
			t.Fatalf("ambiguous %s silently rewritten", tc.field)
		}
	}
}
