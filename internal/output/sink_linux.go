//go:build linux

// SPDX-License-Identifier: Apache-2.0
package output

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const DefaultWait = 2 * time.Second

// ContextWriter must return when ctx expires. Arbitrary io.Writer callbacks
// cannot be interrupted safely, so they are rejected before a task starts.
type ContextWriter interface {
	WriteContext(context.Context, []byte) (int, error)
}

type Sink struct {
	w        io.Writer
	custom   ContextWriter
	fd       int
	socket   bool
	gate     chan struct{}
	wait     time.Duration
	close    sync.Once
	closeErr error
}

type Sinks struct{ Out, Err *Sink }

func New(stdout, stderr io.Writer, wait time.Duration) (*Sinks, error) {
	if wait <= 0 {
		wait = DefaultWait
	}
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	out, err := newSink(stdout, wait, gate)
	if err != nil {
		return nil, err
	}
	errOut, err := newSink(stderr, wait, gate)
	if err != nil {
		out.Close()
		return nil, err
	}
	return &Sinks{out, errOut}, nil
}

func newSink(w io.Writer, wait time.Duration, gate chan struct{}) (*Sink, error) {
	if w == nil {
		w = io.Discard
	}
	s := &Sink{w: w, fd: -1, gate: gate, wait: wait}
	if custom, ok := w.(ContextWriter); ok {
		s.custom = custom
		return s, nil
	}
	switch w := w.(type) {
	case *bytes.Buffer, *strings.Builder:
		return s, nil
	case *os.File:
		// SyscallConn does not make a caller's nonblocking File blocking, as Fd
		// may do. Reopen pipes/ttys into a private open file description.
		conn, err := w.SyscallConn()
		if err != nil {
			return nil, err
		}
		var setupErr error
		err = conn.Control(func(fd uintptr) {
			var st unix.Stat_t
			if setupErr = unix.Fstat(int(fd), &st); setupErr != nil {
				return
			}
			switch st.Mode & unix.S_IFMT {
			case unix.S_IFREG:
				// Preserve the caller's offset/append semantics. Filesystem
				// kernel I/O stalls are outside the cancellable stream contract.
			case unix.S_IFIFO, unix.S_IFCHR:
				s.fd, setupErr = unix.Open(fmt.Sprintf("/proc/self/fd/%d", fd), unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC|unix.O_NOCTTY, 0)
			case unix.S_IFSOCK:
				var kind int
				kind, setupErr = unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_TYPE)
				if setupErr != nil {
					return
				}
				if kind != unix.SOCK_STREAM {
					setupErr = errors.New("output socket must be a stream")
					return
				}
				s.fd, setupErr = unix.FcntlInt(fd, unix.F_DUPFD_CLOEXEC, 3)
				s.socket = true
			default:
				setupErr = errors.New("unsupported output file type")
			}
		})
		if err = errors.Join(err, setupErr); err != nil {
			s.Close()
			return nil, err
		}
		return s, nil
	default:
		if w == io.Discard {
			return s, nil
		}
		return nil, errors.New("custom output writer must implement WriteContext")
	}
}

func (s *Sink) Close() error {
	s.close.Do(func() {
		if s.fd >= 0 {
			s.closeErr = unix.Close(s.fd)
		}
	})
	return s.closeErr
}
func (s *Sinks) Close() error { return errors.Join(s.Out.Close(), s.Err.Close()) }

func (s *Sink) WriteContext(parent context.Context, p []byte) (int, error) {
	ctx, cancel := context.WithTimeout(parent, s.wait)
	defer cancel()
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-s.gate:
	}
	defer func() { s.gate <- struct{}{} }()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if s.custom != nil {
		return s.custom.WriteContext(ctx, p)
	}
	if s.fd < 0 {
		return s.w.Write(p)
	}
	written := 0
	for written < len(p) {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		var n int
		var err error
		if s.socket {
			n, err = unix.SendmsgN(s.fd, p[written:], nil, nil, unix.MSG_DONTWAIT|unix.MSG_NOSIGNAL)
		} else {
			n, err = unix.Write(s.fd, p[written:])
		}
		if n > 0 {
			written += n
		}
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil && !errors.Is(err, unix.EAGAIN) {
			return written, err
		}
		if err == nil && n > 0 {
			continue
		}
		fds := []unix.PollFd{{Fd: int32(s.fd), Events: unix.POLLOUT}}
		if _, err = unix.Poll(fds, 20); err != nil && !errors.Is(err, unix.EINTR) {
			return written, err
		}
		if fds[0].Revents&unix.POLLNVAL != 0 {
			return written, unix.EBADF
		}
	}
	return written, nil
}
