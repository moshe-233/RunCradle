package workflow

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func Relative(workspace, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("file path is required")
	}
	full := name
	if !filepath.IsAbs(full) {
		full = filepath.Join(workspace, full)
	}
	rel, err := filepath.Rel(workspace, full)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path must name a file inside the workspace")
	}
	return rel, nil
}
func ReadFile(workspace, name string, limit int64) (data []byte, rel string, err error) {
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return nil, "", err
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	rel, err = Relative(workspace, name)
	if err != nil {
		return nil, "", err
	}
	file, err := root.OpenFile(rel, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, "", err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	info, err := file.Stat()
	if err != nil {
		return nil, "", err
	}
	if !info.Mode().IsRegular() {
		return nil, "", fmt.Errorf("input must be a regular file")
	}
	data, err = io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(data)) > limit {
		return nil, "", fmt.Errorf("input exceeds %d bytes", limit)
	}
	if data == nil {
		data = []byte{}
	}
	return data, rel, nil
}
func Load(workspace, file, ui string) (Document, error) {
	absolute, err := filepath.Abs(workspace)
	if err != nil {
		return Document{}, err
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return Document{}, err
	}
	source, entry, err := ReadFile(absolute, file, MaxSource)
	if err != nil {
		return Document{}, err
	}
	var metadata []byte
	var uiPath string
	if ui != "" {
		metadata, uiPath, err = ReadFile(absolute, ui, MaxUI)
		if err != nil {
			return Document{}, err
		}
	}
	format := strings.TrimPrefix(strings.ToLower(filepath.Ext(entry)), ".")
	if format == "yml" {
		format = "yaml"
	}
	d := ParseDocument(source, format, metadata)
	d.Workspace = absolute
	d.Entry = entry
	d.UIPath = uiPath
	files := map[string][]byte{"format": []byte(format), "native/" + filepath.ToSlash(entry): source}
	if ui != "" {
		files["ui/"+filepath.ToSlash(uiPath)] = metadata
	}
	d.Revision = ComputeRevision(files)
	return d, nil
}
