package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// A slot is the only genuinely concurrent thing in this service, and its three
// cases are easy to get subtly wrong: a fresh slot must not fetch, a stale one
// must not make anyone wait, and a cold one must not let twenty requests turn
// into twenty upstream calls.

// counter records how often a fetch ran and how long each caller had to wait.
type counter struct {
	mu    sync.Mutex
	calls int
	block time.Duration
	err   error
}

func (c *counter) fetch(context.Context) error {
	c.mu.Lock()
	c.calls++
	block, err := c.block, c.err
	c.mu.Unlock()

	time.Sleep(block)
	return err
}

func (c *counter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func TestSlotFetchesOnceWhileFresh(t *testing.T) {
	var s slot
	f := &counter{}

	for range 5 {
		s.ensure(context.Background(), time.Hour, f.fetch)
	}
	if got := f.count(); got != 1 {
		t.Errorf("fetched %d times, want 1 — a fresh slot must not touch the upstream", got)
	}
}

func TestSlotBlocksOnlyWhenNothingIsCached(t *testing.T) {
	var s slot
	f := &counter{block: 50 * time.Millisecond}

	start := time.Now()
	s.ensure(context.Background(), time.Millisecond, f.fetch)
	cold := time.Since(start)
	if cold < 50*time.Millisecond {
		t.Fatalf("cold ensure returned after %s, want to wait for the fetch", cold)
	}

	// Now the slot holds data but is stale at once: the caller must get the
	// old value back immediately and the refresh must happen behind it.
	time.Sleep(5 * time.Millisecond)
	start = time.Now()
	s.ensure(context.Background(), time.Millisecond, f.fetch)
	stale := time.Since(start)
	if stale > 10*time.Millisecond {
		t.Errorf("stale ensure blocked for %s, want an immediate return "+
			"(stale-while-revalidate)", stale)
	}

	// ... and it must actually happen.
	deadline := time.Now().Add(2 * time.Second)
	for f.count() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := f.count(); got != 2 {
		t.Errorf("fetched %d times, want 2 — the background refresh never ran", got)
	}
}

func TestSlotCollapsesAColdStampede(t *testing.T) {
	var s slot
	f := &counter{block: 100 * time.Millisecond}

	var wg sync.WaitGroup
	for range 30 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.ensure(context.Background(), time.Hour, f.fetch)
		}()
	}
	wg.Wait()

	if got := f.count(); got != 1 {
		t.Errorf("fetched %d times for 30 simultaneous callers, want 1", got)
	}
}

func TestSlotKeepsQuietAfterAFailure(t *testing.T) {
	var s slot
	f := &counter{err: errors.New("upstream on fire")}

	// A failed fetch leaves nothing cached, but the slot must not retry on
	// every single request — that would turn an outage into a flood.
	s.ensure(context.Background(), time.Hour, f.fetch)
	for range 10 {
		s.ensure(context.Background(), time.Hour, f.fetch)
	}
	if got := f.count(); got != 1 {
		t.Errorf("fetched %d times after a failure, want 1 within the retry window", got)
	}
}

func TestJitterStaysInBoundsAndVaries(t *testing.T) {
	const base = time.Hour
	low, high := base-base/5, base+base/5

	seen := map[time.Duration]bool{}
	for range 200 {
		d := jitter(base)
		if d < low || d > high {
			t.Fatalf("jitter(%s) = %s, outside [%s, %s]", base, d, low, high)
		}
		seen[d] = true
	}
	if len(seen) < 100 {
		t.Errorf("only %d distinct values out of 200 — that is a pattern, not jitter", len(seen))
	}

	// A zero interval means "always stale" and must survive the jitter.
	if d := jitter(0); d != 0 {
		t.Errorf("jitter(0) = %s, want 0", d)
	}
}
