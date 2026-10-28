package runs

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runcradle/internal/engine"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxEvents = 16 << 20
const maxEvent = 16 << 10

// EventStamp lets stream readers avoid scanning an unchanged journal.
func (s *Store) EventStamp(id string) (int64, error) {
	dir, e := s.path(id)
	if e != nil {
		return 0, e
	}
	st, e := s.root.Lstat(filepath.Join(dir, "events.jsonl"))
	if errors.Is(e, os.ErrNotExist) {
		return 0, nil
	}
	if e != nil {
		return 0, e
	}
	if !st.Mode().IsRegular() || st.Mode().Perm() != 0600 || st.Size() > maxEvents {
		return 0, fmt.Errorf("invalid journal")
	}
	return st.Size(), nil
}

var controls = regexp.MustCompile("\x1b\\[[0-?]*[ -/]*[@-~]|\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)")

func clean(s string, n int) string {
	s = controls.ReplaceAllString(strings.ToValidUTF8(s, "�"), "")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	if len(s) > n {
		s = s[:n]
		for !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return s
}
func (s *Store) Append(id string, event engine.Event) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, e := s.path(id)
	if e != nil {
		return false, e
	}
	event.RunID = id
	event.Seq = s.sequences[id] + 1
	if event.Node != nil {
		n := *event.Node
		if n.Diagnostic != nil {
			d := *n.Diagnostic
			d.Text = ""
			d.ExpiresAt = nil
			if d.Status == "available" {
				d.Status = "not_retained"
			}
			n.Diagnostic = &d
		}
		event.Node = &n
	}
	raw, e := json.Marshal(event)
	if e != nil {
		return false, e
	}
	if len(raw)+1 > maxEvent {
		return true, nil
	}
	f, e := s.root.OpenFile(filepath.Join(dir, "events.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW, 0600)
	if e != nil {
		return false, e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return false, e
	}
	if !st.Mode().IsRegular() {
		return false, fmt.Errorf("invalid journal file")
	}
	if st.Size() > 0 && s.sequences[id] == 0 {
		return false, fmt.Errorf("journal already exists; a new writer cannot reset its sequence")
	}
	if st.Size()+int64(len(raw)+1) > maxEvents {
		return true, nil
	}
	n, e := f.Write(append(raw, '\n'))
	if e != nil || n != len(raw)+1 {
		return false, errors.Join(e, io.ErrShortWrite)
	}
	if e = f.Sync(); e != nil {
		return false, e
	}
	s.sequences[id] = event.Seq
	return false, nil
}
func (s *Store) Events(id string, after uint64, limit int) ([]engine.Event, uint64, bool, error) {
	if limit < 1 || limit > 100 {
		return nil, after, false, fmt.Errorf("event limit must be 1..100")
	}
	var events []engine.Event
	next := after
	_, gap, err := s.WalkEvents(id, after, func(event engine.Event) error {
		if len(events) < limit {
			events = append(events, event)
			next = event.Seq
		}
		return nil
	})
	return events, next, gap, err
}

// WalkEvents validates a bounded journal once and delivers each newer event.
// The callback owns delivery/backpressure; a delivery error does not invent a journal gap.
func (s *Store) WalkEvents(id string, after uint64, visit func(engine.Event) error) (uint64, bool, error) {
	dir, e := s.path(id)
	if e != nil {
		return after, false, e
	}
	f, e := s.root.OpenFile(filepath.Join(dir, "events.jsonl"), os.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW, 0)
	if errors.Is(e, os.ErrNotExist) {
		return after, false, nil
	}
	if e != nil {
		return after, true, e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() {
		return after, true, fmt.Errorf("invalid event journal")
	}
	if st.Size() > maxEvents {
		return after, true, fmt.Errorf("journal exceeds budget")
	}
	scanner := bufio.NewScanner(io.LimitReader(f, maxEvents+1))
	scanner.Buffer(make([]byte, 1024), maxEvent)
	scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if index := bytes.IndexByte(data, '\n'); index >= 0 {
			return index + 1, data[:index], nil
		}
		if atEOF && len(data) > 0 {
			return 0, nil, fmt.Errorf("partial event journal")
		}
		return 0, nil, nil
	})
	next := after
	var previous uint64
	size := 0
	for scanner.Scan() {
		size += len(scanner.Bytes()) + 1
		if size > maxEvents {
			return next, true, fmt.Errorf("journal exceeds budget")
		}
		var v engine.Event
		if e = json.Unmarshal(scanner.Bytes(), &v); e != nil || v.RunID != id || v.Seq != previous+1 {
			return next, true, fmt.Errorf("event integrity gap")
		}
		previous = v.Seq
		if v.Seq > after {
			if e = visit(v); e != nil {
				return next, false, e
			}
			next = v.Seq
		}
	}
	if e = scanner.Err(); e != nil {
		return next, true, e
	}
	return next, false, nil
}
