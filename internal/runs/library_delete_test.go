package runs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runcradle/internal/engine"
	"runcradle/internal/workflow"
	"strings"
	"testing"
	"time"
)

func TestDeleteWorkflowCASRetainsDefinitionsCopiesSourcesAndRuns(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	d := libraryDocument(t, dir, "echo old")
	ui := []byte(`{"version":1,"title":"Retained UI"}`)
	if err = os.WriteFile(filepath.Join(dir, "ui.json"), ui, 0600); err != nil {
		t.Fatal(err)
	}
	d, err = workflow.Load(dir, "pc.yaml", "ui.json")
	if err != nil {
		t.Fatal(err)
	}
	item, err := s.CreateWorkflow(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	clone, err := s.CopyWorkflow(ctx, item.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	next := libraryDocument(t, dir, "echo new")
	if _, err = s.SaveWorkflow(ctx, item.ID, d.Revision, next); err != nil {
		t.Fatal(err)
	}
	intent, err := s.Begin(ctx, "retained-run", strings.Repeat("a", 64), d)
	if err != nil {
		t.Fatal(err)
	}
	state := engine.Snapshot{Version: 1, RunID: intent.RunID, Revision: d.Revision, WorkflowID: item.ID, Final: false, Reason: "running", StartedAt: time.Now().UTC()}
	if err = s.Save(state, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	snapshotPath := filepath.Join(dir, ".runcradle/runs", intent.RunID, "snapshot.json")
	snapshotBefore, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Append(intent.RunID, engine.Event{Kind: "running"}); err != nil {
		t.Fatal(err)
	}
	invocation := json.RawMessage(`{"workflow_id":"` + item.ID + `"}`)
	if err = s.SaveInvocation(intent.RunID, invocation); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"", "bad", strings.ToUpper(next.Revision)} {
		if _, err = s.DeleteWorkflow(ctx, item.ID, expected); err == nil || errors.Is(err, ErrConflict) {
			t.Fatalf("invalid expected revision %q: %v", expected, err)
		}
	}
	if _, err = s.DeleteWorkflow(ctx, item.ID, d.Revision); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale delete: %v", err)
	}
	deleted, err := s.DeleteWorkflow(ctx, item.ID, next.Revision)
	if err != nil || !deleted.Deleted || deleted.WorkflowID != item.ID || deleted.DeletedRevision != next.Revision {
		t.Fatalf("delete: %+v %v", deleted, err)
	}
	if _, _, err = s.Workflow(item.ID, ""); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("record retained: %v", err)
	}
	if _, err = s.DeleteWorkflow(ctx, item.ID, next.Revision); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("absent delete: %v", err)
	}
	for _, revision := range []string{d.Revision, next.Revision} {
		if _, err = s.Definition(revision); err != nil {
			t.Fatalf("definition removed: %v", err)
		}
	}
	if _, _, err = s.Workflow(clone.ID, ""); err != nil {
		t.Fatalf("copy damaged: %v", err)
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "pc.yaml")); err != nil || !bytes.Equal(raw, next.Source) {
		t.Fatalf("source damaged: %v", err)
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "ui.json")); err != nil || !bytes.Equal(raw, ui) {
		t.Fatalf("UI source damaged: %v", err)
	}
	if got, err := os.ReadFile(snapshotPath); err != nil || !bytes.Equal(got, snapshotBefore) {
		t.Fatalf("active state modified: %s %v", got, err)
	}
	if got, err := s.Load(intent.RunID, time.Now()); err != nil || got.WorkflowID != item.ID || got.Final {
		t.Fatalf("state damaged: %+v %v", got, err)
	}
	if got, _, _, err := s.Events(intent.RunID, 0, 20); err != nil || len(got) != 1 || got[0].Kind != "running" {
		t.Fatalf("events damaged: %+v %v", got, err)
	}
	if got, err := s.Invocation(intent.RunID); err != nil || !bytes.Equal(got, invocation) {
		t.Fatalf("invocation damaged: %s %v", got, err)
	}
	if got, err := s.Request("retained-run"); err != nil || got.RunID != intent.RunID {
		t.Fatalf("request damaged: %+v %v", got, err)
	}
}

