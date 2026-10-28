package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestManyEnvironmentBindingsApplyWithinAdmissionBudget(t *testing.T) {
	source := []byte("processes: {a: {command: echo ok}}\nx-padding: '" + strings.Repeat("z", 250000) + "'\nx-number: 9223372036854775807\n")
	meta := Metadata{Version: 1}
	for i := 0; i < 300; i++ {
		meta.Inputs = append(meta.Inputs, Input{Name: fmt.Sprintf("p%d", i), Type: "integer", Default: json.RawMessage("9223372036854775807"), Bindings: []Binding{{Process: "a", Field: "environment", Key: fmt.Sprintf("K%d", i)}}})
	}
	ui, e := json.Marshal(meta)
	if e != nil {
		t.Fatal(e)
	}
	d := ParseDocument(source, "yaml", ui)
	if !d.Valid() {
		t.Fatal(d.Issues)
	}
	start := time.Now()
	effective, e := ApplyValues(d, nil)
	elapsed := time.Since(start)
	if e != nil || elapsed > 4*time.Second {
		t.Fatalf("parameter application consumed admission budget: %v %v", elapsed, e)
	}
	if !bytes.Equal(source, d.Source) || !bytes.Contains(effective.Source, []byte("K299=9223372036854775807")) || !bytes.Contains(effective.Source, []byte("9223372036854775807")) {
		t.Fatal("base mutated or exact value lost")
	}
}

func TestParameterApplicationObservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d := ParseDocument([]byte("processes: {}"), "yaml", nil)
	if _, e := ApplyValuesContext(ctx, d, nil); !errors.Is(e, context.Canceled) {
		t.Fatalf("cancelled application: %v", e)
	}
}

func TestBatchEnvironmentPreservesJSONNumbersAndYAMLComments(t *testing.T) {
	meta := valueDocument().UIRaw
	for _, tc := range []struct{ format, source, retained string }{
		{"json", `{"processes":{"a":{"command":"echo ok","environment":["OTHER=keep"],"custom":9223372036854775807}},"extra":0.1234567890123456789}`, "0.1234567890123456789"},
		{"yaml", "# original\nprocesses:\n  a:\n    command: echo ok\n    custom: 9223372036854775807\n    environment:\n      - OTHER=keep # preserve entry comment\n", "# preserve entry comment"},
	} {
		d := ParseDocument([]byte(tc.source), tc.format, meta)
		next, e := ApplyValues(d, nil)
		if e != nil || !bytes.Equal(d.Source, []byte(tc.source)) || !bytes.Contains(next.Source, []byte(tc.retained)) || !bytes.Contains(next.Source, []byte("9223372036854775807")) || !bytes.Contains(next.Source, []byte("OTHER=keep")) {
			t.Fatalf("batch fidelity %s: %v %s", tc.format, e, next.Source)
		}
	}
}
