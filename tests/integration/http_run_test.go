package integration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestW2ThreeNodeHTTPCLI(t *testing.T) {
	for _, actual := range []int{0, 7, 8} {
		t.Run(fmt.Sprint(actual), func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range []string{"process-compose.yaml", "workflow.py"} {
				raw, e := os.ReadFile(filepath.Join("..", "fixtures", "http-workflow", name))
				if e != nil {
					t.Fatal(e)
				}
				if name == "process-compose.yaml" {
					raw = []byte(strings.Replace(string(raw), "--exit-code, '0'", fmt.Sprintf("--exit-code, '%d'", actual), 1))
				}
				if e = os.WriteFile(filepath.Join(dir, name), raw, 0600); e != nil {
					t.Fatal(e)
				}
			}
			out, code := cliCall(t, "workflow", "run", "--workspace", dir, "--file", "process-compose.yaml", "--target", "check", "--request-id", "http", "--timeout-ms", "10000", "--process-compose", engine(t))
			want := 0
			if actual != 0 {
				want = 1
			}
			if code != want {
				t.Fatalf("HTTP run %d: code=%d %s", actual, code, out)
			}
			body, e := os.ReadFile(filepath.Join(dir, "checked"))
			if e != nil || string(body) != "runcradle-http-ok\n" {
				t.Fatalf("HTTP request not verified: %q %v", body, e)
			}
			out, code = cliCall(t, "workflow", "inspect", "--workspace", dir, "--request-id", "http", "--node", "check")
			var v struct {
				Nodes []struct {
					ExitCode *int `json:"exit_code"`
				} `json:"nodes"`
			}
			if e = json.Unmarshal(out, &v); e != nil || code != 0 || len(v.Nodes) != 1 || v.Nodes[0].ExitCode == nil || *v.Nodes[0].ExitCode != actual {
				t.Fatalf("HTTP actual code lost: %s %v", out, e)
			}
		})
	}
}
