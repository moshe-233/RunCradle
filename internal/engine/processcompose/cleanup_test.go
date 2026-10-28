package processcompose

import (
	"os"
	"runcradle/internal/engine"
	"runcradle/internal/nodeexec"
	"syscall"
	"testing"
	"time"
)

func TestFinishCleanupObservationHasOneTotalBudget(t *testing.T) {
	boot, e := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if e != nil {
		t.Fatal(e)
	}
	ns, e := os.Stat("/proc/self/ns/pid")
	if e != nil {
		t.Fatal(e)
	}
	namespace := ns.Sys().(*syscall.Stat_t).Ino
	group := syscall.Getpgrp()
	zero := 0
	r := &run{state: engine.Snapshot{RunID: "fixture", EngineExitCode: &zero}, dir: t.TempDir(), events: make(chan engine.Event, 2)}
	for i := 0; i < 4096; i++ {
		r.records = append(r.records, nodeexec.Receipt{Final: true, HelperPID: group, GroupID: group, BootID: string(boot), PIDNamespace: namespace})
	}
	done := make(chan struct{})
	go func() { r.finish(); close(done) }()
	select {
	case <-done:
		if r.state.Cleanup != "unknown" {
			t.Fatal("live registered group declared cleaned")
		}
	case <-time.After(time.Second):
		t.Fatal("cleanup observation multiplied its budget by receipt count")
	}
}
