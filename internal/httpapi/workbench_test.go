package httpapi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkbenchAPI(t *testing.T) {
	s := apiFixture(t)
	body := map[string]any{"format": "yaml", "source": "processes: {a: {command: echo ok}}", "workbench_edit": map[string]any{"action": "bind_input", "input_spec": map[string]any{"name": "n", "type": "integer", "default": json.RawMessage(`9223372036854775807`), "bindings": []any{map[string]string{"process": "a", "field": "environment", "key": "N"}}}}}
	code, raw := callAPI(t, s, "POST", "/api/v1/validate", body, true)
	if code != 200 || !strings.Contains(string(raw), `"ui_path":"ui.json"`) {
		t.Fatalf("%d %s", code, raw)
	}
	var draft Draft
	if e := json.Unmarshal(raw, &draft); e != nil {
		t.Fatal(e)
	}
	code, raw = callAPI(t, s, "POST", "/api/v1/workflows", draft, true)
	if code != 200 && code != 201 {
		t.Fatalf("save %d %s", code, raw)
	}
	var saved struct {
		Revision string `json:"revision"`
		Workflow struct {
			ID string `json:"workflow_id"`
		} `json:"workflow"`
	}
	if e := json.Unmarshal(raw, &saved); e != nil {
		t.Fatal(e)
	}
	code, raw = callAPI(t, s, "GET", "/api/v1/workflows/"+saved.Workflow.ID, nil, true)
	var restored struct {
		Draft
		Revision string `json:"revision"`
	}
	if e := json.Unmarshal(raw, &restored); e != nil {
		t.Fatal(e)
	}
	if code != 200 || restored.UIPath != "ui.json" || restored.UISource == nil || *restored.UISource != *draft.UISource || restored.Revision != saved.Revision {
		t.Fatalf("restore generated UI metadata: %d %s", code, raw)
	}
	body["edit"] = map[string]any{"process": "a", "field": "command", "value": "echo bad"}
	code, _ = callAPI(t, s, "POST", "/api/v1/validate", body, true)
	if code != 400 {
		t.Fatal("mixed edits", code)
	}
}
func TestWorkspaceFilesBoundaries(t *testing.T) {
	s := apiFixture(t)
	for _, f := range []string{"a.yaml", "sub/b.yml", "sub/ui.json", ".runcradle/secret.json", ".hidden/x.yaml", "node_modules/x.json"} {
		p := filepath.Join(s.workspace, f)
		if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(p, []byte("{}"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	if e := os.Symlink(t.TempDir(), filepath.Join(s.workspace, "outside")); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink("a.yaml", filepath.Join(s.workspace, "link.yaml")); e != nil {
		t.Fatal(e)
	}
	if c, _ := callAPI(t, s, "GET", "/api/v1/workspace-files", nil, false); c != 401 {
		t.Fatal(c)
	}
	c, b := callAPI(t, s, "GET", "/api/v1/workspace-files", nil, true)
	if c != 200 {
		t.Fatalf("%d %s", c, b)
	}
	if string(b) != "{\"files\":[\"a.yaml\",\"sub/b.yml\"],\"ui_files\":[\"sub/ui.json\"],\"truncated\":false}\n" {
		t.Fatal(string(b))
	}
	for i := 0; i < 205; i++ {
		if e := os.WriteFile(filepath.Join(s.workspace, fmt.Sprintf("f%03d.json", i)), nil, 0600); e != nil {
			t.Fatal(e)
		}
	}
	_, b = callAPI(t, s, "GET", "/api/v1/workspace-files", nil, true)
	var result struct {
		Files, UIFiles []string
		Truncated      bool
	}
	if e := json.Unmarshal(b, &result); e != nil {
		t.Fatal(e)
	}
	if !result.Truncated || len(result.Files) > 200 {
		t.Fatal(string(b))
	}
}

func TestWorkspaceFilesReadBudget(t *testing.T) {
	s := apiFixture(t)
	existing, err := os.ReadDir(s.workspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(s.workspace, "a"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.workspace, "a/child.yaml"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	// The root itself consumes all 2,000 reads. Recursion must not read more.
	for i := 0; i < 2000-len(existing)-2; i++ {
		if err := os.WriteFile(filepath.Join(s.workspace, fmt.Sprintf("f%04d.txt", i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(s.workspace, "z.yaml"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	code, raw := callAPI(t, s, "GET", "/api/v1/workspace-files", nil, true)
	var result workspaceFiles
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if code != 200 || !result.Truncated || len(result.Files) != 1 || result.Files[0] != "z.yaml" {
		t.Fatalf("read budget: %d %s", code, raw)
	}
}

func TestWorkspaceFilesDepthBudget(t *testing.T) {
	s := apiFixture(t)
	allowed := "a/b/c/d/e/f/ok.yaml"
	hidden := "a/b/c/d/e/f/g/too-deep.yaml"
	for _, name := range []string{allowed, hidden} {
		p := filepath.Join(s.workspace, name)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	code, raw := callAPI(t, s, "GET", "/api/v1/workspace-files", nil, true)
	var result workspaceFiles
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if code != 200 || !result.Truncated || len(result.Files) != 1 || result.Files[0] != allowed {
		t.Fatalf("depth budget: %d %s", code, raw)
	}
}
