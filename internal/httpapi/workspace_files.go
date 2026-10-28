package httpapi

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"
)

type workspaceFiles struct {
	Files     []string `json:"files"`
	UIFiles   []string `json:"ui_files"`
	Truncated bool     `json:"truncated"`
}

// listWorkspaceFiles bounds directory reads as well as the returned candidate set.
func (s *Server) listWorkspaceFiles(w http.ResponseWriter, r *http.Request) {
	root, e := os.OpenRoot(s.workspace)
	if e != nil {
		apiError(w, e)
		return
	}
	defer root.Close()
	out := workspaceFiles{Files: []string{}, UIFiles: []string{}}
	visited := 0
	var walk func(string, int) error
	walk = func(dir string, depth int) error {
		if e := r.Context().Err(); e != nil {
			return e
		}
		if visited >= 2000 {
			out.Truncated = true
			return nil
		}
		f, e := root.Open(dir)
		if e != nil {
			if errors.Is(e, os.ErrPermission) {
				out.Truncated = true
				return nil
			}
			return e
		}
		defer f.Close()
		// Charge buffered entries before recursing so all directory reads share one budget.
		budget := 2000 - visited
		entries, e := f.ReadDir(budget)
		if e != nil && !errors.Is(e, io.EOF) {
			return e
		}
		visited += len(entries)
		if len(entries) == budget {
			// Without another read we cannot establish EOF; report conservative truncation.
			out.Truncated = true
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			if len(out.Files)+len(out.UIFiles) >= 200 {
				out.Truncated = true
				return nil
			}
			name := entry.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			rel := path.Join(dir, name)
			if entry.IsDir() {
				if depth >= 6 {
					out.Truncated = true
					continue
				}
				if e := walk(rel, depth+1); e != nil {
					return e
				}
				continue
			}
			if !entry.Type().IsRegular() {
				continue
			}
			ext := strings.ToLower(path.Ext(name))
			if ext != ".yaml" && ext != ".yml" && ext != ".json" {
				continue
			}
			if name == "ui.json" {
				out.UIFiles = append(out.UIFiles, rel)
			} else {
				out.Files = append(out.Files, rel)
			}
		}
		return nil
	}
	if e = walk(".", 0); e != nil {
		apiError(w, e)
		return
	}
	sort.Strings(out.Files)
	sort.Strings(out.UIFiles)
	reply(w, 200, out)
}