func TestLibraryMetadataIntegrityIsRequiredWithoutDefinitions(t *testing.T) {
	for _, tc := range []struct {
		name    string
		corrupt func(*WorkflowRecord)
	}{
		{"version", func(r *WorkflowRecord) { r.Version = 2 }},
		{"identity", func(r *WorkflowRecord) { r.ID = strings.Repeat("0", 64) }},
		{"head", func(r *WorkflowRecord) { r.Revision = "bad" }},
		{"empty_history", func(r *WorkflowRecord) { r.Revisions = nil }},
		{"invalid_history", func(r *WorkflowRecord) { r.Revisions = append(r.Revisions, "bad") }},
		{"head_not_in_history", func(r *WorkflowRecord) { r.Revisions = []string{strings.Repeat("0", 64)} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			s, err := Open(dir, true)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			d := libraryDocument(t, dir, "echo archive")
			item, err := s.CreateWorkflow(context.Background(), d)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.Remove(filepath.Join(dir, ".runcradle/workflows", d.Revision, "definition.json")); err != nil {
				t.Fatal(err)
			}
			broken := item
			tc.corrupt(&broken)
			if err = s.write(".runcradle/library/"+item.ID+".json", broken); err != nil {
				t.Fatal(err)
			}
			if _, _, err = s.Workflow(item.ID, ""); err == nil || errors.Is(err, os.ErrNotExist) {
				t.Fatalf("get ignored metadata corruption: %v", err)
			}
			if _, _, err = s.ListWorkflows(0, 20); err == nil {
				t.Fatal("list ignored metadata corruption")
			}
			if _, err = s.DeleteWorkflow(context.Background(), item.ID, item.Revision); err == nil {
				t.Fatal("delete ignored metadata corruption")
			}
			if _, err = s.SaveWorkflow(context.Background(), item.ID, item.Revision, d); err == nil {
				t.Fatal("save ignored metadata corruption")
			}
			if _, err = os.Stat(filepath.Join(dir, ".runcradle/library", item.ID+".json")); err != nil {
				t.Fatalf("corrupt record removed: %v", err)
			}
		})
	}
}

func TestMissingDefinitionCanBeListedAndDeleted(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	d := libraryDocument(t, dir, "echo removed")
	item, err := s.CreateWorkflow(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(dir, ".runcradle/workflows", d.Revision, "definition.json")); err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateWorkflow(context.Background(), libraryDocument(t, dir, "echo healthy"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Workflow(item.ID, ""); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("get must still validate definition: %v", err)
	}
	items, _, err := s.ListWorkflows(0, 20)
	if err != nil || len(items) != 2 {
		t.Fatalf("dangling definition poisoned listing: %+v %v", items, err)
	}
	if _, err = s.DeleteWorkflow(context.Background(), item.ID, other.Revision); !errors.Is(err, ErrConflict) {
		t.Fatalf("dangling CAS: %v", err)
	}
	if _, err = s.DeleteWorkflow(context.Background(), item.ID, item.Revision); err != nil {
		t.Fatal(err)
	}
	items, _, err = s.ListWorkflows(0, 20)
	if err != nil || len(items) != 1 || items[0].ID != other.ID {
		t.Fatalf("remaining library: %+v %v", items, err)
	}
}

func TestConcurrentSaveAndDeleteUseOneLibraryLock(t *testing.T) {
	for range 12 {
		dir := t.TempDir()
		s, err := Open(dir, true)
		if err != nil {
			t.Fatal(err)
		}
		d := libraryDocument(t, dir, "echo old")
		item, err := s.CreateWorkflow(context.Background(), d)
		if err != nil {
			t.Fatal(err)
		}
		next := libraryDocument(t, dir, "echo new")
		start := make(chan struct{})
		save, del := make(chan error, 1), make(chan error, 1)
		go func() { <-start; _, e := s.SaveWorkflow(context.Background(), item.ID, d.Revision, next); save <- e }()
		go func() { <-start; _, e := s.DeleteWorkflow(context.Background(), item.ID, d.Revision); del <- e }()
		close(start)
		saveErr, deleteErr := <-save, <-del
		switch {
		case saveErr == nil && errors.Is(deleteErr, ErrConflict):
			if head, _, e := s.Workflow(item.ID, ""); e != nil || head.Revision != next.Revision {
				t.Fatalf("save winner: %+v %v", head, e)
			}
		case errors.Is(saveErr, ErrConflict) && deleteErr == nil:
			if _, _, e := s.Workflow(item.ID, ""); !errors.Is(e, os.ErrNotExist) {
				t.Fatalf("delete winner resurrected: %v", e)
			}
		default:
			t.Fatalf("nonserializable save/delete: %v / %v", saveErr, deleteErr)
		}
		s.Close()
	}
}
