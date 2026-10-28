package workflow

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrOutputPath = errors.New("invalid output path")

func WriteOutput(d Document, name string, content []byte, force bool) (relative string, err error) {
	root, err := os.OpenRoot(d.Workspace)
	if err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	relative, err = Relative(d.Workspace, name)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrOutputPath, err)
	}
	target, statErr := root.Stat(relative)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return "", fmt.Errorf("%w: %v", ErrOutputPath, statErr)
	}
	for _, input := range []string{d.Entry, d.UIPath} {
		if input == "" {
			continue
		}
		st, e := root.Stat(input)
		if e != nil {
			return "", e
		}
		if relative == input || (target != nil && os.SameFile(target, st)) {
			return "", fmt.Errorf("%w: cannot overwrite input", ErrOutputPath)
		}
	}
	if target != nil && !force {
		return "", fmt.Errorf("%w: output exists; use --force", ErrOutputPath)
	}
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		return "", err
	}
	tmp := filepath.Join(filepath.Dir(relative), fmt.Sprintf(".runcradle-export-%x", random))
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	defer func() {
		if e := root.Remove(tmp); e != nil && !errors.Is(e, os.ErrNotExist) {
			err = errors.Join(err, e)
		}
	}()
	_, writeErr := f.Write(content)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err = errors.Join(writeErr, syncErr, closeErr); err != nil {
		return "", err
	}
	if force {
		err = root.Rename(tmp, relative)
	} else {
		err = root.Link(tmp, relative)
	}
	if errors.Is(err, os.ErrExist) {
		return "", fmt.Errorf("%w: output exists; use --force", ErrOutputPath)
	}
	if err != nil {
		return "", err
	}
	dir, e := root.Open(filepath.Dir(relative))
	if e != nil {
		return "", e
	}
	err = errors.Join(dir.Sync(), dir.Close())
	return relative, err
}
