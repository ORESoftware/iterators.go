package compare

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oresoftware/go-iterators/v1/iter"
	"golang.org/x/sync/errgroup"
)

// This module compares the go-iterators library against two idiomatic Go ways
// of running at most N jobs at a time:
//   - golang.org/x/sync/errgroup with SetLimit(N)
//   - a stdlib buffered-channel semaphore + sync.WaitGroup worker pool
//
// TestDifferential runs the same randomized workloads through all three and
// asserts identical, correct results (every item processed exactly once,
// concurrency never exceeded). BenchmarkCompare measures the per-framework
// overhead (time + allocations) of each.

func rangeN(n int) []int {
	s := make([]int, n)
	for i := range s {
		s[i] = i
	}
	return s
}

// collector records which values were processed and the peak concurrency.
type collector struct {
	mu          sync.Mutex
	seen        map[int]int
	inFlight    int64
	maxInFlight int64
}

func newCollector() *collector { return &collector{seen: make(map[int]int)} }

func (c *collector) do(v int, work func(int)) {
	cur := atomic.AddInt64(&c.inFlight, 1)
	for {
		m := atomic.LoadInt64(&c.maxInFlight)
		if cur <= m || atomic.CompareAndSwapInt64(&c.maxInFlight, m, cur) {
			break
		}
	}
	c.mu.Lock()
	c.seen[v]++
	c.mu.Unlock()
	if work != nil {
		work(v)
	}
	atomic.AddInt64(&c.inFlight, -1)
}

// ---- implementations under test (correctness/differential variants) ----

func runIter(n, c int, work func(int)) *collector {
	col := newCollector()
	var wg sync.WaitGroup
	for r := range iter.SeqFromList(c, rangeN(n)) {
		r.StartNextTask() // fan out: request next up front
		wg.Add(1)
		go func(r iter.Ret[int]) {
			defer wg.Done()
			defer r.MarkTaskAsComplete()
			col.do(r.Value, work)
		}(r)
	}
	wg.Wait()
	return col
}

func runErrgroup(n, c int, work func(int)) *collector {
	col := newCollector()
	g, _ := errgroup.WithContext(context.Background())
	g.SetLimit(c)
	for _, v := range rangeN(n) {
		v := v
		g.Go(func() error {
			col.do(v, work)
			return nil
		})
	}
	_ = g.Wait()
	return col
}

func runPool(n, c int, work func(int)) *collector {
	col := newCollector()
	sem := make(chan struct{}, c)
	var wg sync.WaitGroup
	for _, v := range rangeN(n) {
		v := v
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			col.do(v, work)
		}()
	}
	wg.Wait()
	return col
}

// runIterWithTimeout guards against the iterator deadlocking (errgroup/pool
// cannot deadlock here, so they are called directly).
func runIterWithTimeout(t *testing.T, n, c int, work func(int)) *collector {
	t.Helper()
	ch := make(chan *collector, 1)
	go func() { ch <- runIter(n, c, work) }()
	select {
	case col := <-ch:
		return col
	case <-time.After(30 * time.Second):
		t.Fatalf("iter deadlocked (n=%d c=%d)", n, c)
		return nil
	}
}

func assertExactlyOnce(t *testing.T, name string, col *collector, n, c int) {
	t.Helper()
	if got := len(col.seen); got != n {
		t.Fatalf("%s: %d distinct values, want %d", name, got, n)
	}
	for i := 0; i < n; i++ {
		if col.seen[i] != 1 {
			t.Fatalf("%s: value %d processed %d times, want once", name, i, col.seen[i])
		}
	}
	if col.maxInFlight > int64(c) {
		t.Fatalf("%s: concurrency exceeded: %d > %d", name, col.maxInFlight, c)
	}
}

// TestDifferential runs 200 randomized workloads through all three
// implementations and asserts they all produce identical, correct results.
func TestDifferential(t *testing.T) {
	t.Parallel()
	base := rand.New(rand.NewSource(20260531))

	for it := 0; it < 200; it++ {
		n := base.Intn(250)
		c := base.Intn(16) + 1

		durs := make([]time.Duration, n)
		for i := range durs {
			durs[i] = time.Duration(base.Intn(40)) * time.Microsecond
		}
		work := func(v int) {
			if durs[v] > 0 {
				time.Sleep(durs[v])
			}
		}

		ci := runIterWithTimeout(t, n, c, work)
		ce := runErrgroup(n, c, work)
		cp := runPool(n, c, work)

		assertExactlyOnce(t, "iter", ci, n, c)
		assertExactlyOnce(t, "errgroup", ce, n, c)
		assertExactlyOnce(t, "pool", cp, n, c)

		if t.Failed() {
			t.Fatalf("failed at iteration %d (n=%d c=%d)", it, n, c)
		}
	}
}

// ---- lightweight variants for benchmarking framework overhead only ----

func benchIter(n, c int) {
	var sink int64
	var wg sync.WaitGroup
	for r := range iter.SeqFromList(c, rangeN(n)) {
		r.StartNextTask()
		wg.Add(1)
		go func(r iter.Ret[int]) {
			defer wg.Done()
			defer r.MarkTaskAsComplete()
			atomic.AddInt64(&sink, int64(r.Value))
		}(r)
	}
	wg.Wait()
	_ = sink
}

func benchErrgroup(n, c int) {
	var sink int64
	g, _ := errgroup.WithContext(context.Background())
	g.SetLimit(c)
	for _, v := range rangeN(n) {
		v := v
		g.Go(func() error {
			atomic.AddInt64(&sink, int64(v))
			return nil
		})
	}
	_ = g.Wait()
	_ = sink
}

func benchPool(n, c int) {
	var sink int64
	sem := make(chan struct{}, c)
	var wg sync.WaitGroup
	for _, v := range rangeN(n) {
		v := v
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			atomic.AddInt64(&sink, int64(v))
		}()
	}
	wg.Wait()
	_ = sink
}

func BenchmarkCompare(b *testing.B) {
	configs := []struct{ n, c int }{
		{100, 4},
		{100, 16},
		{1000, 8},
		{1000, 64},
	}
	for _, cfg := range configs {
		n, c := cfg.n, cfg.c
		b.Run(fmt.Sprintf("iter/n%d_c%d", n, c), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				benchIter(n, c)
			}
		})
		b.Run(fmt.Sprintf("errgroup/n%d_c%d", n, c), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				benchErrgroup(n, c)
			}
		})
		b.Run(fmt.Sprintf("pool/n%d_c%d", n, c), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				benchPool(n, c)
			}
		})
	}
}
