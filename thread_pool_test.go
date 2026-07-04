// Copyright (c) the go-ruby-puma/puma authors
//
// SPDX-License-Identifier: BSD-3-Clause

package puma

import (
	"sync"
	"testing"
	"time"
)

// waitFor polls cond until it is true or the deadline elapses.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestNewThreadPoolNormalization(t *testing.T) {
	p := NewThreadPool(-1, 0) // min<0 => 0, max<1 => 1
	defer p.Shutdown()
	if p.min != 0 || p.max != 1 {
		t.Fatalf("min/max = %d/%d, want 0/1", p.min, p.max)
	}

	p2 := NewThreadPool(5, 2) // max<min => max=min
	defer p2.Shutdown()
	if p2.max != 5 {
		t.Fatalf("max = %d, want 5", p2.max)
	}
	waitFor(t, "min workers spawned", func() bool { return p2.Spawned() == 5 })
}

func TestThreadPoolScheduleAndRun(t *testing.T) {
	p := NewThreadPool(0, 3)
	defer p.Shutdown()

	var mu sync.Mutex
	got := 0
	var wg sync.WaitGroup
	wg.Add(10)
	for i := 0; i < 10; i++ {
		p.Schedule(func() {
			mu.Lock()
			got++
			mu.Unlock()
			wg.Done()
		})
	}
	wg.Wait()
	if got != 10 {
		t.Fatalf("ran %d, want 10", got)
	}
	if p.Spawned() > 3 {
		t.Fatalf("spawned %d exceeds max", p.Spawned())
	}
}

func TestThreadPoolPanicIsolation(t *testing.T) {
	p := NewThreadPool(1, 1)
	defer p.Shutdown()

	done := make(chan struct{})
	p.Schedule(func() { panic("boom") })
	p.Schedule(func() { close(done) })
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("pool did not survive a panicking block")
	}
}

func TestThreadPoolTrim(t *testing.T) {
	p := NewThreadPool(1, 4)
	defer p.Shutdown()

	// Force several workers to spawn by scheduling blocking work.
	release := make(chan struct{})
	var started sync.WaitGroup
	started.Add(3)
	for i := 0; i < 3; i++ {
		p.Schedule(func() {
			started.Done()
			<-release
		})
	}
	started.Wait()
	waitFor(t, "3+ workers", func() bool { return p.Spawned() >= 3 })
	close(release)

	// Once idle, trimming should reap workers back toward min.
	waitFor(t, "workers idle", func() bool { return p.Backlog() == 0 })
	for i := 0; i < 5; i++ {
		p.Trim(true)
	}
	waitFor(t, "reaped to min", func() bool { return p.Spawned() == 1 })

	// Trim at the floor is a no-op (spawned == min).
	p.Trim(true)
	if p.Spawned() != 1 {
		t.Fatalf("spawned = %d after floor trim, want 1", p.Spawned())
	}
}

func TestThreadPoolShutdownDrains(t *testing.T) {
	p := NewThreadPool(1, 2)
	var mu sync.Mutex
	ran := 0
	for i := 0; i < 5; i++ {
		p.Schedule(func() {
			time.Sleep(2 * time.Millisecond)
			mu.Lock()
			ran++
			mu.Unlock()
		})
	}
	p.Shutdown()
	if ran != 5 {
		t.Fatalf("drained %d, want 5", ran)
	}
	// Scheduling after shutdown is a no-op.
	p.Schedule(func() { t.Error("should not run") })
	if p.Backlog() != 0 {
		t.Fatalf("backlog = %d after post-shutdown schedule", p.Backlog())
	}
}
