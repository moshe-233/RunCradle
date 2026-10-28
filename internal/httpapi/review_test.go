package httpapi

import (
	"fmt"
	"runcradle/internal/workflow"
	"sync"
	"testing"
)

func TestFreshWorkspaceHasReadableEmptyHistory(t *testing.T) {
	s := apiFixture(t)
	for _, path := range []string{"/api/v1/runs?offset=0&limit=20", "/api/v1/workflows?offset=0&limit=20"} {
		if code, raw := callAPI(t, s, "GET", path, nil, true); code != 200 {
			t.Fatalf("fresh %s: %d %s", path, code, raw)
		}
	}
}

func TestFirstConcurrentRunContentConflictIs409(t *testing.T) {
	s := apiFixture(t)
	source := "processes: {a: {command: echo ok}}"
	d, e := workflow.FromBytes(s.workspace, "pc.yaml", "", "yaml", []byte(source), nil)
	if e != nil {
		t.Fatal(e)
	}
	for round := 0; round < 4; round++ {
		start := make(chan struct{})
		codes := make(chan int, 2)
		var group sync.WaitGroup
		for _, timeout := range []int64{1000, 2000} {
			group.Add(1)
			go func(timeout int64) {
				defer group.Done()
				<-start
				code, _ := callAPI(t, s, "POST", "/api/v1/runs", RunRequest{Document: &Draft{Format: "yaml", Source: source, Entry: "pc.yaml"}, Revision: d.Revision, Targets: []string{"a"}, RequestID: fmt.Sprintf("http-race-%d", round), TimeoutMS: timeout}, true)
				codes <- code
			}(timeout)
		}
		close(start)
		group.Wait()
		close(codes)
		accepted, conflicts := 0, 0
		for code := range codes {
			switch code {
			case 202:
				accepted++
			case 409:
				conflicts++
			default:
				t.Fatalf("first concurrent request: HTTP %d", code)
			}
		}
		if accepted != 1 || conflicts != 1 {
			t.Fatalf("accepted %d conflicts %d", accepted, conflicts)
		}
	}
}
