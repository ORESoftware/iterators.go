package iter

import (
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// This file adds chaos / stress testing focused on the iterator's behavior
// under adversarial drive patterns: it proves empirically that concurrency is
// consumer-driven (the same iterator is serial or parallel depending purely on
// how the caller drives it), that the iterator survives recovered panics as
// long as MarkTaskAsComplete is deferred, and it fuzzes both drive modes with
// injected work, yields, and panics.

func rangeN(n int) []int {
	s := make([]int, n)
	for i := range s {
		s[i] = i
	}
	return s
}

// trackMax atomically bumps *max to cur if cur is larger.
func trackMax(max *int64, cur int64) {
	for {
		m := atomic.LoadInt64(max)
		if cur <= m || atomic.CompareAndSwapInt64(max, m, cur) {
			return
		}
	}
}

// TestSequenceSequentialDriveIsSerial demonstrates the footgun: when the caller
// does the work inline and only then advances the iterator, the concurrency
// argument is completely wasted -- at most one task is ever in flight. This is
// deterministic (single consumer goroutine), so it is not timing-sensitive.
func TestSequenceSequentialDriveIsSerial(t *testing.T) {
	const n = 100
	const concurrency = 8

	var inFlight, maxIn int64
	for r := range SeqFromList(concurrency, rangeN(n)) {
		trackMax(&maxIn, atomic.AddInt64(&inFlight, 1))
		// "work" happens here, inline, in the single consumer goroutine.
		atomic.AddInt64(&inFlight, -1)
		r.StartNextTask()
		r.MarkTaskAsComplete()
	}

	if maxIn != 1 {
		t.Fatalf("sequential drive must keep exactly 1 task in flight regardless of concurrency=%d, got %d", concurrency, maxIn)
	}
	t.Logf("sequential drive: max in-flight = %d (concurrency arg %d was unused)", maxIn, concurrency)
}

// TestSequenceParallelDriveReachesConcurrency demonstrates the other half of
// the footgun: the *same* iterator becomes genuinely parallel only when the
// caller requests the next item up front and offloads the work to a goroutine.
func TestSequenceParallelDriveReachesConcurrency(t *testing.T) {
	if testing.Short() {
		t.Skip("timing-sensitive; skipped under -short")
	}
	const n = 300
	const concurrency = 8

	var inFlight, maxIn int64
	var wg sync.WaitGroup
	for r := range SeqFromList(concurrency, rangeN(n)) {
		r.StartNextTask() // request next up front -> production runs ahead
		wg.Add(1)
		go func(r Ret[int]) {
			defer wg.Done()
			defer r.MarkTaskAsComplete()
			trackMax(&maxIn, atomic.AddInt64(&inFlight, 1))
			time.Sleep(2 * time.Millisecond) // real work
			atomic.AddInt64(&inFlight, -1)
		}(r)
	}
	wg.Wait()

	if maxIn > concurrency {
		t.Fatalf("exceeded concurrency limit: %d > %d", maxIn, concurrency)
	}
	if maxIn < 2 {
		t.Fatalf("fan-out drive should produce real parallelism (>1 in flight), got %d", maxIn)
	}
	t.Logf("parallel fan-out: max in-flight = %d (limit %d)", maxIn, concurrency)
}

// TestSequenceChaosWithPanics shows the iterator survives recovered panics in
// worker goroutines AS LONG AS MarkTaskAsComplete is deferred (so the
// concurrency slot is released even when the work blows up). If the release
// were skipped on the panic path the whole iteration would deadlock.
func TestSequenceChaosWithPanics(t *testing.T) {
	const n = 500
	const concurrency = 8

	rng := rand.New(rand.NewSource(99))
	doPanic := make([]bool, n)
	for i := range doPanic {
		doPanic[i] = rng.Intn(5) == 0
	}

	var mu sync.Mutex
	seen := make([]int, n)

	runWithTimeout(t, 20*time.Second, func() {
		var wg sync.WaitGroup
		for r := range SeqFromList(concurrency, rangeN(n)) {
			r.StartNextTask()
			wg.Add(1)
			go func(r Ret[int]) {
				defer wg.Done()
				defer r.MarkTaskAsComplete() // released even on panic
				defer func() { _ = recover() }()

				mu.Lock()
				seen[r.Value]++
				mu.Unlock()

				if doPanic[r.Value] {
					panic("chaos")
				}
			}(r)
		}
		wg.Wait()
	})

	for i := 0; i < n; i++ {
		if seen[i] != 1 {
			t.Fatalf("value %d processed %d times, want exactly once (even panicking items)", i, seen[i])
		}
	}
}

// FuzzSequenceChaos fuzzes both drive modes (sequential and parallel) together
// with injected scheduler yields and recovered panics, asserting the iterator
// always terminates, delivers every element exactly once, and never exceeds the
// configured concurrency.
//
//	go test -race -run x -fuzz FuzzSequenceChaos ./v1/iter/
func FuzzSequenceChaos(f *testing.F) {
	f.Add(4, 50, uint64(1), false)
	f.Add(1, 7, uint64(2), true)
	f.Add(16, 200, uint64(3), true)
	f.Add(8, 0, uint64(4), false)
	f.Add(32, 137, uint64(5), true)

	f.Fuzz(func(t *testing.T, concurrency, n int, seed uint64, parallel bool) {
		if concurrency < 0 {
			concurrency = -concurrency
		}
		concurrency = concurrency%32 + 1
		if n < 0 {
			n = -n
		}
		n = n % 300

		rng := rand.New(rand.NewSource(int64(seed)))
		doPanic := make([]bool, n)
		doYield := make([]bool, n)
		for i := 0; i < n; i++ {
			doPanic[i] = rng.Intn(6) == 0
			doYield[i] = rng.Intn(3) == 0
		}

		var mu sync.Mutex
		seen := make([]int, n)
		var inFlight, maxIn int64

		runWithTimeout(t, 20*time.Second, func() {
			var wg sync.WaitGroup
			for r := range SeqFromList(concurrency, rangeN(n)) {
				r := r // per-iteration copy: go 1.21 shares the loop variable,
				// and capturing it in the worker goroutine below would race and
				// call MarkTaskAsComplete on the wrong Ret -> deadlock.
				v := r.Value
				if v < 0 || v >= n {
					t.Errorf("value out of range: %d (n=%d)", v, n)
					continue
				}

				handle := func() {
					defer r.MarkTaskAsComplete()
					defer func() { _ = recover() }()

					trackMax(&maxIn, atomic.AddInt64(&inFlight, 1))
					mu.Lock()
					seen[v]++
					mu.Unlock()
					if doYield[v] {
						runtime.Gosched()
					}
					atomic.AddInt64(&inFlight, -1)
					if doPanic[v] {
						panic("chaos")
					}
				}

				r.StartNextTask()
				if parallel {
					wg.Add(1)
					go func() {
						defer wg.Done()
						handle()
					}()
				} else {
					handle()
				}
			}
			wg.Wait()
		})

		for i := 0; i < n; i++ {
			if seen[i] != 1 {
				t.Fatalf("value %d seen %d times (parallel=%v c=%d n=%d)", i, seen[i], parallel, concurrency, n)
			}
		}
		if maxIn > int64(concurrency) {
			t.Fatalf("concurrency exceeded: %d > %d", maxIn, concurrency)
		}
	})
}
