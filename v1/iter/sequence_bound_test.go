package iter

import (
	"sync/atomic"
	"testing"
	"time"
)

type pullCountingSource struct {
	values []int
	index  int
	calls  atomic.Int64
}

func (s *pullCountingSource) Next() (bool, int) {
	s.calls.Add(1)
	if s.index >= len(s.values) {
		return true, 0
	}
	value := s.values[s.index]
	s.index++
	return false, value
}

func TestSequenceDoesNotReadAheadPastConcurrency(t *testing.T) {
	const concurrency = 2
	source := &pullCountingSource{values: []int{1, 2, 3}}
	sequence := Sequence[int](concurrency, source)

	first := <-sequence
	first.StartNextTask()
	second := <-sequence
	second.StartNextTask()

	deadline := time.Now().Add(2 * time.Second)
	for source.calls.Load() < concurrency && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := source.calls.Load(); got != concurrency {
		t.Fatalf("source was pulled %d times with %d occupied slots; want exactly %d", got, concurrency, concurrency)
	}

	// Hold both delivered tasks open long enough for any incorrectly eager
	// producer to call Next(). A third pull here means source reservation is not
	// actually bounded by the concurrency setting.
	time.Sleep(100 * time.Millisecond)
	if got := source.calls.Load(); got != concurrency {
		t.Fatalf("source read ahead to %d pulls while %d tasks were still in flight", got, concurrency)
	}

	first.MarkTaskAsComplete()
	third := <-sequence
	if third.Value != 3 {
		t.Fatalf("expected third value after releasing a slot, got %d", third.Value)
	}
	second.MarkTaskAsComplete()
	third.StartNextTask()
	third.MarkTaskAsComplete()

	select {
	case _, ok := <-sequence:
		if ok {
			t.Fatal("expected sequence to close after source exhaustion")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("sequence did not close after final completion")
	}

	if got := source.calls.Load(); got != 4 {
		t.Fatalf("expected three value pulls plus one exhaustion pull, got %d", got)
	}
}
