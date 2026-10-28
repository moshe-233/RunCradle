package workflow

import (
	"bytes"
	"testing"
)

func TestNativeViewPreservesSourceAndDependencies(t *testing.T) {
	source := []byte("version: '0.5'\nunknown: keep\nprocesses:\n  api:\n    command: echo hello\n    readiness_probe: {http_get: {port: '8080'}}\n  check:\n    entrypoint: [echo, '中文 🐈']\n    depends_on: {api: {condition: process_healthy}}\n")
	d := ParseDocument(source, "yaml", nil)
	if !d.Valid() || len(d.View.Nodes) != 2 || len(d.View.Edges) != 1 || !bytes.Equal(d.Source, source) {
		t.Fatalf("unexpected document: %+v", d)
	}
	if d.View.Edges[0].Condition != "process_healthy" || d.View.Nodes[1].Name != "check" || len(d.Revision) != 64 {
		t.Fatalf("lost declaration: %+v", d.View)
	}
}

func TestRejectCyclesAndMissingDependency(t *testing.T) {
	for _, source := range []string{
		"processes: {a: {command: x, depends_on: {missing: {}}}}",
		"processes: {a: {command: x, depends_on: {b: {}}}, b: {command: x, depends_on: {a: {}}}}",
	} {
		if d := ParseDocument([]byte(source), "yaml", nil); d.Valid() {
			t.Fatalf("accepted invalid graph: %s", source)
		}
	}
}

func TestEquivalentJSONYAMLAndRevision(t *testing.T) {
	a := ParseDocument([]byte(`{"processes":{"a":{"command":"echo ok"}}}`), "json", nil)
	b := ParseDocument([]byte("processes:\n  a: {command: echo ok}\n"), "yaml", nil)
	if !a.Valid() || !b.Valid() || len(a.View.Nodes) != 1 || len(b.View.Nodes) != 1 || a.View.Nodes[0].ID != b.View.Nodes[0].ID {
		t.Fatalf("views differ: %+v %+v", a, b)
	}
	if a.Revision == b.Revision || a.Revision != ParseDocument(a.Source, "json", nil).Revision {
		t.Fatal("revision is not raw-content sensitive and stable")
	}
}

func TestStepsFollowDependencies(t *testing.T) {
	d := ParseDocument([]byte("processes: {api: {command: x, depends_on: {prepare: {}}}, check: {command: x, depends_on: {api: {}}}, prepare: {command: x}}"), "yaml", nil)
	if !d.Valid() || len(d.View.Nodes) != 3 {
		t.Fatalf("bad document: %+v", d.Issues)
	}
	want := []string{"prepare", "api", "check"}
	for i, n := range d.View.Nodes {
		if n.Name != want[i] {
			t.Fatalf("step %d=%s, want %s", i, n.Name, want[i])
		}
	}
}

func TestJSONSyntaxHasActualLineAndColumn(t *testing.T) {
	d := ParseDocument([]byte("{\n\"processes\": {},\n?\n}"), "json", nil)
	if d.Valid() || len(d.Issues) == 0 || d.Issues[0].Line != 3 || d.Issues[0].Column != 1 {
		t.Fatalf("lost JSON location: %+v", d.Issues)
	}
}
