package nodeexec

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
)

func ReadReceipts(dir, runID string) ([]Receipt, error) {
	root, e := os.OpenRoot(dir)
	if e != nil {
		return nil, e
	}
	defer root.Close()
	f, e := root.Open(".")
	if e != nil {
		return nil, e
	}
	defer f.Close()
	entries, e := f.ReadDir(4097)
	if e != nil && e != io.EOF {
		return nil, e
	}
	if len(entries) > 4096 {
		return nil, fmt.Errorf("receipt count limit")
	}
	var receipts []Receipt
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		file, e := root.OpenFile(entry.Name(), os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if e != nil {
			return nil, e
		}
		st, e := file.Stat()
		if e != nil || !st.Mode().IsRegular() {
			file.Close()
			return nil, fmt.Errorf("receipt is not a regular file")
		}
		raw, e := io.ReadAll(io.LimitReader(file, 16385))
		file.Close()
		if e != nil || len(raw) > 16384 {
			return nil, fmt.Errorf("receipt read limit/error")
		}
		var r Receipt
		if json.Unmarshal(raw, &r) != nil || r.Version != 1 || r.RunID != runID || r.NodeID == "" || r.Incarnation+".json" != entry.Name() || r.HelperPID < 1 || r.GroupID < 1 {
			return nil, fmt.Errorf("invalid receipt identity")
		}
		receipts = append(receipts, r)
	}
	return receipts, nil
}
