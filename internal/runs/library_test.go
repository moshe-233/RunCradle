package runs

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runcradle/internal/workflow"
	"sync"
	"testing"
)

func libraryDocument(t *testing.T, dir, command string) workflow.Document {
	t.Helper()
	raw := []byte("processes:\n  a:\n    command: " + command + "\n")
	if err := os.WriteFile(filepath.Join(dir, "pc.yaml"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	d, err := workflow.Load(dir, "pc.yaml", "")
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func TestLibraryCASHistoryCopyAndExport(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	d := libraryDocument(t, dir, "echo old")
	item, err := s.CreateWorkflow(context.Background(), d)
	if err != nil || len(item.ID) != 64 {
		t.Fatalf("create: %+v %v", item, err)
	}
	same, err := s.SaveWorkflow(context.Background(), item.ID, d.Revision, d)
	if err != nil || len(same.Revisions) != 1 {
		t.Fatalf("no-op: %+v %v", same, err)
	}
	next := libraryDocument(t, dir, "echo new")
	updated, err := s.SaveWorkflow(context.Background(), item.ID, d.Revision, next)
	if err != nil || updated.Revision != next.Revision {
		t.Fatalf("save: %+v %v", updated, err)
	}
	if _, err = s.SaveWorkflow(context.Background(), item.ID, d.Revision, d); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale save accepted: %v", err)
	}
	_, old, err := s.Workflow(item.ID, d.Revision)
	if err != nil || !bytes.Equal(old.Source, d.Source) {
		t.Fatalf("old revision: %v", err)
	}
	clone, err := s.CopyWorkflow(context.Background(), item.ID, d.Revision)
	if err != nil || clone.ID == item.ID || clone.Revision != d.Revision {
		t.Fatalf("copy: %+v %v", clone, err)
	}
	archive, err := s.ExportWorkflow(item.ID, d.Revision)
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil || len(z.File) != 1 || z.File[0].Name != "process-compose.yaml" {
		t.Fatalf("export %v %v", z, err)
	}
	list, more, err := s.ListWorkflows(0, 20)
	if err != nil || more || len(list) != 2 {
		t.Fatalf("list: %+v %v %v", list, more, err)
	}
	source, err := os.ReadFile(filepath.Join(dir, "pc.yaml"))
	if err != nil || !bytes.Equal(source, next.Source) {
		t.Fatal("library modified source")
	}
}
func TestLibraryConcurrentWritersAndFailedSave(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	d := libraryDocument(t, dir, "echo old")
	item, err := s.CreateWorkflow(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	next := libraryDocument(t, dir, "echo new")
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			other, e := Open(dir, false)
			if e != nil {
				results <- e
				return
			}
			defer other.Close()
			_, e = other.SaveWorkflow(context.Background(), item.ID, d.Revision, next)
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for e := range results {
		if e == nil {
			success++
		} else if errors.Is(e, ErrConflict) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("writers %d/%d", success, conflict)
	}
	invalid := next
	invalid.Revision = "bad"
	if _, err = s.SaveWorkflow(context.Background(), item.ID, next.Revision, invalid); err == nil {
		t.Fatal("invalid definition accepted")
	}
	head, _, err := s.Workflow(item.ID, "")
	if err != nil || head.Revision != next.Revision {
		t.Fatalf("head corrupted: %+v %v", head, err)
	}
}

func TestUnpublishedDefinitionAndWriteFailureDoNotAdvanceLibraryHead(t *testing.T) {
	dir := t.TempDir()
	s, e := Open(dir, true)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	d := libraryDocument(t, dir, "echo first")
	record, e := s.CreateWorkflow(context.Background(), d)
	if e != nil {
		t.Fatal(e)
	}
	next := libraryDocument(t, dir, "echo next")
	if e = s.SaveDefinition(next); e != nil {
		t.Fatal(e)
	}
	// Crash-window fixture: archive exists but the publishing manifest was never replaced.
	head, _, e := s.Workflow(record.ID, "")
	if e != nil || head.Revision != d.Revision || len(head.Revisions) != 1 {
		t.Fatal("unpublished definition advanced head")
	}
	broken := libraryDocument(t, dir, "echo blocked")
	if e = os.WriteFile(filepath.Join(dir, ".runcradle/workflows", broken.Revision), []byte("ordinary file blocks archive directory"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveWorkflow(context.Background(), record.ID, d.Revision, broken); e == nil {
		t.Fatal("archive write failure accepted")
	}
	head, _, e = s.Workflow(record.ID, "")
	if e != nil || head.Revision != d.Revision {
		t.Fatal("write failure advanced head")
	}
}
