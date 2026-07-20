package uigen

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Sweeper owns a single background goroutine that coalesces scaffold-generation
// triggers into debounced Sweep runs. Callers fire Kick (cheap, non-blocking)
// from hot paths such as the registry OnWrite/OnSchemaChange hooks; the actual
// (potentially expensive) sweep runs at most once per debounce window on the
// background goroutine.
//
// Only one sweep is ever in flight. Kicks that arrive while a sweep is running
// are coalesced into exactly one follow-up sweep after it finishes, so no trigger
// is lost and the store is never swept concurrently with itself.
type Sweeper struct {
	sweep  func(context.Context) (*SweepResult, error)
	delay  time.Duration
	logger *slog.Logger

	// OnNewPages, if set, is invoked (on its own goroutine) after any sweep that
	// created one or more pages, with the created page names. It chains the LLM
	// ui-builder polish pass; it never fires for updates or deletes, which bounds
	// LLM cost. Set once before triggers begin; read without locking.
	OnNewPages func(created []string)

	// rootCtx governs background (Kick-driven) sweeps. rootCancel is invoked by
	// Stop before wg.Wait so an in-flight sweep can abort rather than block
	// shutdown. SweepNow uses the caller's ctx instead.
	rootCtx    context.Context
	rootCancel context.CancelFunc

	mu       sync.Mutex
	timer    *time.Timer // debounce timer; nil when idle
	running  bool        // a sweep is currently executing
	pending  bool        // a kick arrived while a sweep was running
	lastKick string      // most recent trigger reason, for logging
	closed   bool
	wakeCh   chan struct{} // buffered(1); wakes the worker when a debounce fires
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// NewSweeper creates a Sweeper that runs gen.Sweep with the given debounce delay.
// A nil logger defaults to slog.Default(); a non-positive delay defaults to 5s.
// The background worker starts immediately.
func NewSweeper(gen *Generator, delay time.Duration, logger *slog.Logger) *Sweeper {
	return newSweeper(gen.Sweep, delay, logger)
}

// newSweeper is the injectable constructor used by tests to supply a controllable
// sweep function (e.g. one that blocks or counts invocations).
func newSweeper(sweep func(context.Context) (*SweepResult, error), delay time.Duration, logger *slog.Logger) *Sweeper {
	if logger == nil {
		logger = slog.Default()
	}
	if delay <= 0 {
		delay = 5 * time.Second
	}
	rootCtx, rootCancel := context.WithCancel(context.Background())
	s := &Sweeper{
		sweep:      sweep,
		delay:      delay,
		logger:     logger,
		rootCtx:    rootCtx,
		rootCancel: rootCancel,
		wakeCh:     make(chan struct{}, 1),
		stopCh:     make(chan struct{}),
	}
	s.wg.Add(1)
	go s.run()
	return s
}

// Kick requests a sweep. It is non-blocking and coalescing: repeated calls within
// the debounce window collapse into a single sweep, and the delay timer resets on
// each call so bursts settle before the sweep fires. A kick received while a sweep
// is running guarantees exactly one follow-up sweep once it completes.
func (s *Sweeper) Kick(reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.lastKick = reason
	if s.running {
		// Don't debounce mid-sweep: just mark that another sweep is owed.
		s.pending = true
		return
	}
	if s.timer == nil {
		s.timer = time.AfterFunc(s.delay, s.fire)
	} else {
		s.timer.Reset(s.delay)
	}
}

// fire is invoked by the debounce timer; it wakes the worker.
func (s *Sweeper) fire() {
	s.mu.Lock()
	s.timer = nil
	s.mu.Unlock()
	select {
	case s.wakeCh <- struct{}{}:
	default:
		// A wake is already queued; the worker will pick up the coalesced work.
	}
}

// run is the background worker loop.
func (s *Sweeper) run() {
	defer s.wg.Done()
	for {
		select {
		case <-s.stopCh:
			return
		case <-s.wakeCh:
			s.drain()
		}
	}
}

// drain runs sweeps until no follow-up work remains. running/pending are managed
// under the lock so a Kick during a sweep produces exactly one more sweep.
func (s *Sweeper) drain() {
	for {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return
		}
		reason := s.lastKick
		s.running = true
		s.pending = false
		s.mu.Unlock()

		s.sweepOnce(reason)

		s.mu.Lock()
		s.running = false
		again := s.pending
		s.mu.Unlock()
		if !again {
			return
		}
	}
}

// sweepOnce performs one synchronous sweep and logs/dispatches its result.
func (s *Sweeper) sweepOnce(reason string) {
	res, err := s.sweep(s.rootCtx)
	if err != nil {
		s.logger.Error("uigen: scaffold sweep failed", "reason", reason, "err", err)
		return
	}
	s.report(reason, res)
}

// report logs non-empty sweep results and dispatches the OnNewPages callback.
func (s *Sweeper) report(reason string, res *SweepResult) {
	if len(res.Created) > 0 || len(res.Updated) > 0 || len(res.Deleted) > 0 {
		s.logger.Info("uigen: scaffold sweep",
			"reason", reason,
			"created", len(res.Created),
			"updated", len(res.Updated),
			"deleted", len(res.Deleted),
			"skipped", len(res.Skipped),
		)
	}
	if len(res.Created) > 0 && s.OnNewPages != nil {
		created := append([]string(nil), res.Created...)
		go s.OnNewPages(created)
	}
}

// SweepNow runs a sweep synchronously (bypassing the debounce) and reports its
// result. Used at startup so scaffold pages exist before the first request. The
// caller's ctx governs cancellation.
func (s *Sweeper) SweepNow(ctx context.Context) error {
	res, err := s.sweep(ctx)
	if err != nil {
		return err
	}
	s.report("startup", res)
	return nil
}

// Stop halts the debounce timer and shuts down the worker goroutine, blocking
// until it exits. Kicks after Stop are no-ops. Safe to call once.
func (s *Sweeper) Stop() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.mu.Unlock()

	// Cancel the root context BEFORE waiting so an in-flight background sweep can
	// abort instead of blocking shutdown.
	s.rootCancel()
	close(s.stopCh)
	s.wg.Wait()
}
