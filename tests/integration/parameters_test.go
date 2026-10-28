package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	eng "runcradle/internal/engine"
	pc "runcradle/internal/engine/processcompose"
	"runcradle/internal/workflow"
	"testing"
	"time"
)

func TestW3NativeParameterValuesAreLiteralAndBaseIsImmutable(t *testing.T) {
	dir := t.TempDir()
	script := []byte("import os,json\nwith open('observed.json','w') as f: json.dump([os.environ['SCOPE'],os.environ['NUMBER']],f)\n")
	if e := os.WriteFile(filepath.Join(dir, "check.py"), script, 0600); e != nil {
		t.Fatal(e)
	}
	source := []byte(`{"processes":{"a":{"entrypoint":["python3","check.py"],"availability":{"exit_on_end":true}}}}`)
	ui := []byte(`{"version":1,"inputs":[{"name":"scope","type":"string","default":"default","bindings":[{"process":"a","field":"environment","key":"SCOPE"}]},{"name":"number","type":"integer","default":9223372036854775807,"bindings":[{"process":"a","field":"environment","key":"NUMBER"}]}]}`)
	d, e := workflow.FromBytes(dir, "pc.json", "ui.json", "json", source, ui)
	if e != nil {
		t.Fatal(e)
	}
	a := pc.New(engine(t), executable)
	h, e := a.Start(context.Background(), eng.StartRequest{Source: eng.WorkflowSource{Document: d}, Targets: []string{"a"}, Values: map[string]json.RawMessage{"scope": json.RawMessage(`"a; touch forbidden 'quote'"`)}, Deadline: time.Now().Add(5 * time.Second)})
	if e != nil {
		t.Fatal(e)
	}
	defer a.Stop(context.Background(), h)
	s := awaitEngine(t, a, h, func(s eng.Snapshot) bool { return s.Final })
	if s.Revision != d.Revision || string(s.Values["number"]) != "9223372036854775807" || s.Nodes[0].ExitCode == nil || *s.Nodes[0].ExitCode != 0 {
		t.Fatalf("facts: %+v", s)
	}
	raw, e := os.ReadFile(filepath.Join(dir, "observed.json"))
	if e != nil {
		t.Fatal(e)
	}
	var observed []string
	if e = json.Unmarshal(raw, &observed); e != nil || len(observed) != 2 || observed[0] != "a; touch forbidden 'quote'" || observed[1] != "9223372036854775807" {
		t.Fatalf("actual env: %s %v", raw, e)
	}
	if _, e = os.Stat(filepath.Join(dir, "forbidden")); !os.IsNotExist(e) {
		t.Fatal("parameter became Shell code")
	}
	if string(d.Source) != string(source) {
		t.Fatal("base bytes modified")
	}
}
