package runs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runcradle/internal/engine"
	"runcradle/internal/workflow"
	"sync"
	"time"
)

type Store struct {
	root      *os.Root
	workspace string
	mu        sync.Mutex
	sequences map[string]uint64
}
type Intent struct {
	Version   int    `json:"schema_version"`
	RequestID string `json:"request_id"`
	Digest    string `json:"invocation_digest"`
	RunID     string `json:"run_id"`
	Revision  string `json:"revision"`
	New       bool   `json:"-"`
}
type definition struct {
	Workspace string
	Entry     string
	UIPath    string
	Format    string
	Source    []byte
	UIRaw     []byte
}
type saved struct {
	Retain   bool            `json:"retain_evidence"`
	Snapshot engine.Snapshot `json:"snapshot"`
}

func Open(workspace string, create bool) (*Store, error) {
	abs, e := filepath.Abs(workspace)
	if e != nil {
		return nil, e
	}
	abs, e = filepath.EvalSymlinks(abs)
	if e != nil {
		return nil, e
	}
	root, e := os.OpenRoot(abs)
	if e != nil {
		return nil, e
	}
	if e = private(root, ".runcradle", create); e != nil {
		root.Close()
		return nil, e
	}
	for _, name := range []string{"runs", "workflows", "requests", "locks"} {
		if e = private(root, ".runcradle/"+name, create); e != nil {
			root.Close()
			return nil, e
		}
	}
	return &Store{root: root, workspace: abs, sequences: map[string]uint64{}}, nil
}
func (s *Store) Close() error { return s.root.Close() }
func (s *Store) path(id string) (string, error) {
	if !validID(id, 32) {
		return "", fmt.Errorf("invalid run identity")
	}
	p := filepath.Join(".runcradle/runs", id)
	if e := private(s.root, p, false); e != nil {
		return "", e
	}
	return p, nil
}
func (s *Store) SaveDefinition(d workflow.Document) error {
	if !validID(d.Revision, 64) || d.Workspace != s.workspace {
		return fmt.Errorf("invalid definition identity")
	}
	p := filepath.Join(".runcradle/workflows", d.Revision)
	if e := private(s.root, p, true); e != nil {
		return e
	}
	file := filepath.Join(p, "definition.json")
	v := definition{d.Workspace, d.Entry, d.UIPath, d.Format, d.Source, d.UIRaw}
	var existing definition
	e := s.read(file, &existing, 2<<20)
	if e == nil {
		a, _ := json.Marshal(existing)
		b, _ := json.Marshal(v)
		if string(a) != string(b) {
			return fmt.Errorf("immutable definition conflict")
		}
		return nil
	}
	if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	return s.write(file, v)
}
func (s *Store) Definition(revision string) (workflow.Document, error) {
	if !validID(revision, 64) {
		return workflow.Document{}, fmt.Errorf("invalid revision")
	}
	p := filepath.Join(".runcradle/workflows", revision)
	if e := private(s.root, p, false); e != nil {
		return workflow.Document{}, e
	}
	var v definition
	if e := s.read(filepath.Join(p, "definition.json"), &v, 2<<20); e != nil {
		return workflow.Document{}, e
	}
	d := workflow.ParseDocument(v.Source, v.Format, v.UIRaw)
	d.Workspace = v.Workspace
	d.Entry = v.Entry
	d.UIPath = v.UIPath
	files := map[string][]byte{"format": []byte(v.Format), "native/" + filepath.ToSlash(v.Entry): v.Source}
	if v.UIPath != "" {
		files["ui/"+filepath.ToSlash(v.UIPath)] = v.UIRaw
	}
	d.Revision = workflow.ComputeRevision(files)
	if !d.Valid() || d.Revision != revision {
		return workflow.Document{}, fmt.Errorf("definition integrity mismatch")
	}
	return d, nil
}
func requestKey(requestID string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(requestID))) }
func (s *Store) Request(requestID string) (Intent, error) {
	var i Intent
	e := s.read(".runcradle/requests/"+requestKey(requestID)+".json", &i, 16384)
	if e == nil && (i.Version != 1 || i.RequestID != requestID || !validID(i.RunID, 32) || !validID(i.Revision, 64)) {
		e = fmt.Errorf("request identity mismatch")
	}
	return i, e
}
func (s *Store) Begin(ctx context.Context, requestID, digest string, d workflow.Document) (Intent, error) {
	if requestID == "" || len(requestID) > 128 || !validID(digest, 64) {
		return Intent{}, fmt.Errorf("invalid request identity")
	}
	op, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	release, e := s.lock(op, ".runcradle/locks/"+requestKey(requestID)+".lock")
	if e != nil {
		return Intent{}, e
	}
	defer release()
	i, e := s.Request(requestID)
	if e == nil {
		if i.Digest != digest {
			return Intent{}, fmt.Errorf("request_id conflicts with existing invocation: %w", ErrConflict)
		}
		return i, nil
	}
	if !errors.Is(e, os.ErrNotExist) {
		return Intent{}, e
	}
	if e = s.SaveDefinition(d); e != nil {
		return Intent{}, e
	}
	id := nonce()
	if id == "" {
		return Intent{}, fmt.Errorf("random identity unavailable")
	}
	dir := filepath.Join(".runcradle/runs", id)
	if e = private(s.root, dir, true); e != nil {
		return Intent{}, e
	}
	i = Intent{Version: 1, RequestID: requestID, Digest: digest, RunID: id, Revision: d.Revision, New: true}
	if e = s.write(filepath.Join(dir, "intent.json"), i); e != nil {
		return Intent{}, e
	}
	initial := engine.Snapshot{Version: 1, RunID: id, Revision: d.Revision, Reason: "submission_unknown", TaskResult: "unknown", Cleanup: "unknown", HistoryIntegrity: "partial", IOStatus: "unknown", StartedAt: time.Now().UTC()}
	if e = s.Save(initial, false, time.Now()); e != nil {
		return Intent{}, e
	}
	if e = s.write(".runcradle/requests/"+requestKey(requestID)+".json", i); e != nil {
		return Intent{}, e
	}
	return i, nil
}
func (s *Store) Save(snapshot engine.Snapshot, retain bool, now time.Time) error {
	dir, e := s.path(snapshot.RunID)
	if e != nil {
		return e
	}
	var intent Intent
	if e = s.read(filepath.Join(dir, "intent.json"), &intent, 16384); e != nil {
		return e
	}
	if intent.RunID != snapshot.RunID || intent.Revision != snapshot.Revision || snapshot.Version != 1 {
		return fmt.Errorf("snapshot identity mismatch")
	}
	raw, e := json.Marshal(snapshot)
	if e != nil {
		return e
	}
	var copy engine.Snapshot
	if e = json.Unmarshal(raw, &copy); e != nil {
		return e
	}
	for n := range copy.Nodes {
		diag := copy.Nodes[n].Diagnostic
		if diag == nil {
			continue
		}
		if !retain {
			diag.Text = ""
			if diag.Status == "available" {
				diag.Status = "not_retained"
			}
			diag.ExpiresAt = nil
		} else {
			diag.Text = clean(diag.Text, 512)
			if diag.Text != "" && diag.ExpiresAt == nil {
				expires := now.Add(7 * 24 * time.Hour)
				diag.ExpiresAt = &expires
			}
		}
	}
	return s.write(filepath.Join(dir, "snapshot.json"), saved{Retain: retain, Snapshot: copy})
}
func (s *Store) Load(id string, now time.Time) (engine.Snapshot, error) {
	return s.load(id, now, 2)
}

