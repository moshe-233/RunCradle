package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"runcradle/internal/workflow"
)

// BindSource binds W2's path-based identities to immutable definition bytes.
// These are record identifiers, never authority to control a process.
func BindSource(src WorkflowSource) (WorkflowSource, error) {
	d := src.Document
	files := map[string][]byte{"format": []byte(d.Format), "native/" + filepath.ToSlash(d.Entry): d.Source}
	if d.UIPath != "" {
		files["ui/"+filepath.ToSlash(d.UIPath)] = d.UIRaw
	}
	if !filepath.IsAbs(d.Workspace) || d.Entry == "" || d.Revision != workflow.ComputeRevision(files) || src.Revision != "" && src.Revision != d.Revision {
		return src, fmt.Errorf("source revision/path identity mismatch")
	}
	workspace := fmt.Sprintf("%x", sha256.Sum256([]byte(d.Workspace)))
	workflowID := fmt.Sprintf("%x", sha256.Sum256([]byte(workspace+"\x00"+filepath.ToSlash(d.Entry))))
	if src.Managed {
		if _, err := hex.DecodeString(src.WorkflowID); err != nil || len(src.WorkflowID) != 64 {
			return src, fmt.Errorf("invalid managed workflow identity")
		}
		workflowID = src.WorkflowID
	}
	if src.WorkspaceID != "" && src.WorkspaceID != workspace || src.WorkflowID != "" && src.WorkflowID != workflowID {
		return src, fmt.Errorf("source workspace/workflow identity mismatch")
	}
	src.WorkspaceID, src.WorkflowID, src.Revision = workspace, workflowID, d.Revision
	return src, nil
}
