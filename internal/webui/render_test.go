package webui

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"html"
	"regexp"
	"runcradle/internal/workflow"
	"strings"
	"testing"
	"time"
)

func TestLayoutCentersBranchEndpoints(t *testing.T) {
	d := workflow.ParseDocument([]byte(`processes:
  start: {command: echo start}
  a: {command: echo a, depends_on: {start: {condition: process_completed_successfully}}}
  b: {command: echo b, depends_on: {start: {condition: process_completed_successfully}}}
  end: {command: echo end, depends_on: {a: {condition: process_completed_successfully}, b: {condition: process_completed_successfully}}}
`), "yaml", nil)
	nodes, _, _, height := layout(d.View)
	positions := map[string]positioned{}
	for _, n := range nodes {
		positions[n.Name] = n
		if n.Y+144 > height {
			t.Fatal("graph clips the node card")
		}
	}
	center := (positions["a"].Y + positions["b"].Y) / 2
	if positions["start"].Y != center || positions["end"].Y != center {
		t.Fatalf("branch source and sink must be centered: %+v", positions)
	}
	if positions["b"].Y-positions["a"].Y < 144+24 {
		t.Fatal("branch node cards overlap or leave no readable gap")
	}
}

func TestRenderPreservesDeclarationAndEscapesText(t *testing.T) {
	source := []byte("processes:\n  prepare: {command: 'echo </script><script>alert(1)</script>'}\n  test: {command: echo ok, depends_on: {prepare: {condition: process_completed_successfully}}}\n")
	d := workflow.ParseDocument(source, "yaml", []byte(`{"version":1,"title":"中文 🐈 <img src=x onerror=alert(1)>"}`))
	page, err := Render(d, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	html := string(page)
	for _, want := range []string{"RunCradle", "未执行", "process_completed_successfully", "workflow-data", "Content-Security-Policy", "源文件"} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q", want)
		}
	}
	for _, bad := range []string{"<img src=x onerror", "<script>alert(1)", "https://", "http://localhost", "fetch("} {
		if strings.Contains(html, bad) {
			t.Fatalf("unsafe or external content: %q", bad)
		}
	}
	again, _ := Render(d, time.Unix(0, 0))
	if !bytes.Equal(page, again) {
		t.Fatal("nondeterministic render")
	}
}
func TestRenderEmptyAndRejectInvalid(t *testing.T) {
	empty := workflow.ParseDocument([]byte("processes: {}"), "yaml", nil)
	page, err := Render(empty, time.Unix(0, 0))
	if err != nil || !strings.Contains(string(page), "暂无节点") {
		t.Fatalf("bad empty render: %v", err)
	}
	invalid := workflow.ParseDocument([]byte("processes: {a: {command: x, depends_on: {missing: {}}}}"), "yaml", nil)
	if _, err = Render(invalid, time.Unix(0, 0)); err == nil {
		t.Fatal("rendered invalid graph")
	}
}

