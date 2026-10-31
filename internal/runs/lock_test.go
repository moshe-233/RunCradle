package runs

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTryClaimServerIsImmediateAndReleasesOwnership(t *testing.T) {
	s, err := Open(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	release, err := s.TryClaimServer()
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	other, err := s.TryClaimServer()
	if err == nil {
		other()
		release()
		t.Fatal("duplicate owner accepted")
	}
	if !errors.Is(err, ErrBusy) || time.Since(start) > time.Second {
		release()
		t.Fatalf("contended claim waited/changed classification: %v", err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	release, err = s.TryClaimServer()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if unlock, err := s.ClaimServer(ctx); err == nil {
		unlock()
		t.Fatal("cancelled blocking claim accepted")
	}
}
