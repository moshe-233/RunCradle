// Package nodeexec records only the direct child result. Process Compose owns
// dependency scheduling and signals to the existing process group.
package nodeexec

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

type Receipt struct {
	Version      int        `json:"schema_version"`
	RunID        string     `json:"run_id"`
	NodeID       string     `json:"node_id"`
	Incarnation  string     `json:"incarnation"`
	HelperPID    int        `json:"helper_pid"`
	GroupID      int        `json:"group_id"`
	BootID       string     `json:"boot_id"`
	PIDNamespace uint64     `json:"pid_namespace"`
	Signal       *int       `json:"signal,omitempty"`
	ChildPID     int        `json:"child_pid,omitempty"`
	Started      bool       `json:"started"`
	Final        bool       `json:"final"`
	ExitCode     *int       `json:"exit_code"`
	StartedAt    time.Time  `json:"started_at"`
	EndedAt      *time.Time `json:"ended_at,omitempty"`
	Failure      string     `json:"failure,omitempty"`
}

func save(root *os.Root, r Receipt) error {
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	name := r.Incarnation + ".json"
	f, err := root.OpenFile(name+".tmp", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, w := f.Write(raw)
	err = errors.Join(w, f.Sync(), f.Close())
	if err == nil {
		err = root.Rename(name+".tmp", name)
	}
	if err != nil {
		_ = root.Remove(name + ".tmp")
		return err
	}
	d, err := root.Open(".")
	if err != nil {
		return err
	}
	return errors.Join(d.Sync(), d.Close())
}

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	f := flag.NewFlagSet("_exec", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	dir := f.String("receipt-dir", "", "")
	run := f.String("run-id", "", "")
	node := f.String("node-id", "", "")
	if f.Parse(args) != nil || f.NArg() == 0 || *dir == "" || *run == "" || *node == "" {
		return 2
	}
	st, err := os.Lstat(*dir)
	if err != nil || !st.IsDir() || st.Mode().Perm() != 0700 {
		return 125
	}
	root, err := os.OpenRoot(filepath.Clean(*dir))
	if err != nil {
		return 125
	}
	defer root.Close()
	var id [16]byte
	if _, err = rand.Read(id[:]); err != nil {
		return 125
	}
	r := Receipt{Version: 1, RunID: *run, NodeID: *node, Incarnation: fmt.Sprintf("%x", id), HelperPID: os.Getpid(), GroupID: syscall.Getpgrp(), StartedAt: time.Now().UTC()}
	boot, _ := os.ReadFile("/proc/sys/kernel/random/boot_id")
	r.BootID = string(boot)
	if info, e := os.Stat("/proc/self/ns/pid"); e == nil {
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			r.PIDNamespace = stat.Ino
		}
	}
	if save(root, r) != nil {
		return 125
	}
	if ctx.Err() != nil {
		return 130
	}
	cmd := exec.Command(f.Args()[0], f.Args()[1:]...)
	cmd.Stdout, cmd.Stderr = stdout, stderr // nil stdin is /dev/null; retain upstream group.
	if err = cmd.Start(); err != nil {
		r.Final = true
		r.Failure = "child_start_failed"
		now := time.Now().UTC()
		r.EndedAt = &now
		if save(root, r) != nil {
			return 125
		}
		return 127
	}
	r.Started = true
	r.ChildPID = cmd.Process.Pid
	writeErr := save(root, r)
	_ = cmd.Wait() // ProcessState is independent of IO relay/drain errors.
	code := 125
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
		if state, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && state.Signaled() {
			code = 128 + int(state.Signal())
			s := int(state.Signal())
			r.Signal = &s
		}
	}
	r.Final = true
	r.ExitCode = &code
	now := time.Now().UTC()
	r.EndedAt = &now
	if writeErr != nil {
		r.Failure = "receipt_write_failed"
	}
	_ = save(root, r) // missing/nonfinal evidence stays unknown; preserve native result
	if r.Signal != nil {
		// exec resets caught Go handlers. Replacing this finished helper with a
		// shell that dies by the same signal preserves upstream WaitStatus=-1;
		// a normal exit(128+signal) could match success_exit_codes incorrectly.
		// This is a finished-helper exit bridge, not another business command:
		// no startup environment or exported function may replace its kill.
		err = syscall.Exec("/bin/sh", []string{"sh", "-c", fmt.Sprintf("kill -%d $$", *r.Signal)}, []string{})
		r.Failure = "signal_replay_failed"
		_ = save(root, r)
		return 125
	}
	return code
}
