package runs

import (
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"sync"
)

// A workspace permits four concurrent run owners; each engine instance
// still supports the independently bounded native multi-node workflow.
func (s *Store) Lease(id string) (func() error, error) {
	dir, e := s.path(id)
	if e != nil {
		return nil, e
	}
	owner, e := s.tryLock(filepath.Join(dir, "owner.lock"))
	if e != nil {
		return nil, e
	}
	for slot := 0; slot < 4; slot++ {
		release, e := s.tryLock(fmt.Sprintf(".runcradle/locks/slot-%d.lock", slot))
		if e == nil {
			var once sync.Once
			var result error
			return func() error { once.Do(func() { result = errors.Join(release(), owner()) }); return result }, nil
		}
		if !errors.Is(e, ErrBusy) {
			owner()
			return nil, e
		}
	}
	owner()
	return nil, fmt.Errorf("workspace has four active runs; inspect or wait for an existing run")
}

// Lock observation never grants authority to signal or adopt a recorded PID.
func (s *Store) Active(id string) (bool, error) {
	dir, e := s.path(id)
	if e != nil {
		return false, e
	}
	f, e := s.root.OpenFile(filepath.Join(dir, "owner.lock"), os.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW, 0)
	if errors.Is(e, os.ErrNotExist) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() {
		return false, fmt.Errorf("invalid owner lock")
	}
	e = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if e == unix.EWOULDBLOCK || e == unix.EAGAIN {
		return true, nil
	}
	if e != nil {
		return false, e
	}
	return false, unix.Flock(int(f.Fd()), unix.LOCK_UN)
}
