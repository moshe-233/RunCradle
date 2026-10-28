package workflow

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestExportProtectsInputsAndExistingOutput(t *testing.T) {
	dir := t.TempDir()
	source := []byte("processes: {}")
	os.WriteFile(filepath.Join(dir, "source.yaml"), source, 0600)
	d, err := Load(dir, "source.yaml", "")
	if err != nil {
		t.Fatal(err)
	}
	os.Link(filepath.Join(dir, "source.yaml"), filepath.Join(dir, "linked.html"))
	os.Symlink("source.yaml", filepath.Join(dir, "sym.html"))
	for _, name := range []string{"source.yaml", "linked.html", "sym.html", "../out.html"} {
		if _, err := WriteOutput(d, name, []byte("unsafe"), true); err == nil {
			t.Fatalf("overwrote input/escaped: %s", name)
		}
	}
	if _, err := WriteOutput(d, "out.html", []byte("first"), false); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteOutput(d, "out.html", []byte("second"), false); err == nil {
		t.Fatal("silently overwrote output")
	}
	if _, err := WriteOutput(d, "out.html", []byte("second"), true); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "source.yaml"))
	if !bytes.Equal(got, source) {
		t.Fatal("source mutated")
	}
	got, _ = os.ReadFile(filepath.Join(dir, "out.html"))
	if string(got) != "second" {
		t.Fatal("force did not replace")
	}
	st, _ := os.Stat(filepath.Join(dir, "out.html"))
	if st.Mode().Perm() != 0600 {
		t.Fatalf("unsafe mode %v", st.Mode())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 4 {
		t.Fatalf("temporary residue: %v", entries)
	}
}
