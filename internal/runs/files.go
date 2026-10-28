package runs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func validID(s string, n int) bool {
	if len(s) != n {
		return false
	}
	_, e := hex.DecodeString(s)
	return e == nil
}
func nonce() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return ""
	}
	return fmt.Sprintf("%x", b)
}
func private(root *os.Root, name string, create bool) error {
	parts := strings.Split(filepath.Clean(name), string(os.PathSeparator))
	current := ""
	for _, part := range parts {
		current = filepath.Join(current, part)
		st, e := root.Lstat(current)
		if errors.Is(e, os.ErrNotExist) && create {
			if e = root.Mkdir(current, 0700); e != nil && !errors.Is(e, os.ErrExist) {
				return e
			}
			st, e = root.Lstat(current)
		}
		if e != nil {
			return e
		}
		if !st.IsDir() || st.Mode().Perm() != 0700 {
			return fmt.Errorf("private state must be an ordinary 0700 directory")
		}
		if id, ok := st.Sys().(*syscall.Stat_t); !ok || id.Uid != uint32(os.Getuid()) {
			return fmt.Errorf("private directory owner mismatch")
		}
	}
	return nil
}
func (s *Store) read(name string, value any, limit int64) error {
	f, e := s.root.OpenFile(name, os.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW, 0)
	if e != nil {
		return e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return e
	}
	if !st.Mode().IsRegular() || st.Mode().Perm() != 0600 {
		return fmt.Errorf("private record must be ordinary 0600 file")
	}
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil {
		return e
	}
	if int64(len(b)) > limit {
		return fmt.Errorf("record exceeds limit")
	}
	return json.Unmarshal(b, value)
}
func (s *Store) write(name string, value any) error {
	raw, e := json.Marshal(value)
	if e != nil {
		return e
	}
	if len(raw) > 2<<20 {
		return fmt.Errorf("record exceeds limit")
	}
	id := nonce()
	if id == "" {
		return fmt.Errorf("random identity unavailable")
	}
	tmp := filepath.Join(filepath.Dir(name), ".write-"+id)
	f, e := s.root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer s.root.Remove(tmp)
	_, w := f.Write(raw)
	e = errors.Join(w, f.Sync(), f.Close())
	if e != nil {
		return e
	}
	if e = s.root.Rename(tmp, name); e != nil {
		return e
	}
	d, e := s.root.Open(filepath.Dir(name))
	if e != nil {
		return e
	}
	return errors.Join(d.Sync(), d.Close())
}
func (s *Store) lock(ctx context.Context, name string) (func() error, error) {
	f, e := s.root.OpenFile(name, os.O_CREATE|os.O_RDWR|unix.O_NONBLOCK|unix.O_NOFOLLOW, 0600)
	if e != nil {
		return nil, e
	}
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm() != 0600 {
		f.Close()
		return nil, fmt.Errorf("invalid private lock")
	}
	for {
		if e = ctx.Err(); e != nil {
			f.Close()
			return nil, e
		}
		e = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if e == nil {
			return func() error { return errors.Join(unix.Flock(int(f.Fd()), unix.LOCK_UN), f.Close()) }, nil
		}
		if e != unix.EWOULDBLOCK && e != unix.EAGAIN {
			f.Close()
			return nil, e
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}
