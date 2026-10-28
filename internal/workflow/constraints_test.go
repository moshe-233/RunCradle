package workflow

import (
	"fmt"
	"strings"
	"testing"
)

func hasCode(d Document, code string) bool {
	for _, i := range d.Issues {
		if i.Code == code {
			return true
		}
	}
	return false
}
func TestParseBudgetsAndDuplicateKeys(t *testing.T) {
	cases := []struct{ name, source, format, code string }{
		{"yaml duplicate", "processes: {a: {command: x, command: y}}", "yaml", "duplicate_key"},
		{"json duplicate", `{"processes":{},"processes":{}}`, "json", "duplicate_key"},
		{"trailing json", `{"processes":{}} {}`, "json", "syntax"},
		{"json comments", "// comment\n{}", "json", "syntax"},
		{"multi yaml", "processes: {}\n---\nprocesses: {}", "yaml", "syntax"},
		{"depth", "processes: {}\nx: " + strings.Repeat("[", 65) + "0" + strings.Repeat("]", 65), "yaml", "limit"},
		{"cycle alias", "processes: {}\nx: &x [*x]", "yaml", "alias_cycle"},
		{"large", strings.Repeat(" ", MaxSource+1), "yaml", "limit"},
		{"bad key", "processes: {}\nx: {42: nope}", "yaml", "key"},
		{"merge type", "processes: {}\nx: {<<: 42}", "yaml", "merge"},
		{"condition", "processes: {a: {command: x}, b: {command: x, depends_on: {a: {condition: imaginary}}}}", "yaml", "condition"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := ParseDocument([]byte(c.source), c.format, nil)
			if d.Valid() || !hasCode(d, c.code) {
				t.Fatalf("accepted or wrong error: %+v", d.Issues)
			}
		})
	}
	many := "processes:\n"
	for i := 0; i < 129; i++ {
		many += fmt.Sprintf("  n%d: {command: x}\n", i)
	}
	if d := ParseDocument([]byte(many), "yaml", nil); d.Valid() || !hasCode(d, "limit") {
		t.Fatal("accepted 129 processes")
	}
}
func TestAliasesAndUnknownFieldsArePreserved(t *testing.T) {
	source := []byte("x-base: &base {command: echo ok, arbitrary: keep}\nprocesses:\n  a: {<<: *base, description: hello}\n  b: {command: echo other}\n")
	d := ParseDocument(source, "yaml", nil)
	if !d.Valid() || !d.View.RawEditOnly || d.View.Nodes[0].Declaration["command"] != "echo ok" || d.View.Nodes[0].Declaration["arbitrary"] != "keep" {
		t.Fatalf("alias/unknown loss: %+v", d)
	}
}
func TestIncompleteExternalViewDoesNotInventMissingNode(t *testing.T) {
	d := ParseDocument([]byte("extends: not-read.yaml\nprocesses: {check: {command: echo x, depends_on: {external: {}}}}"), "yaml", nil)
	if !d.Valid() || d.View.Complete || len(d.View.Nodes) != 1 || len(d.View.Edges) != 0 {
		t.Fatalf("wrong partial view: %+v", d)
	}
}
func TestHealthDependencyLocation(t *testing.T) {
	d := ParseDocument([]byte("processes:\n  api: {command: echo x}\n  test:\n    command: echo y\n    depends_on:\n      api: {condition: process_healthy}\n"), "yaml", nil)
	if d.Valid() || !hasCode(d, "health_dependency") {
		t.Fatal("accepted unprobed health dependency")
	}
	found := false
	for _, i := range d.Issues {
		if i.Code == "health_dependency" && i.Line == 6 && i.Column > 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("lost location: %+v", d.Issues)
	}
}
func TestMetadataBindingsAndTypes(t *testing.T) {
	source := []byte("processes: {a: {command: echo x}}")
	valid := []byte(`{"version":1,"title":"检查","nodes":{"a":{"label":"准备"}},"inputs":[{"name":"count","label":"次数","type":"integer","default":1,"enum":[1,2],"bindings":[{"process":"a","field":"environment","key":"COUNT"}]}],"presets":[{"id":"two","label":"两次","values":{"count":2}}]}`)
	d := ParseDocument(source, "yaml", valid)
	if !d.Valid() || d.View.Title != "检查" || d.View.Nodes[0].Label != "准备" {
		t.Fatalf("bad metadata: %+v", d.Issues)
	}
	for _, ui := range []string{
		`null`,
		`{"version":1,"version":1}`,
		`{"version":1,"nodes":{"missing":{"label":"x"}}}`,
		`{"version":1,"inputs":[{"name":"n","type":"integer","default":"1","bindings":[{"process":"a","field":"environment","key":"N"}]}]}`,
		`{"version":1,"inputs":[{"name":"n","type":"integer","default":9007199254740993,"enum":[9007199254740992],"bindings":[{"process":"a","field":"environment","key":"N"}]}]}`,
		`{"version":1,"inputs":[{"name":"n","type":"string","default":null,"bindings":[{"process":"a","field":"environment","key":"N"}]}]}`,
		`{"version":1,"inputs":[{"name":"a","type":"string","bindings":[{"process":"a","field":"environment","key":"N"}]},{"name":"b","type":"string","bindings":[{"process":"a","field":"environment","key":"N"}]}]}`,
		`{"version":1,"presets":[{"id":"bad","values":{"unknown":true}}]}`,
	} {
		if d := ParseDocument(source, "yaml", []byte(ui)); d.Valid() {
			t.Fatalf("accepted bad metadata: %s", ui)
		}
	}
	if d := ParseDocument(source, "yaml", []byte(strings.Repeat(" ", MaxUI+1))); d.Valid() {
		t.Fatal("accepted oversized UI")
	}
}

func TestAliasExpansionBudget(t *testing.T) {
	source := "processes: {}\nx0: &x0 [text]\n"
	for i := 1; i <= 6; i++ {
		source += fmt.Sprintf("x%d: &x%d [", i, i)
		for j := 0; j < 10; j++ {
			if j > 0 {
				source += ", "
			}
			source += fmt.Sprintf("*x%d", i-1)
		}
		source += "]\n"
	}
	d := ParseDocument([]byte(source), "yaml", nil)
	if d.Valid() || !hasCode(d, "limit") {
		t.Fatalf("unbounded aliases accepted: %+v", d.Issues)
	}
}
func TestDynamicNamesAreMarkedIncomplete(t *testing.T) {
	d := ParseDocument([]byte(`{"processes":{"\u0024NODE":{"command":"echo ok"}}}`), "json", nil)
	if !d.Valid() || d.View.Complete {
		t.Fatal("dynamic process name looked fully resolved")
	}
}
