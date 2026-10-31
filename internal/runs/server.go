package runs

import (
	"context"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
)

// ServerActive is only a liveness prerequisite for connecting; it grants no run ownership.
func (s *Store) ServerActive() (bool, error) {
	f, e := s.root.OpenFile(".runcradle/server/owner.lock", os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if e != nil {
		return false, e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm() != 0600 {
		return false, fmt.Errorf("invalid server lock")
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

type ServerDescriptor struct {
	Version    int    `json:"schema_version"`
	URL        string `json:"url"`
	InstanceID string `json:"instance_id"`
	Workspace  string `json:"workspace"`
	Token      string `json:"token"`
}

func (s *Store) ClaimServer(ctx context.Context) (func() error, error) {
	if e := private(s.root, ".runcradle/server", true); e != nil {
		return nil, e
	}
	return s.lock(ctx, ".runcradle/server/owner.lock")
}
func (s *Store) TryClaimServer() (func() error, error) {
	if err := private(s.root, ".runcradle/server", true); err != nil {
		return nil, err
	}
	return s.tryLock(".runcradle/server/owner.lock")
}
func (s *Store) PublishServer(d ServerDescriptor) error {
	if d.Workspace != s.workspace || d.Version != 1 || !validID(d.InstanceID, 32) || !validID(d.Token, 64) {
		return fmt.Errorf("invalid server descriptor")
	}
	return s.write(".runcradle/server/connection.json", d)
}
func (s *Store) Server() (ServerDescriptor, error) {
	var d ServerDescriptor
	if e := private(s.root, ".runcradle/server", false); e != nil {
		return d, e
	}
	e := s.read(".runcradle/server/connection.json", &d, 16384)
	if e == nil && (d.Version != 1 || d.Workspace != s.workspace || !validID(d.InstanceID, 32) || !validID(d.Token, 64)) {
		e = fmt.Errorf("invalid server descriptor")
	}
	return d, e
}
func (s *Store) ClearServer(instance string) error {
	d, e := s.Server()
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if d.InstanceID != instance {
		return fmt.Errorf("server instance changed")
	}
	return s.root.Remove(".runcradle/server/connection.json")
}
