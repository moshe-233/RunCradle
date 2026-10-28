package processcompose

import (
	"encoding/json"
	"testing"
)

func TestPinnedEngineIdentity(t *testing.T) {
	p, err := PinnedEngine()
	if err != nil {
		t.Fatal(err)
	}
	if p.Version != "v1.122.0" || p.SourceCommit != "673850dd20683ef14b33890444ca3416d02c751c" || p.OS != "linux" || p.Arch != "amd64" || p.ArchiveSHA256 != "9b6dbc38324c0b0481f1cd1dd828ffdc78117129ec797678f4bf8c4023311281" || p.BinarySHA256 != "7127df85c0d0c62f2f79b4d2b3082ca1d85f7c0c74523457617861d32150b0dd" {
		t.Fatalf("incorrect official identity: %+v", p)
	}
	raw, _ := json.Marshal(p)
	if _, err := parsePin(raw); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*EnginePin){
		func(p *EnginePin) { p.Version = "v0.0.0" }, func(p *EnginePin) { p.ArchiveURL = "" },
		func(p *EnginePin) { p.BinarySHA256 = "XX" }, func(p *EnginePin) { p.SourceCommit = "" },
		func(p *EnginePin) { p.OS = "" }, func(p *EnginePin) { p.Arch = "" },
		func(p *EnginePin) { p.ArchiveSHA256 = "" },
	} {
		bad := p
		mutate(&bad)
		raw, _ := json.Marshal(bad)
		if _, err := parsePin(raw); err == nil {
			t.Fatalf("accepted invalid pin: %s", raw)
		}
	}
	if _, err := parsePin([]byte("{")); err == nil {
		t.Fatal("accepted invalid JSON")
	}
}
