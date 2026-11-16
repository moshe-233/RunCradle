package runs

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runcradle/internal/engine"
	"runcradle/internal/workflow"
	"sort"
	"strings"
	"time"
)

var ErrConflict = errors.New("saved revision or request content conflicts")

// WorkflowDeletion acknowledges removal of one library record, not its shared
// immutable definitions, source files, or any running or historical execution.
type WorkflowDeletion struct {
	WorkflowID      string `json:"workflow_id"`
	DeletedRevision string `json:"deleted_revision"`
	Deleted         bool   `json:"deleted"`
}

type WorkflowRecord struct {
	Version   int       `json:"schema_version"`
	ID        string    `json:"workflow_id"`
	Revision  string    `json:"revision"`
	Title     string    `json:"title"`
	Revisions []string  `json:"revisions,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (s *Store) CreateWorkflow(ctx context.Context, d workflow.Document) (WorkflowRecord, error) {
	var id [32]byte
	if _, err := rand.Read(id[:]); err != nil {
		return WorkflowRecord{}, err
	}
	return s.SaveWorkflow(ctx, hex.EncodeToString(id[:]), "", d)
}

func (s *Store) SaveWorkflow(ctx context.Context, id, expected string, d workflow.Document) (WorkflowRecord, error) {
	if !validID(id, 64) || !d.Valid() || d.Workspace != s.workspace {
		return WorkflowRecord{}, fmt.Errorf("invalid workflow identity or definition")
	}
	if _, err := engine.BindSource(engine.WorkflowSource{Document: d}); err != nil {
		return WorkflowRecord{}, err
	}
	if err := private(s.root, ".runcradle/library", true); err != nil {
		return WorkflowRecord{}, err
	}
	op, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	release, err := s.lock(op, ".runcradle/locks/library-"+id+".lock")
	if err != nil {
		return WorkflowRecord{}, err
	}
	defer release()
	file := ".runcradle/library/" + id + ".json"
	record, err := s.workflowRecord(id)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return record, err
	}
	if expected != record.Revision {
		return record, ErrConflict
	}
	if record.Revision == d.Revision {
		return record, nil
	}
	if len(record.Revisions) >= 4096 {
		return record, fmt.Errorf("workflow version limit (4096) reached; copy to a new workflow")
	}
	if op.Err() != nil {
		return record, op.Err()
	}
	if err = s.SaveDefinition(d); err != nil {
		return record, err
	}
	found := false
	for _, revision := range record.Revisions {
		found = found || revision == d.Revision
	}
	if !found {
		record.Revisions = append(record.Revisions, d.Revision)
	}
	record.Version, record.ID, record.Revision, record.Title = 1, id, d.Revision, d.View.Title
	record.UpdatedAt = time.Now().UTC()
	if op.Err() != nil {
		return WorkflowRecord{}, op.Err()
	}
	return record, s.write(file, record)
}

// workflowRecord validates library metadata without opening its definition.
// A missing archive must not prevent listing or deleting the library entry.
func (s *Store) workflowRecord(id string) (WorkflowRecord, error) {
	var record WorkflowRecord
	if !validID(id, 64) {
		return record, fmt.Errorf("invalid workflow identity")
	}
	if err := private(s.root, ".runcradle/library", false); err != nil {
		return record, err
	}
	if err := s.read(".runcradle/library/"+id+".json", &record, 2<<20); err != nil {
		return record, err
	}
	if record.Version != 1 || record.ID != id || !validID(record.Revision, 64) || len(record.Revisions) == 0 || len(record.Revisions) > 4096 {
		return record, fmt.Errorf("library integrity mismatch")
	}
	head := false
	for _, r := range record.Revisions {
		if !validID(r, 64) {
			return record, fmt.Errorf("invalid library revision")
		}
		head = head || r == record.Revision
	}
	if !head {
		return record, fmt.Errorf("library head is not a version of this workflow")
	}
	return record, nil
}

func (s *Store) Workflow(id, revision string) (WorkflowRecord, workflow.Document, error) {
	record, err := s.workflowRecord(id)
	if err != nil {
		return record, workflow.Document{}, err
	}
	if revision == "" {
		revision = record.Revision
	}
	found := false
	for _, r := range record.Revisions {
		found = found || r == revision
	}
	if !found {
		return record, workflow.Document{}, fmt.Errorf("revision is not a version of this workflow")
	}
	d, err := s.Definition(revision)
	if err == nil && d.Workspace != s.workspace {
		err = fmt.Errorf("workflow workspace mismatch")
	}
	return record, d, err
}

func (s *Store) DeleteWorkflow(ctx context.Context, id, expected string) (WorkflowDeletion, error) {
	if !validID(id, 64) || !validID(expected, 64) || expected != strings.ToLower(expected) {
		return WorkflowDeletion{}, fmt.Errorf("workflow identity and 64-character lowercase hexadecimal expected_revision required")
	}
	op, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	release, err := s.lock(op, ".runcradle/locks/library-"+id+".lock")
	if err != nil {
		return WorkflowDeletion{}, err
	}
	defer release()
	record, err := s.workflowRecord(id)
	if err != nil {
		return WorkflowDeletion{}, err
	}
	if expected != record.Revision {
		return WorkflowDeletion{}, ErrConflict
	}
	if err = op.Err(); err != nil {
		return WorkflowDeletion{}, err
	}
	if err = s.root.Remove(".runcradle/library/" + id + ".json"); err != nil {
		return WorkflowDeletion{}, err
	}
	// Match SaveWorkflow's durability boundary. A failure after unlink remains
	// an uncertain mutation and callers must reconcile rather than auto-retry.
	dir, err := s.root.Open(".runcradle/library")
	if err != nil {
		return WorkflowDeletion{}, err
	}
	if err = errors.Join(dir.Sync(), dir.Close()); err != nil {
		return WorkflowDeletion{}, err
	}
	return WorkflowDeletion{WorkflowID: id, DeletedRevision: record.Revision, Deleted: true}, nil
}

func (s *Store) ListWorkflows(offset, limit int) ([]WorkflowRecord, bool, error) {
	if offset < 0 || limit < 1 || limit > 100 {
		return nil, false, fmt.Errorf("offset >=0 and limit 1..100 required")
	}
	if err := private(s.root, ".runcradle/library", false); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []WorkflowRecord{}, false, nil
		}
		return nil, false, err
	}
	dir, err := s.root.Open(".runcradle/library")
	if err != nil {
		return nil, false, err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(10001)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, false, err
	}
	if len(entries) > 10000 {
		return nil, false, fmt.Errorf("library scan limit exceeded")
	}
	records := []WorkflowRecord{}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		id := entry.Name()[:len(entry.Name())-5]
		r, e := s.workflowRecord(id)
		// A concurrently deleted entry is absent from this page.
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return nil, false, e
		}
		records = append(records, r)
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].UpdatedAt.Equal(records[j].UpdatedAt) {
			return records[i].ID < records[j].ID
		}
		return records[i].UpdatedAt.After(records[j].UpdatedAt)
	})
	if offset > len(records) {
		return nil, false, fmt.Errorf("offset exceeds library size")
	}
	end := min(len(records), offset+limit)
	more := end < len(records)
	result := records[offset:end]
	for i := range result {
		result[i].Revisions = nil
	}
	return result, more, nil
}

func (s *Store) CopyWorkflow(ctx context.Context, id, revision string) (WorkflowRecord, error) {
	_, d, err := s.Workflow(id, revision)
	if err != nil {
		return WorkflowRecord{}, err
	}
	return s.CreateWorkflow(ctx, d)
}

func (s *Store) ExportWorkflow(id, revision string) ([]byte, error) {
	_, d, err := s.Workflow(id, revision)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	files := []struct {
		name string
		raw  []byte
	}{{"process-compose." + d.Format, d.Source}}
	if d.UIPath != "" {
		files = append(files, struct {
			name string
			raw  []byte
		}{"ui.json", d.UIRaw})
	}
	for _, file := range files {
		header := &zip.FileHeader{Name: file.name, Method: zip.Deflate}
		header.SetMode(0600)
		f, e := z.CreateHeader(header)
		if e != nil {
			return nil, e
		}
		if _, e = f.Write(file.raw); e != nil {
			return nil, e
		}
	}
	if err = z.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
