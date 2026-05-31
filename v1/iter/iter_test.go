package iter

import (
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// runWithTimeout runs fn and fails the test if it does not return within d.
// It guards against the iterator deadlocking / never closing its channel.
func runWithTimeout(t *testing.T, d time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		fn()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("timed out after %s (iterator likely deadlocked or never closed its channel)", d)
	}
}

// TestSequenceSimple drives a small list to completion and verifies every
// element is delivered exactly once and that the channel is closed at the end.
func TestSequenceSimple(t *testing.T) {
	runWithTimeout(t, 5*time.Second, func() {
		reader := &FromList[int]{list: []int{1, 2, 3, 4, 5}}

		var results []int
		for r := range Sequence[int](2, reader) {
			if r.Done {
				t.Errorf("Done should never be true for a produced value")
			}
			results = append(results, r.Value)
			r.StartNextTask()
			r.MarkTaskAsComplete()
		}

		sort.Ints(results)
		want := []int{1, 2, 3, 4, 5}
		if len(results) != len(want) {
			t.Fatalf("expected %d results, got %d (%v)", len(want), len(results), results)
		}
		for i := range want {
			if results[i] != want[i] {
				t.Fatalf("results mismatch: got %v want %v", results, want)
			}
		}
	})
}

// TestSequenceConcurrentDrain is the headline race-condition test. It fans out
// work across many goroutines (calling StartNextTask before deferring the
// completion to a worker) so that up to `concurrency` tasks are genuinely in
// flight at once. Run with `-race` it must report no data race, deliver every
// element exactly once, and never exceed the configured concurrency.
func TestSequenceConcurrentDrain(t *testing.T) {
	t.Parallel()

	const n = 2000
	const concurrency = 16

	list := make([]int, n)
	for i := range list {
		list[i] = i
	}

	runWithTimeout(t, 30*time.Second, func() {
		reader := &FromList[int]{list: list}

		var mu sync.Mutex
		seen := make(map[int]int)

		var inFlight int64
		var maxInFlight int64

		var wg sync.WaitGroup
		for r := range Sequence[int](concurrency, reader) {
			if r.Done {
				t.Errorf("Done should never be true for a produced value")
			}

			mu.Lock()
			seen[r.Value]++
			mu.Unlock()

			cur := atomic.AddInt64(&inFlight, 1)
			for {
				m := atomic.LoadInt64(&maxInFlight)
				if cur <= m || atomic.CompareAndSwapInt64(&maxInFlight, m, cur) {
					break
				}
			}

			// Request the next item immediately so production runs ahead and
			// real concurrency is exercised, then defer completion to a worker.
			r.StartNextTask()

			wg.Add(1)
			r := r
			go func() {
				defer wg.Done()
				time.Sleep(time.Millisecond)
				atomic.AddInt64(&inFlight, -1)
				r.MarkTaskAsComplete()
			}()
		}
		wg.Wait()

		if maxInFlight > concurrency {
			t.Errorf("concurrency exceeded: observed %d in flight, limit was %d", maxInFlight, concurrency)
		}
		if got := len(seen); got != n {
			t.Fatalf("expected %d distinct values, got %d", n, got)
		}
		for i := 0; i < n; i++ {
			if seen[i] != 1 {
				t.Fatalf("value %d delivered %d times (expected exactly once)", i, seen[i])
			}
		}
	})
}

// TestSequenceCompleteBeforeStartNext verifies that calling MarkTaskAsComplete
// before StartNextTask does not prematurely close the channel. The previous
// implementation closed the channel as soon as the in-flight count hit zero,
// without checking whether the source was actually exhausted, so this would
// drop elements.
func TestSequenceCompleteBeforeStartNext(t *testing.T) {
	runWithTimeout(t, 5*time.Second, func() {
		reader := &FromList[int]{list: []int{10, 20, 30, 40, 50, 60, 70}}

		count := 0
		for r := range Sequence[int](3, reader) {
			count++
			// Complete first, then ask for the next item.
			r.MarkTaskAsComplete()
			r.StartNextTask()
		}

		if count != 7 {
			t.Fatalf("expected 7 elements, got %d (channel closed early?)", count)
		}
	})
}

