package runs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runcradle/internal/engine"
	"runcradle/internal/workflow"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) (*Store, workflow.Document) {
	t.Helper()
	workspace := t.TempDir()
	os.WriteFile(filepath.Join(workspace, "pc.yaml"), []byte("processes: {a: {entrypoint: [/bin/true]}}"), 0600)
	d, e := workflow.Load(workspace, "pc.yaml", "")
	if e != nil {
		t.Fatal(e)
	}
	s, e := Open(workspace, true)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s, d
}
func TestIntentIsIdempotentConflictsAndFreezesDefinition(t *testing.T) {
	s, d := fixture(t)
	ctx := context.Background()
	i, e := s.Begin(ctx, "request", strings.Repeat("a", 64), d)
	if e != nil || !i.New {
		t.Fatalf("begin %+v %v", i, e)
	}
	retry, e := s.Begin(ctx, "request", strings.Repeat("a", 64), d)
	if e != nil || retry.New || retry.RunID != i.RunID {
		t.Fatalf("repeated side effects %+v %v", retry, e)
	}
	if _, e = s.Begin(ctx, "request", strings.Repeat("b", 64), d); e == nil {
		t.Fatal("conflict overwritten")
	}
	os.WriteFile(filepath.Join(d.Workspace, d.Entry), []byte("changed"), 0600)
	old, e := s.Definition(d.Revision)
	if e != nil || string(old.Source) != string(d.Source) {
		t.Fatalf("historical definition replaced %v", e)
	}
	state, e := s.Load(i.RunID, time.Now())
	if e != nil || state.Final || state.Reason != "submission_unknown" {
		t.Fatalf("uncertain startup repeated/forgotten %+v %v", state, e)
	}
}
func TestDiagnosticDefaultOffAndExpiry(t *testing.T) {
	s, d := fixture(t)
	i, e := s.Begin(context.Background(), "request", strings.Repeat("a", 64), d)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	snapshot := engine.Snapshot{Version: 1, RunID: i.RunID, Revision: d.Revision, Final: true, Nodes: []engine.Node{{Name: "a", ID: d.View.Nodes[0].ID, Diagnostic: &engine.Diagnostic{Status: "available", Text: "private failure"}}}}
	if e = s.Save(snapshot, false, now); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(filepath.Join(d.Workspace, ".runcradle/runs", i.RunID, "snapshot.json"))
	if e != nil || strings.Contains(string(raw), "private failure") {
		t.Fatal("diagnostic stored without opt-in")
	}
	if e = s.Save(snapshot, true, now); e != nil {
		t.Fatal(e)
	}
	stored, e := s.Load(i.RunID, now.Add(8*24*time.Hour))
	if e != nil || stored.Nodes[0].Diagnostic.Text != "" || stored.Nodes[0].Diagnostic.Status != "expired" {
		t.Fatalf("expired text leaked %+v %v", stored, e)
	}
}

