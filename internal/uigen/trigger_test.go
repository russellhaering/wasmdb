package uigen

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSweeperCoalescesKicks verifies that a burst of Kicks within the debounce
// window collapses into exactly one sweep.
func TestSweeperCoalescesKicks(t *testing.T) {
	var count int32
	sweep := func(ctx context.Context) (*SweepResult, error) {
		atomic.AddInt32(&count, 1)
		return &SweepResult{}, nil
	}
	s := newSweeper(sweep, 40*time.Millisecond, nil)
	defer s.Stop()

	for i := 0; i < 10; i++ {
		s.Kick("burst")
		time.Sleep(2 * time.Millisecond)
	}

	// Wait comfortably past the debounce window for the single sweep to run.
	time.Sleep(150 * time.Millisecond)

	if got := atomic.LoadInt32(&count); got != 1 {
		t.Fatalf("expected exactly 1 sweep from a coalesced burst, got %d", got)
	}
}

// TestSweeperKickDuringSweepRunsOneFollowup verifies that a Kick delivered while
// a sweep is executing results in exactly one follow-up sweep (not zero, not one
// per kick).
func TestSweeperKickDuringSweepRunsOneFollowup(t *testing.T) {
	var count int32
	var mu sync.Mutex
	// entered is signalled when the first sweep begins; release blocks it there so
	// the test can Kick mid-sweep deterministically.
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var blockedFirst bool

	sweep := func(ctx context.Context) (*SweepResult, error) {
		n := atomic.AddInt32(&count, 1)
		if n == 1 {
			mu.Lock()
			blockedFirst = true
			mu.Unlock()
			entered <- struct{}{}
			<-release // hold the first sweep open
		}
		return &SweepResult{}, nil
	}
	s := newSweeper(sweep, 20*time.Millisecond, nil)
	defer s.Stop()

	// Trigger the first sweep and wait until it is actually running.
	s.Kick("first")
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first sweep never started")
	}

	// Fire several kicks while the first sweep is blocked; they must coalesce into
	// a single follow-up.
	for i := 0; i < 5; i++ {
		s.Kick("during")
	}

	// Let the first sweep finish; the follow-up should run once.
	close(release)

	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&count) < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	// Give any erroneous extra sweeps a chance to appear.
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if !blockedFirst {
		mu.Unlock()
		t.Fatal("first sweep was not the blocking one")
	}
	mu.Unlock()

	if got := atomic.LoadInt32(&count); got != 2 {
		t.Fatalf("expected exactly 2 sweeps (initial + one coalesced follow-up), got %d", got)
	}
}

// TestSweeperSweepNow verifies the synchronous startup path invokes the sweep
// exactly once and reports the error, if any.
func TestSweeperSweepNow(t *testing.T) {
	var count int32
	s := newSweeper(func(ctx context.Context) (*SweepResult, error) {
		atomic.AddInt32(&count, 1)
		return &SweepResult{Created: []string{"tbl-x"}}, nil
	}, time.Second, nil)
	defer s.Stop()

	if err := s.SweepNow(context.Background()); err != nil {
		t.Fatalf("SweepNow: %v", err)
	}
	if got := atomic.LoadInt32(&count); got != 1 {
		t.Fatalf("expected 1 synchronous sweep, got %d", got)
	}
}

// TestSweeperStopIsIdempotent verifies Stop can be called safely and that kicks
// afterward are no-ops.
func TestSweeperStopIsIdempotent(t *testing.T) {
	var count int32
	s := newSweeper(func(ctx context.Context) (*SweepResult, error) {
		atomic.AddInt32(&count, 1)
		return &SweepResult{}, nil
	}, 10*time.Millisecond, nil)

	s.Stop()
	s.Stop() // second call must not panic

	s.Kick("after-stop")
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt32(&count); got != 0 {
		t.Fatalf("expected no sweeps after Stop, got %d", got)
	}
}

// TestSweeperOnNewPages verifies OnNewPages fires (once, async) only when a sweep
// creates pages.
func TestSweeperOnNewPages(t *testing.T) {
	results := make(chan *SweepResult, 4)
	s := newSweeper(func(ctx context.Context) (*SweepResult, error) {
		return <-results, nil
	}, 15*time.Millisecond, nil)
	defer s.Stop()

	var gotMu sync.Mutex
	var got [][]string
	s.OnNewPages = func(created []string) {
		gotMu.Lock()
		got = append(got, created)
		gotMu.Unlock()
	}

	// First sweep creates a page → callback fires.
	results <- &SweepResult{Created: []string{"tbl-a"}}
	s.Kick("create")
	time.Sleep(80 * time.Millisecond)

	// Second sweep only updates → callback must not fire.
	results <- &SweepResult{Updated: []string{"tbl-a"}}
	s.Kick("update")
	time.Sleep(80 * time.Millisecond)

	gotMu.Lock()
	defer gotMu.Unlock()
	if len(got) != 1 {
		t.Fatalf("expected OnNewPages once (only on creation), got %d calls: %v", len(got), got)
	}
	if len(got[0]) != 1 || got[0][0] != "tbl-a" {
		t.Fatalf("unexpected created payload: %v", got[0])
	}
}