func (s *Store) load(id string, now time.Time, retries int) (engine.Snapshot, error) {
	dir, e := s.path(id)
	if e != nil {
		return engine.Snapshot{}, e
	}
	var record saved
	if e = s.read(filepath.Join(dir, "snapshot.json"), &record, 2<<20); e != nil {
		return engine.Snapshot{}, e
	}
	original, e := json.Marshal(record)
	if e != nil {
		return engine.Snapshot{}, e
	}
	snapshot := record.Snapshot
	var intent Intent
	if e = s.read(filepath.Join(dir, "intent.json"), &intent, 16384); e != nil {
		return snapshot, e
	}
	if snapshot.Version != 1 || snapshot.RunID != id || snapshot.Revision != intent.Revision {
		return snapshot, fmt.Errorf("snapshot identity mismatch")
	}
	d, e := s.Definition(snapshot.Revision)
	if e != nil {
		return snapshot, e
	}
	nodes := map[string]string{}
	for _, n := range d.View.Nodes {
		nodes[n.Name] = n.ID
	}
	seen := map[string]bool{}
	for _, n := range snapshot.Nodes {
		if nodes[n.Name] == "" || nodes[n.Name] != n.ID || seen[n.Name] || (n.ExitCode != nil && (*n.ExitCode < 0 || *n.ExitCode > 255)) {
			return snapshot, fmt.Errorf("snapshot node identity/result mismatch")
		}
		seen[n.Name] = true
	}
	if snapshot.EngineInstanceID != "" && len(snapshot.Nodes) != len(nodes) {
		return snapshot, fmt.Errorf("snapshot node set incomplete")
	}
	if (snapshot.TaskResult == "succeeded" || snapshot.TaskResult == "failed") && engine.TargetResult(d, snapshot.Targets, snapshot.Nodes) != snapshot.TaskResult {
		return snapshot, fmt.Errorf("snapshot task result inconsistent with actual target exits")
	}
	expired := false
	for n := range snapshot.Nodes {
		d := snapshot.Nodes[n].Diagnostic
		if d == nil {
			continue
		}
		if !record.Retain {
			d.Text = ""
		} else if d.ExpiresAt != nil && !now.Before(*d.ExpiresAt) {
			d.Text = ""
			d.Status = "expired"
			expired = true
		}
	}
	if expired {
		record.Snapshot = snapshot
		if e = s.write(filepath.Join(dir, "snapshot.json"), record); e != nil {
			return snapshot, e
		}
	}
	_, _, gap, journalErr := s.Events(id, 0, 1)
	if gap || journalErr != nil {
		snapshot.HistoryIntegrity = "gap"
	}
	if !snapshot.Final && snapshot.Reason != "submission_unknown" {
		active, e := s.Active(id)
		if e != nil {
			return snapshot, e
		}
		if !active {
			// The owner may have published its final snapshot after our first read.
			// Reconcile that atomic publication before interpreting lock absence.
			var latest saved
			if e = s.read(filepath.Join(dir, "snapshot.json"), &latest, 2<<20); e != nil {
				return snapshot, e
			}
			raw, e := json.Marshal(latest)
			if e != nil {
				return snapshot, e
			}
			if !bytes.Equal(raw, original) {
				if retries == 0 {
					return snapshot, fmt.Errorf("snapshot changed during ownership observation; retry inspection")
				}
				return s.load(id, now, retries-1)
			}
			active, e = s.Active(id)
			if e != nil {
				return snapshot, e
			}
			if !active {
				snapshot.Reason = "interrupted"
				snapshot.TaskResult = "unknown"
				snapshot.Cleanup = "unknown"
			}
		}
	}
	return snapshot, nil
}
