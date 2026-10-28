package runs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestEmptyLibraryAndRunListsAreReadable(t *testing.T) {
	s, d := fixture(t)
	if rows, more, e := s.ListRuns("", 0, 20); e != nil || more || len(rows) != 0 {
		t.Fatalf("empty runs: %v %v", rows, e)
	}
	if e := os.Mkdir(filepath.Join(d.Workspace, ".runcradle/library"), 0700); e != nil {
		t.Fatal(e)
	}
	if rows, more, e := s.ListWorkflows(0, 20); e != nil || more || len(rows) != 0 {
		t.Fatalf("empty existing library: %v %v", rows, e)
	}
}

func TestConcurrentFirstInvocationsReturnTypedConflict(t *testing.T) {
	s, d := fixture(t)
	start := make(chan struct{})
	results := make(chan error, 2)
	var group sync.WaitGroup
	for _, digest := range []string{strings.Repeat("a", 64), strings.Repeat("b", 64)} {
		group.Add(1)
		go func(digest string) {
			defer group.Done()
			<-start
			_, e := s.Begin(context.Background(), "first-race", digest, d)
			results <- e
		}(digest)
	}
	close(start)
	group.Wait()
	close(results)
	success, conflicts := 0, 0
	for e := range results {
		if e == nil {
			success++
		} else if errors.Is(e, ErrConflict) {
			conflicts++
		} else {
			t.Fatalf("untyped conflict: %T %v", e, e)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success %d conflicts %d", success, conflicts)
	}
}
