package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runcradle/internal/workflow"
	"strings"
)

// apiFile reserves the output before a remote mutation can take place. It never
// overwrites files, follows output symlinks, or writes into product/Git state.
type apiFile struct {
	root *os.Root
	file *os.File
	name string
}

func reserveAPIFile(workspace, name string) (*apiFile, error) {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return nil, err
	}
	rel, err := workflow.Relative(abs, name)
	if err != nil {
		return nil, err
	}
	if len(rel) > 512 {
		return nil, fmt.Errorf("output path exceeds 512 bytes")
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(rel, string(os.PathSeparator))
	for i, part := range parts {
		if part == ".runcradle" || part == ".git" {
			return nil, errors.Join(fmt.Errorf("output must not write private product or Git state"), root.Close())
		}
		if i < len(parts)-1 {
			info, err := root.Lstat(filepath.Join(parts[:i+1]...))
			if err != nil || !info.IsDir() {
				return nil, errors.Join(fmt.Errorf("output parent must be an existing directory without symlinks"), err, root.Close())
			}
		}
	}
	f, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, errors.Join(err, root.Close())
	}
	return &apiFile{root, f, rel}, nil
}

func (f *apiFile) abort() error {
	return errors.Join(f.file.Close(), f.root.Remove(f.name), f.root.Close())
}

func (f *apiFile) finish(raw []byte) error {
	_, writeErr := f.file.Write(raw)
	err := errors.Join(writeErr, f.file.Sync(), f.file.Close())
	if err != nil {
		err = errors.Join(err, f.root.Remove(f.name))
	}
	return errors.Join(err, f.root.Close())
}