func TestJournalBudgetsPreserveSnapshot(t *testing.T) {
	s, d := fixture(t)
	i, e := s.Begin(context.Background(), "budget", strings.Repeat("a", 64), d)
	if e != nil {
		t.Fatal(e)
	}
	if cut, e := s.Append(i.RunID, engine.Event{Kind: strings.Repeat("x", maxEvent)}); e != nil || !cut {
		t.Fatalf("oversized event accepted: %v %v", cut, e)
	}
	if cut, e := s.Append(i.RunID, engine.Event{Kind: "first"}); e != nil || cut {
		t.Fatalf("ordinary event rejected: %v %v", cut, e)
	}
	file := filepath.Join(d.Workspace, ".runcradle/runs", i.RunID, "events.jsonl")
	// Sparse size fixture isolates the file-budget guard; malformed journal
	// integrity is separately covered by the partial-history test.
	if e = os.Truncate(file, maxEvents); e != nil {
		t.Fatal(e)
	}
	if cut, e := s.Append(i.RunID, engine.Event{Kind: "over-budget"}); e != nil || !cut {
		t.Fatalf("journal exceeded budget: %v %v", cut, e)
	}
	st, e := os.Stat(file)
	if e != nil || st.Size() != maxEvents {
		t.Fatalf("journal grew: %v %v", st, e)
	}
	state := engine.Snapshot{Version: 1, RunID: i.RunID, Revision: d.Revision, Final: true, TaskResult: "unknown", HistoryIntegrity: "truncated"}
	if e = s.Save(state, false, time.Now()); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(filepath.Dir(file), "snapshot.json")); e != nil {
		t.Fatal("event budget prevented independent snapshot", e)
	}
}
func TestJournalRejectsPartialHistoryAndKeepsFinalSnapshot(t *testing.T) {
	s, d := fixture(t)
	i, e := s.Begin(context.Background(), "request", strings.Repeat("a", 64), d)
	if e != nil {
		t.Fatal(e)
	}
	for k := 0; k < 2; k++ {
		if truncated, e := s.Append(i.RunID, engine.Event{Kind: "state", ObservedAt: time.Now()}); e != nil || truncated {
			t.Fatalf("append %v %v", truncated, e)
		}
	}
	page, next, gap, e := s.Events(i.RunID, 0, 1)
	if e != nil || gap || len(page) != 1 || next != 1 {
		t.Fatalf("page %+v %d %v %v", page, next, gap, e)
	}
	if e = os.WriteFile(filepath.Join(d.Workspace, ".runcradle/runs", i.RunID, "events.jsonl"), []byte("{unfinished"), 0600); e != nil {
		t.Fatal(e)
	}
	_, _, gap, e = s.Events(i.RunID, 0, 10)
	if e == nil || !gap {
		t.Fatal("partial journal presented as complete")
	}
	state, e := s.Load(i.RunID, time.Now())
	if e != nil || state.HistoryIntegrity != "gap" {
		t.Fatalf("gap erased %+v %v", state, e)
	}
}
func TestPrivateStateSymlinkRejected(t *testing.T) {
	workspace := t.TempDir()
	os.Mkdir(filepath.Join(workspace, "other"), 0700)
	os.Symlink("other", filepath.Join(workspace, ".runcradle"))
	if s, e := Open(workspace, true); e == nil {
		s.Close()
		t.Fatal("private state alias accepted")
	}
}

func TestReopenedJournalCannotResetSequence(t *testing.T) {
	s, d := fixture(t)
	i, e := s.Begin(context.Background(), "request", strings.Repeat("a", 64), d)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Append(i.RunID, engine.Event{Kind: "first"}); e != nil {
		t.Fatal(e)
	}
	other, e := Open(d.Workspace, false)
	if e != nil {
		t.Fatal(e)
	}
	defer other.Close()
	if _, e = other.Append(i.RunID, engine.Event{Kind: "duplicate-sequence"}); e == nil {
		t.Fatal("another writer reset persistent sequence")
	}
	page, _, gap, e := s.Events(i.RunID, 0, 100)
	if e != nil || gap || len(page) != 1 {
		t.Fatalf("original history damaged: %v %v %v", page, gap, e)
	}
}
func TestOwnerLeaseAndWorkspaceConcurrencyBound(t *testing.T) {
	s, d := fixture(t)
	var release []func() error
	for k := 0; k < 4; k++ {
		i, e := s.Begin(context.Background(), fmt.Sprint(k), strings.Repeat("a", 64), d)
		if e != nil {
			t.Fatal(e)
		}
		f, e := s.Lease(i.RunID)
		if e != nil {
			t.Fatal(e)
		}
		release = append(release, f)
		if active, e := s.Active(i.RunID); e != nil || !active {
			t.Fatalf("owner not held %v %v", active, e)
		}
	}
	defer func() {
		for _, f := range release {
			f()
		}
	}()
	i, e := s.Begin(context.Background(), "fifth", strings.Repeat("a", 64), d)
	if e != nil {
		t.Fatal(e)
	}
	if f, e := s.Lease(i.RunID); e == nil {
		f()
		t.Fatal("workspace capacity unbounded")
	}
}
