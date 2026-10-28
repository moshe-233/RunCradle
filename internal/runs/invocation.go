package runs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runcradle/internal/engine"
	"sort"
	"time"
)

func (s *Store) SaveInvocation(id string, raw json.RawMessage) error {
	dir, err := s.path(id)
	if err != nil {
		return err
	}
	if len(raw) > 256<<10 || !json.Valid(raw) {
		return fmt.Errorf("invalid or oversized invocation")
	}
	file := filepath.Join(dir, "invocation.json")
	var old json.RawMessage
	if err = s.read(file, &old, 256<<10); err == nil {
		if !bytes.Equal(old, raw) {
			return fmt.Errorf("immutable invocation conflict")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return s.write(file, raw)
}
func (s *Store) Invocation(id string) (json.RawMessage, error) {
	dir, e := s.path(id)
	if e != nil {
		return nil, e
	}
	var raw json.RawMessage
	e = s.read(filepath.Join(dir, "invocation.json"), &raw, 256<<10)
	return raw, e
}

func (s *Store) ListRuns(workflowID string, offset, limit int) ([]engine.Snapshot, bool, error) {
	if offset < 0 || limit < 1 || limit > 100 {
		return nil, false, fmt.Errorf("offset >=0 and limit 1..100 required")
	}
	dir, err := s.root.Open(".runcradle/runs")
	if err != nil {
		return nil, false, err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(10001)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, false, err
	}
	if len(entries) > 10000 {
		return nil, false, fmt.Errorf("run scan budget exceeded")
	}
	items := []engine.Snapshot{}
	for _, entry := range entries {
		if !validID(entry.Name(), 32) {
			continue
		}
		state, e := s.Load(entry.Name(), time.Now())
		if e != nil {
			return nil, false, e
		}
		if workflowID == "" || state.WorkflowID == workflowID {
			state.Nodes = nil
			items = append(items, state)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].StartedAt.After(items[j].StartedAt) })
	if offset > len(items) {
		return nil, false, fmt.Errorf("offset exceeds run count")
	}
	end := min(len(items), offset+limit)
	return items[offset:end], end < len(items), nil
}
