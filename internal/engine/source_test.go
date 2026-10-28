package engine

import (
	"runcradle/internal/workflow"
	"strings"
	"testing"
)

func TestManagedIdentityKeepsBaseRevisionAndLegacyFallback(t *testing.T) {
	d := workflow.ParseDocument([]byte("processes: {a: {command: echo ok}}"), "yaml", nil)
	d.Workspace = t.TempDir()
	d.Entry = "pc.yaml"
	d.Revision = workflow.ComputeRevision(map[string][]byte{"format": []byte("yaml"), "native/pc.yaml": d.Source})
	old, err := BindSource(WorkflowSource{Document: d})
	if err != nil {
		t.Fatal(err)
	}
	managed, err := BindSource(WorkflowSource{Document: d, WorkflowID: strings.Repeat("a", 64), Managed: true})
	if err != nil || managed.WorkflowID == old.WorkflowID || managed.Revision != d.Revision {
		t.Fatalf("managed %+v %v", managed, err)
	}
	if _, err = BindSource(WorkflowSource{Document: d, WorkflowID: "bad", Managed: true}); err == nil {
		t.Fatal("invalid ID accepted")
	}
}
