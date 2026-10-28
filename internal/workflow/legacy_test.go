package workflow

import (
	"bytes"
	"strings"
	"testing"
)

func TestLegacyV2RequiresExplicitMigrationWithoutRewriting(t *testing.T) {
	raw := []byte(`{"version":2,"workspace":".","tasks":{"check":{"service":{"argv":["server"]},"task":{"argv":["check"]}}}}`)
	d := ParseDocument(raw, "json", nil)
	if d.Valid() || !bytes.Equal(d.Source, raw) {
		t.Fatal("legacy policy accepted or changed")
	}
	for _, issue := range d.Issues {
		if issue.Code == "legacy_v2" && issue.Path == "$.tasks" && strings.Contains(issue.Message, "W3_MIGRATION") {
			return
		}
	}
	t.Fatalf("missing concrete migration guidance: %+v", d.Issues)
}
