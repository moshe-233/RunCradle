package workflow

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestLiteralMergeNamesAndFields(t *testing.T) {
	for _, source := range []string{
		`{"version":"0.5","processes":{"<<":{"command":"echo ok","<<":"literal"}}}`,
		"version: '0.5'\nprocesses:\n  \"<<\": {command: echo ok, \"<<\": literal}\n",
	} {
		d := ParseDocument([]byte(source), "yaml", nil)
		if !d.Valid() || len(d.View.Nodes) != 1 || d.View.Nodes[0].Name != "<<" || d.View.Nodes[0].Declaration["<<"] != "literal" || d.View.RawEditOnly {
			t.Fatalf("ordinary << key was merged: %+v %+v", d.View, d.Issues)
		}
	}
}

func TestExpandedScalarBytesAreBoundedBeforeProjection(t *testing.T) {
	source := "processes: {}\nx: &x " + strings.Repeat("a", 16384) + "\ny: [" + strings.Repeat("*x,", 512) + "]\n"
	d := ParseDocument([]byte(source), "yaml", nil)
	if d.Valid() || !hasCode(d, "limit") || d.Native != nil {
		t.Fatal("large alias byte expansion reached projection")
	}
}

func TestNonUTF8InputsAreRejected(t *testing.T) {
	utf16 := append([]byte{0xff, 0xfe}, bytes.Join(bytes.Split([]byte("processes: {}"), nil), []byte{0})...)
	utf16 = append(utf16, 0)
	if d := ParseDocument(utf16, "yaml", nil); d.Valid() || !hasCode(d, "encoding") {
		t.Fatal("UTF-16 source accepted")
	}
	if d := ParseDocument([]byte("processes: {}"), "yaml", []byte("{\"version\":1,\"title\":\"\xff\"}")); d.Valid() || !hasCode(d, "encoding") {
		t.Fatal("invalid UTF-8 metadata accepted")
	}
}

func TestNativeNumbersKeepExactDecimalLexemes(t *testing.T) {
	source := `{"processes":{"a":{"command":"echo ok","integer":18446744073709551617,"decimal":0.12345678901234567890123456789,"scientific":1.234567890123456789e+200}}}`
	d := ParseDocument([]byte(source), "json", nil)
	if !d.Valid() {
		t.Fatal(d.Issues)
	}
	raw, err := json.Marshal(d.View.Nodes[0].Declaration)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"18446744073709551617", "0.12345678901234567890123456789", "1.234567890123456789e+200"} {
		if !bytes.Contains(raw, []byte(want)) {
			t.Fatalf("lost numeric literal %s in %s", want, raw)
		}
	}
}

func TestMetadataRejectsCaseAliasesAndNullShapes(t *testing.T) {
	for _, ui := range []string{
		`{"version":2,"Version":1}`,
		`{"Version":1}`, `{"version":1,"Title":"x"}`,
		`{"version":1,"title":null}`, `{"version":1,"nodes":{"a":null}}`,
		`{"version":1,"nodes":null}`, `{"version":1,"inputs":null}`,
		`{"version":1,"inputs":[null]}`, `{"version":1,"presets":[{"id":"empty","values":null}]}`,
	} {
		if d := ParseDocument([]byte("processes: {a: {command: echo ok}}"), "yaml", []byte(ui)); d.Valid() {
			t.Fatalf("accepted noncanonical metadata shape: %s", ui)
		}
	}
}