// TestSequenceIdempotentCallbacks verifies the per-task callbacks are safe to
// call more than once: extra StartNextTask / MarkTaskAsComplete calls must be
// no-ops and must never panic with "close of closed channel" or
// "send on closed channel".
func TestSequenceIdempotentCallbacks(t *testing.T) {
	runWithTimeout(t, 5*time.Second, func() {
		reader := &FromList[int]{list: []int{1, 2, 3, 4}}

		count := 0
		for r := range Sequence[int](2, reader) {
			count++
			r.StartNextTask()
			r.StartNextTask() // extra: must be a no-op
			r.MarkTaskAsComplete()
			r.MarkTaskAsComplete() // extra: must be a no-op
		}

		if count != 4 {
			t.Fatalf("expected 4 elements, got %d", count)
		}
	})
}

// TestSequenceSerializesSource confirms the iterator serializes calls to the
// source's Next(). The source below mutates shared state without its own
// synchronization; because Sequence calls Next() under its lock this must be
// race-free under `-race` and yield every value exactly once.
func TestSequenceSerializesSource(t *testing.T) {
	t.Parallel()

	const n = 500
	const concurrency = 8

	runWithTimeout(t, 30*time.Second, func() {
		counter := 0 // deliberately unsynchronized in the source closure
		src := struct{ Next func() (bool, int) }{
			Next: func() (bool, int) {
				if counter >= n {
					return true, 0
				}
				v := counter
				counter++
				return false, v
			},
		}

		var mu sync.Mutex
		seen := make(map[int]int)

		var wg sync.WaitGroup
		for r := range Seq[int](concurrency, src) {
			if r.Done {
				t.Errorf("Done should never be true for a produced value")
			}
			mu.Lock()
			seen[r.Value]++
			mu.Unlock()

			r.StartNextTask()

			wg.Add(1)
			r := r
			go func() {
				defer wg.Done()
				r.MarkTaskAsComplete()
			}()
		}
		wg.Wait()

		if got := len(seen); got != n {
			t.Fatalf("expected %d distinct values, got %d", n, got)
		}
		for i := 0; i < n; i++ {
			if seen[i] != 1 {
				t.Fatalf("value %d delivered %d times (expected exactly once)", i, seen[i])
			}
		}
	})
}

// TestAsyncSequenceFromChan exercises the channel-backed source (AsyncSequence)
// which finishes when the underlying channel is closed.
func TestAsyncSequenceFromChan(t *testing.T) {
	t.Parallel()

	const n = 300
	const concurrency = 4

	runWithTimeout(t, 30*time.Second, func() {
		src := make(chan int)
		go func() {
			defer close(src)
			for i := 0; i < n; i++ {
				src <- i
			}
		}()

		var mu sync.Mutex
		seen := make(map[int]int)

		var wg sync.WaitGroup
		for r := range AsyncSequence[int](concurrency, src) {
			if r.Done {
				t.Errorf("Done should never be true for a produced value")
			}
			mu.Lock()
			seen[r.Value]++
			mu.Unlock()

			r.StartNextTask()

			wg.Add(1)
			r := r
			go func() {
				defer wg.Done()
				r.MarkTaskAsComplete()
			}()
		}
		wg.Wait()

		if got := len(seen); got != n {
			t.Fatalf("expected %d distinct values, got %d", n, got)
		}
		for i := 0; i < n; i++ {
			if seen[i] != 1 {
				t.Fatalf("value %d delivered %d times (expected exactly once)", i, seen[i])
			}
		}
	})
}

// TestSequenceEmptySource verifies that an immediately-exhausted source closes
// the output channel without emitting anything.
func TestSequenceEmptySource(t *testing.T) {
	runWithTimeout(t, 5*time.Second, func() {
		reader := &FromList[int]{list: []int{}}
		count := 0
		for range Sequence[int](4, reader) {
			count++
		}
		if count != 0 {
			t.Fatalf("expected 0 elements from an empty source, got %d", count)
		}
	})
}
