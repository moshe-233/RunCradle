package workflow

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"sort"

	"gopkg.in/yaml.v3"
)

const MaxSource = 1 << 20
const MaxUI = 256 << 10
const MaxExpanded = 8 << 20

type Issue struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Path     string `json:"path,omitempty"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
	Message  string `json:"message"`
}
type Node struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Label       string         `json:"label"`
	Declaration map[string]any `json:"declaration"`
}
type Edge struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Condition string `json:"condition"`
}
type View struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Nodes       []Node `json:"nodes"`
	Edges       []Edge `json:"edges"`
	Complete    bool   `json:"complete"`
	RawEditOnly bool   `json:"raw_edit_only"`
}
type Document struct {
	Workspace string
	Entry     string
	UIPath    string
	Source    []byte
	UIRaw     []byte
	Format    string
	AST       *yaml.Node
	Native    map[string]any
	Metadata  *Metadata
	View      View
	Issues    []Issue
	Revision  string
}

func (d Document) Valid() bool {
	for _, i := range d.Issues {
		if i.Severity == "error" {
			return false
		}
	}
	return true
}
func (d *Document) problem(code, path, message string, n *yaml.Node) {
	i := Issue{Severity: "error", Code: code, Path: path, Message: message}
	if n != nil {
		i.Line = n.Line
		i.Column = n.Column
	}
	d.Issues = append(d.Issues, i)
}
func (d *Document) deferred(path, message string) {
	d.View.Complete = false
	d.Issues = append(d.Issues, Issue{Severity: "warning", Code: "unresolved", Path: path, Message: message})
}
func ComputeRevision(files map[string][]byte) string {
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	h.Write([]byte("runcradle-definition-v1\x00"))
	for _, k := range keys {
		for _, v := range [][]byte{[]byte(k), files[k]} {
			var size [8]byte
			binary.BigEndian.PutUint64(size[:], uint64(len(v)))
			h.Write(size[:])
			h.Write(v)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}
