package iter

import (
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// This file fuzz/property-tests the Sequence iterator. Every drive below
// preserves the iterator's contract (each delivered item gets exactly one
// StartNextTask and exactly one MarkTaskAsComplete), but fuzzes everything
// else: the source implementation, the concurrency, the order of the two
// callbacks, whether they run inline or in a goroutine, whether they are
// (idempotently) called more than once, and the goroutine interleavings.
//
// The invariants asserted for every run are:
//   - it never panics and is race-free (run with -race),
//   - it always terminates (the output channel is eventually closed),
//   - every source element is delivered exactly once,
//   - the number of simultaneously in-flight tasks never exceeds concurrency.

// sourceCount is the number of distinct source constructors exercised.
const sourceCount = 6

// makeSource returns a factory that builds a fresh Sequence-style channel
// emitting the logical values 0..n-1 exactly once, using one of the library's
// several source constructors selected by sel.
func makeSource(sel int, n, concurrency int) func() chan Ret[int] {
	switch ((sel % sourceCount) + sourceCount) % sourceCount {

	case 0: // raw Sequence over FromList
		return func() chan Ret[int] {
			list := make([]int, n)
			for i := range list {
				list[i] = i
			}
			return Sequence[int](concurrency, &FromList[int]{list: list})
		}

	case 1: // SeqFromList helper
		return func() chan Ret[int] {
			list := make([]int, n)
			for i := range list {
				list[i] = i
			}
			return SeqFromList[int](concurrency, list)
		}

	case 2: // SeqFromListOfPointers helper
		return func() chan Ret[int] {
			ptrs := make([]*int, n)
			for i := range ptrs {
				v := i
				ptrs[i] = &v
			}
			return SeqFromListOfPointers[int](concurrency, ptrs)
		}

	case 3: // AsyncSequence over a channel fed by a goroutine
		return func() chan Ret[int] {
			ch := make(chan int)
			go func() {
				defer close(ch)
				for i := 0; i < n; i++ {
					ch <- i
				}
			}()
			return AsyncSequence[int](concurrency, ch)
		}

	case 4: // Seq over an anonymous struct with a Next closure
		return func() chan Ret[int] {
			i := 0
			return Seq[int](concurrency, struct{ Next func() (bool, int) }{
				Next: func() (bool, int) {
					if i >= n {
						return true, 0
					}
					v := i
					i++
					return false, v
				},
			})
		}

	default: // case 5: FromNext over an HsNext closure
		return func() chan Ret[int] {
			i := 0
			return FromNext[int](concurrency, HsNext[int]{
				Next: func() (bool, int) {
					if i >= n {
						return true, 0
					}
					v := i
					i++
					return false, v
				},
			})
		}
	}
}

// itemPlan captures the randomized decisions for handling a single delivered item.
type itemPlan struct {
	startFirst  bool // call StartNextTask before MarkTaskAsComplete
	dupStart    bool // call StartNextTask an extra time (must be a no-op)
	dupComplete bool // call MarkTaskAsComplete an extra time (must be a no-op)
	inGoroutine bool // run the callbacks from a spawned goroutine
	yield       bool // perturb scheduling before running the callbacks
}

func planFor(rng *rand.Rand) itemPlan {
	return itemPlan{
		startFirst:  rng.Intn(2) == 0,
		dupStart:    rng.Intn(4) == 0,
		dupComplete: rng.Intn(4) == 0,
		inGoroutine: rng.Intn(2) == 0,
		yield:       rng.Intn(8) == 0,
	}
}

// driveSequence drains a freshly built sequence of n elements, fuzzing the
// consumer behavior with rng, and asserts all invariants.
func driveSequence(t *testing.T, mk func() chan Ret[int], n, concurrency int, rng *rand.Rand) {
	t.Helper()

	seen := make([]int, n) // written only by the single range goroutine
	var inFlight int64
	var maxInFlight int64
	var wg sync.WaitGroup

	work := func() {
		for r := range mk() {
			if r.Done {
				t.Errorf("Done should never be true for a produced value")
			}

			v := r.Value
			if v < 0 || v >= n {
				t.Errorf("value out of range: got %d, want [0,%d)", v, n)
			} else {
				seen[v]++
			}

			cur := atomic.AddInt64(&inFlight, 1)
			for {
				m := atomic.LoadInt64(&maxInFlight)
				if cur <= m || atomic.CompareAndSwapInt64(&maxInFlight, m, cur) {
					break
				}
			}

			p := planFor(rng)
			r := r

			// run performs exactly one StartNextTask and exactly one
			// MarkTaskAsComplete (plus optional idempotent duplicates). inFlight
			// is decremented immediately before releasing the concurrency slot
			// (MarkTaskAsComplete), so it is always <= the slots held <= concurrency.
			run := func() {
				if p.yield {
					runtime.Gosched()
				}
				if p.startFirst {
					r.StartNextTask()
					if p.dupStart {
						r.StartNextTask()
					}
					atomic.AddInt64(&inFlight, -1)
					r.MarkTaskAsComplete()
					if p.dupComplete {
						r.MarkTaskAsComplete()
					}
				} else {
					atomic.AddInt64(&inFlight, -1)
					r.MarkTaskAsComplete()
					if p.dupComplete {
						r.MarkTaskAsComplete()
					}
					r.StartNextTask()
					if p.dupStart {
						r.StartNextTask()
					}
				}
			}

			if p.inGoroutine {
				wg.Add(1)
				go func() {
					defer wg.Done()
					run()
				}()
			} else {
				run()
			}
		}
		wg.Wait()
	}

	// Guard against deadlocks / channels that never close: a hang becomes a
	// test failure instead of stalling the whole run.
	done := make(chan struct{})
	go func() {
		work()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatalf("timed out: deadlock or channel never closed (n=%d, concurrency=%d)", n, concurrency)
	}

	for i := 0; i < n; i++ {
		if seen[i] != 1 {
			t.Fatalf("value %d delivered %d times, want exactly 1 (n=%d, concurrency=%d)", i, seen[i], n, concurrency)
		}
	}
	if maxInFlight > int64(concurrency) {
		t.Fatalf("concurrency exceeded: observed %d in flight, limit %d (n=%d)", maxInFlight, concurrency, n)
	}
}

// TestSequenceRandomized runs many randomized configurations deterministically
// so that `go test -race` exercises a wide variety of interleavings without
// needing the fuzzing engine.
func TestSequenceRandomized(t *testing.T) {
	t.Parallel()

	const iterations = 400
	base := rand.New(rand.NewSource(0xC0FFEE))

	for iter := 0; iter < iterations; iter++ {
		rng := rand.New(rand.NewSource(base.Int63()))
		concurrency := rng.Intn(32) + 1
		n := rng.Intn(200)
		sel := rng.Intn(sourceCount)

		mk := makeSource(sel, n, concurrency)
		driveSequence(t, mk, n, concurrency, rng)
		if t.Failed() {
			t.Fatalf("failed on iteration %d (n=%d, concurrency=%d, source=%d)", iter, n, concurrency, sel)
		}
	}
}

// FuzzSequence lets the Go fuzzing engine search the input space (source type,
// concurrency, element count, and the PRNG seed that drives all per-item
// scheduling decisions). Run with:
//
//	go test -race -run x -fuzz FuzzSequence ./v1/iter/
func FuzzSequence(f *testing.F) {
	f.Add(2, 10, 0, uint64(1))
	f.Add(1, 1, 5, uint64(42))
	f.Add(8, 64, 3, uint64(99))
	f.Add(16, 200, 1, uint64(123456789))
	f.Add(4, 0, 2, uint64(7))
	f.Add(32, 37, 4, uint64(0xABCDEF))

	f.Fuzz(func(t *testing.T, concurrency, n, sel int, seed uint64) {
		// Clamp inputs to keep each iteration cheap and well-defined.
		if concurrency < 0 {
			concurrency = -concurrency
		}
		concurrency = concurrency%64 + 1 // 1..64

		if n < 0 {
			n = -n
		}
		n = n % 300 // 0..299

		rng := rand.New(rand.NewSource(int64(seed)))
		mk := makeSource(sel, n, concurrency)
		driveSequence(t, mk, n, concurrency, rng)
	})
}