func TestRawSourceIsPackagedWithoutNewlineNormalization(t *testing.T) {
	source := []byte("\r\nprocesses: {}\r\n# 中文\r\n")
	d := workflow.ParseDocument(source, "yaml", nil)
	html, err := Render(d, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	marker := []byte(`<script type="application/json" id="workflow-data">`)
	_, data, ok := bytes.Cut(html, marker)
	if !ok {
		t.Fatal("missing structured page data")
	}
	data, _, ok = bytes.Cut(data, []byte(`</script>`))
	if !ok {
		t.Fatal("unterminated data")
	}
	var decoded struct {
		Source string `json:"source"`
	}
	if err = json.Unmarshal(data, &decoded); err != nil || decoded.Source != string(source) {
		t.Fatalf("raw source not recoverable: %v %q", err, decoded.Source)
	}
}

func TestUnknownSpecialScalarsRemainViewable(t *testing.T) {
	d := workflow.ParseDocument([]byte("processes: {a: {command: echo ok, custom: .nan}}"), "yaml", nil)
	if !d.Valid() {
		t.Fatalf("lost source: %+v", d.Issues)
	}
	if _, err := Render(d, time.Unix(0, 0)); err != nil {
		t.Fatalf("unknown native scalar broke rendering: %v", err)
	}
}

func TestDisplayProjectionKeepsExactNumbers(t *testing.T) {
	source := []byte(`{"processes":{"a":{"command":"echo ok","integer":18446744073709551617,"decimal":0.12345678901234567890123456789,"scientific":1.234567890123456789e+200}}}`)
	ui := []byte(`{"version":1,"inputs":[{"name":"count","type":"integer","default":9223372036854775807,"enum":[9223372036854775807],"bindings":[{"process":"a","field":"environment","key":"COUNT"}]},{"name":"low","type":"integer","default":-9223372036854775808,"bindings":[{"process":"a","field":"environment","key":"LOW"}]}],"presets":[{"id":"max","values":{"count":9223372036854775807}}]}`)
	page, err := Render(workflow.ParseDocument(source, "json", ui), time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	_, data, _ := bytes.Cut(page, []byte(`<script type="application/json" id="workflow-data">`))
	data, _, _ = bytes.Cut(data, []byte(`</script>`))
	var projection struct {
		View struct {
			Nodes []struct {
				Text map[string]string `json:"declaration_text"`
			}
		}
		Metadata struct {
			Inputs []struct {
				Default string   `json:"default_text"`
				Enum    []string `json:"enum_text"`
			}
			Presets []struct {
				Values string `json:"values_text"`
			}
		}
	}
	if err := json.Unmarshal(data, &projection); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"integer": "18446744073709551617", "decimal": "0.12345678901234567890123456789", "scientific": "1.234567890123456789e+200"} {
		if projection.View.Nodes[0].Text[key] != want {
			t.Fatalf("display lost %s", key)
		}
	}
	if projection.Metadata.Inputs[0].Default != "9223372036854775807" || projection.Metadata.Inputs[0].Enum[0] != "9223372036854775807" || projection.Metadata.Inputs[1].Default != "-9223372036854775808" || !strings.Contains(projection.Metadata.Presets[0].Values, "9223372036854775807") {
		t.Fatal("UI display lost int64 precision")
	}
}

func TestRenderRejectsOversizedHTML(t *testing.T) {
	d := workflow.ParseDocument([]byte("processes: {a: {command: echo ok}}"), "yaml", nil)
	d.View.Nodes[0].Declaration["custom"] = strings.Repeat("\x00", 6<<20)
	if _, err := Render(d, time.Unix(0, 0)); err == nil {
		t.Fatal("HTML export larger than 32 MiB accepted")
	}
}

func TestWorkbenchProjection(t *testing.T) {
	d := workflow.ParseDocument([]byte("processes:\n  a:\n    command: echo hello\n    environment: [PORT=8080]\n    readiness_probe: {exec: {command: 'true'}}\n  b:\n    command: echo b\n    depends_on: {a: {condition: process_healthy}}\n"), "yaml", []byte(`{"version":1,"inputs":[{"name":"port","type":"integer","default":8080,"bindings":[{"process":"a","field":"environment","key":"PORT"}]}]}`))
	raw, e := Display(d)
	if e != nil {
		t.Fatal(e)
	}
	for _, part := range []string{`"role":"service"`, `"command_preview":"echo hello"`, `"bound_input":"port"`, `"upstream_ids":["node-`, `"downstream_ids":["node-`, `"FromName":"a"`, `"exit_on_skipped":false`} {
		if !strings.Contains(string(raw), part) {
			t.Fatalf("missing %s in %s", part, raw)
		}
	}
	again, _ := Display(d)
	if string(raw) != string(again) {
		t.Fatal("nondeterministic display")
	}
}

func TestRenderedInlineAssetsMatchCSP(t *testing.T) {
	d := workflow.ParseDocument([]byte("processes: {a: {command: echo ok}}"), "yaml", nil)
	raw, e := Render(d, time.Unix(0, 0))
	if e != nil {
		t.Fatal(e)
	}
	for _, tag := range []string{"script", "style"} {
		re := regexp.MustCompile("(?s)<" + tag + ">(.*?)</" + tag + ">")
		match := re.FindSubmatch(raw)
		if len(match) != 2 {
			t.Fatalf("missing %s", tag)
		}
		sum := sha256.Sum256(match[1])
		hash := base64.StdEncoding.EncodeToString(sum[:])
		if !strings.Contains(html.UnescapeString(string(raw)), "sha256-"+hash) {
			t.Errorf("%s actual bytes do not match CSP hash", tag)
		}
	}
}
