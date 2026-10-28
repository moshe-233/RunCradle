package processcompose

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
)

//go:embed pinned.json
var pinnedJSON []byte

type EnginePin struct {
	Version       string `json:"version"`
	SourceCommit  string `json:"source_commit"`
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	ArchiveURL    string `json:"archive_url"`
	ArchiveSHA256 string `json:"archive_sha256"`
	BinarySHA256  string `json:"binary_sha256"`
}

func PinnedEngine() (EnginePin, error) { return parsePin(pinnedJSON) }

func parsePin(raw []byte) (EnginePin, error) {
	var pin EnginePin
	err := json.Unmarshal(raw, &pin)
	sha := regexp.MustCompile(`^[0-9a-f]{64}$`)
	commit := regexp.MustCompile(`^[0-9a-f]{40}$`)
	if err != nil || pin.Version != Version || pin.OS != "linux" || pin.Arch != "amd64" || pin.ArchiveURL == "" || !commit.MatchString(pin.SourceCommit) || !sha.MatchString(pin.ArchiveSHA256) || !sha.MatchString(pin.BinarySHA256) {
		return EnginePin{}, fmt.Errorf("%w: invalid embedded Process Compose identity", ErrInternal)
	}
	return pin, nil
}
