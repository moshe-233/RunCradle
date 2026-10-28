package nodeexec

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestRejectNonRegularReceiptWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	if e := syscall.Mkfifo(filepath.Join(dir, "fifo.json"), 0600); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, e := ReadReceipts(dir, "run"); done <- e }()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("FIFO accepted")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("receipt reader blocked on a FIFO")
	}
}
