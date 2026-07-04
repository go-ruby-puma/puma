// Copyright (c) the go-ruby-puma/puma authors
//
// SPDX-License-Identifier: BSD-3-Clause

package puma

import "sync"

// ThreadPool is a faithful port of Puma::ThreadPool: a pool of worker threads
// (goroutines) that runs scheduled blocks, growing on demand from Min up to Max
// and reaping idle workers back down to Min. Concurrency of Rack calls is
// bounded by Max; excess work waits in the backlog.
type ThreadPool struct {
	mutex   sync.Mutex
	cond    *sync.Cond
	todo    []func()
	spawned int
	waiting int
	trim    int
	min     int
	max     int
	shutdn  bool
	wg      sync.WaitGroup
}

// NewThreadPool builds a pool bounded by [min, max] worker threads and
// immediately spawns the minimum, mirroring Puma::ThreadPool.new(min, max).
func NewThreadPool(min, max int) *ThreadPool {
	if min < 0 {
		min = 0
	}
	if max < 1 {
		max = 1
	}
	if max < min {
		max = min
	}
	tp := &ThreadPool{min: min, max: max}
	tp.cond = sync.NewCond(&tp.mutex)
	tp.mutex.Lock()
	for i := 0; i < min; i++ {
		tp.spawnThread()
	}
	tp.mutex.Unlock()
	return tp
}

// Spawned returns the current number of live worker threads.
func (tp *ThreadPool) Spawned() int {
	tp.mutex.Lock()
	defer tp.mutex.Unlock()
	return tp.spawned
}

// Backlog returns the number of scheduled blocks not yet picked up by a worker.
func (tp *ThreadPool) Backlog() int {
	tp.mutex.Lock()
	defer tp.mutex.Unlock()
	return len(tp.todo)
}

// spawnThread starts one worker goroutine. The caller must hold mutex.
func (tp *ThreadPool) spawnThread() {
	tp.spawned++
	tp.wg.Add(1)
	go func() {
		defer tp.wg.Done()
		tp.mutex.Lock()
		for {
			for len(tp.todo) == 0 {
				if tp.shutdn {
					tp.spawned--
					tp.mutex.Unlock()
					return
				}
				if tp.trim > 0 && tp.spawned > tp.min {
					tp.trim--
					tp.spawned--
					tp.mutex.Unlock()
					return
				}
				tp.waiting++
				tp.cond.Wait()
				tp.waiting--
			}
			work := tp.todo[0]
			tp.todo = tp.todo[1:]
			tp.mutex.Unlock()
			tp.runOne(work)
			tp.mutex.Lock()
		}
	}()
}

// runOne executes a single block, isolating a panic so one bad block never
// tears down the worker (puma rescues inside the pool). The server schedules
// Rack calls that already recover, so a panic reaching here is a defensive
// backstop that keeps the pool alive.
func (tp *ThreadPool) runOne(work func()) {
	defer func() { _ = recover() }()
	work()
}

// Schedule enqueues a block to run on the pool, spawning an extra worker (up to
// Max) when the backlog outgrows the idle workers. It is a no-op after Shutdown.
func (tp *ThreadPool) Schedule(work func()) {
	tp.mutex.Lock()
	defer tp.mutex.Unlock()
	if tp.shutdn {
		return
	}
	tp.todo = append(tp.todo, work)
	if tp.waiting < len(tp.todo) && tp.spawned < tp.max {
		tp.spawnThread()
	}
	tp.cond.Signal()
}

// Trim requests that one idle worker above Min exit. With force it also trims
// when no worker is currently idle. Mirrors Puma::ThreadPool#trim.
func (tp *ThreadPool) Trim(force bool) {
	tp.mutex.Lock()
	defer tp.mutex.Unlock()
	if (force || tp.waiting > 0) && tp.spawned-tp.trim > tp.min {
		tp.trim++
		tp.cond.Signal()
	}
}

// Shutdown stops accepting new work, wakes every worker and blocks until all
// in-flight and backlogged blocks have drained. Mirrors Puma::ThreadPool#shutdown.
func (tp *ThreadPool) Shutdown() {
	tp.mutex.Lock()
	tp.shutdn = true
	tp.cond.Broadcast()
	tp.mutex.Unlock()
	tp.wg.Wait()
}
