# Async Iterators in Golang

Control iteration with — gasp! — callbacks.

This package turns any "pull" source (a slice, a channel, or anything with a
`Next()` method) into a channel of results that you can `range` over, while
giving you explicit, callback-driven control over **back-pressure** and
**bounded concurrency**.

```bash
go get github.com/oresoftware/go-iterators
```

```go
import "github.com/oresoftware/go-iterators/v1/iter"
```

---

## Core concepts

### The `HasNext` interface

A source is anything that can produce values one at a time:

```go
type HasNext[T any] interface {
    // Next returns (done, value). When done is true, iteration stops and the
    // value is ignored.
    Next() (bool, T)
}
```

### `Sequence` and `Ret`

`Sequence` is the heart of the package. You give it a concurrency limit and a
source; you get back a channel of `Ret[T]` values to `range` over:

```go
func Sequence[T any](concurrency int, h HasNext[T]) chan Ret[T]

type Ret[T any] struct {
    Done               bool      // always false for delivered values; see note below
    Value              T         // the produced value
    StartNextTask      func()    // request the next element from the source
    MarkTaskAsComplete func()    // signal this element's work is finished
}
```

### The contract (read this!)

For **every** delivered `Ret`:

1. Call `r.StartNextTask()` **once** to ask the iterator to fetch the next
   element. If you never call it, the pipeline stops producing and the channel
   never closes (your `range` loop will block forever).
2. Call `r.MarkTaskAsComplete()` **once** when you are done with the element.
   This frees a concurrency slot and lets the channel close when the source is
   exhausted.

Notes:

- Both callbacks are **idempotent** — calling either more than once is a safe
  no-op, so duplicate calls won't panic.
- You may call them in **either order**.
- `r.Done` is always `false` for delivered values (it exists for API symmetry).
  Don't loop on it — the `range` ends naturally when the channel closes, i.e.
  once the source is exhausted **and** every in-flight task has been completed.
- `concurrency` is the maximum number of tasks that may be **in flight** at once
  (produced but not yet `MarkTaskAsComplete`d). A value `< 1` is treated as `1`.
- `Sequence` calls `Next()` under a lock, so your source does **not** need its
  own synchronization.

---

## Quick start — sequential processing

The simplest pattern: do the work, then advance and complete before moving on.
This processes one element at a time, in order.

```go
package main

import (
    "fmt"

    "github.com/oresoftware/go-iterators/v1/iter"
)

func main() {
    for r := range iter.SeqFromList(2, []int{1, 2, 3, 4, 5}) {
        fmt.Println("value:", r.Value)

        r.StartNextTask()      // request the next element
        r.MarkTaskAsComplete() // release this task's concurrency slot
    }
}
```

---

## Bounded concurrency — fan-out

To process up to `concurrency` elements in parallel, call `StartNextTask()`
immediately (so production runs ahead) and defer `MarkTaskAsComplete()` to your
worker goroutine. The iterator will keep at most `concurrency` tasks in flight.

```go
package main

import (
    "fmt"
    "sync"
    "time"

    "github.com/oresoftware/go-iterators/v1/iter"
)

func process(url string) {
    time.Sleep(100 * time.Millisecond) // pretend to do I/O
    fmt.Println("processed:", url)
}

func main() {
    urls := []string{"a", "b", "c", "d", "e", "f", "g", "h"}

    var wg sync.WaitGroup

    // At most 3 of these run at the same time.
    for r := range iter.SeqFromList(3, urls) {
        r.StartNextTask() // ask for the next item right away to fan out

        wg.Add(1)
        go func(r iter.Ret[string]) {
            defer wg.Done()
            process(r.Value)
            r.MarkTaskAsComplete() // free the slot when the work is done
        }(r)
    }

    wg.Wait() // wait for the last in-flight workers to finish
}
```

> Tip: when you defer completion to a goroutine, capture `r` as a parameter
> (as above) so each goroutine gets its own copy.

---

## Sources

All constructors share the same shape: `(concurrency int, source) chan Ret[T]`.

### From a slice — `SeqFromList`

```go
for r := range iter.SeqFromList(4, []string{"x", "y", "z"}) {
    fmt.Println(r.Value)
    r.StartNextTask()
    r.MarkTaskAsComplete()
}
```

### From a slice of pointers — `SeqFromListOfPointers`

Dereferences each pointer for you:

```go
a, b, c := 1, 2, 3
for r := range iter.SeqFromListOfPointers(2, []*int{&a, &b, &c}) {
    fmt.Println(r.Value) // 1, 2, 3
    r.StartNextTask()
    r.MarkTaskAsComplete()
}
```

### From a channel — `AsyncSequence`

Reads from a channel until it is closed. Great for streaming / long-running
producers.

```go
func longRunningTask() chan int {
    out := make(chan int)
    go func() {
        defer close(out) // closing the channel signals "done"
        for i := 1; i < 10; i++ {
            time.Sleep(time.Second) // simulate a workload
            out <- i
        }
    }()
    return out
}

func main() {
    for r := range iter.AsyncSequence[int](4, longRunningTask()) {
        fmt.Println("got:", r.Value)

        r.StartNextTask()
        go func(r iter.Ret[int]) {
            // ... do something with r.Value ...
            r.MarkTaskAsComplete()
        }(r)
    }
}
```

### From a `Next` closure — `Seq`

When you don't want to define a type, pass an anonymous struct with a `Next`
field:

```go
i := 0
src := struct{ Next func() (bool, int) }{
    Next: func() (bool, int) {
        if i >= 5 {
            return true, 0 // done
        }
        v := i
        i++
        return false, v
    },
}

for r := range iter.Seq[int](3, src) {
    fmt.Println(r.Value) // 0, 1, 2, 3, 4
    r.StartNextTask()
    r.MarkTaskAsComplete()
}
```

### From an `HsNext` closure — `FromNext`

Same idea as `Seq`, using the exported `HsNext[T]` helper type:

```go
i := 0
next := iter.HsNext[int]{
    Next: func() (bool, int) {
        if i >= 3 {
            return true, 0
        }
        i++
        return false, i
    },
}

for r := range iter.FromNext[int](3, next) {
    fmt.Println(r.Value) // 1, 2, 3
    r.StartNextTask()
    r.MarkTaskAsComplete()
}
```

### A custom type — `Sequence` + `HasNext`

Implement `Next()` on your own type and pass it straight to `Sequence`. Because
`Sequence` serializes calls to `Next()`, no internal locking is required.

```go
type Counter struct {
    n, max int
}

func (c *Counter) Next() (bool, int) {
    if c.n >= c.max {
        return true, 0 // done
    }
    v := c.n
    c.n++
    return false, v
}

func main() {
    for r := range iter.Sequence[int](8, &Counter{n: 0, max: 100}) {
        fmt.Println(r.Value)
        r.StartNextTask()
        r.MarkTaskAsComplete()
    }
}
```

---

## Common pitfalls

- **Forgetting `StartNextTask()`** → only the first element is ever produced and
  the `range` blocks forever. Call it once per delivered item.
- **Forgetting `MarkTaskAsComplete()`** → the concurrency slot is never released,
  so the channel never closes and the `range` blocks once `concurrency` items
  are in flight. Call it once per delivered item (typically when your work for
  that item finishes).
- **Not waiting for deferred workers** → if you complete tasks inside goroutines,
  use a `sync.WaitGroup` (as shown above) so `main`/your test doesn't exit before
  the last workers run.

---

## Testing

The package ships with unit, randomized property, and fuzz tests. Run them like
any other Go tests:

```bash
# Run the whole suite
go test ./...

# Run with the race detector (recommended — this package is all about concurrency)
go test -race ./...

# Run just the iterator package, verbose
go test -race -v ./v1/iter/
```

### Fuzzing

A native Go fuzz target (`FuzzSequence`) randomizes the source type, the
concurrency limit, the element count, and every per-item scheduling decision
(callback order, inline vs. goroutine, duplicate calls). It asserts the
iterator never panics, never races, always terminates, delivers every element
exactly once, and never exceeds the configured concurrency.

```bash
# Coverage-guided fuzzing with the race detector
go test -race -run '^$' -fuzz '^FuzzSequence$' -fuzztime 30s ./v1/iter/
```

### Writing your own test

A good test drives the iterator to completion and checks that every element was
delivered exactly once. Always run concurrency-sensitive tests with `-race`.

```go
package mypkg

import (
    "sort"
    "sync"
    "testing"

    "github.com/oresoftware/go-iterators/v1/iter"
)

func TestDrainExactlyOnce(t *testing.T) {
    const concurrency = 8
    input := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

    var mu sync.Mutex
    seen := map[int]int{}

    var wg sync.WaitGroup
    for r := range iter.SeqFromList(concurrency, input) {
        r.StartNextTask() // fan out

        wg.Add(1)
        go func(r iter.Ret[int]) {
            defer wg.Done()

            mu.Lock()
            seen[r.Value]++
            mu.Unlock()

            r.MarkTaskAsComplete()
        }(r)
    }
    wg.Wait()

    if len(seen) != len(input) {
        t.Fatalf("expected %d distinct values, got %d", len(input), len(seen))
    }
    for _, v := range input {
        if seen[v] != 1 {
            t.Errorf("value %d delivered %d times, want exactly once", v, seen[v])
        }
    }

    // (Optional) verify the exact set that came through.
    got := make([]int, 0, len(seen))
    for v := range seen {
        got = append(got, v)
    }
    sort.Ints(got)
    _ = got
}
```

> Because deferred workers do the recording, the `range` loop only reads from
> the channel — the shared `seen` map is guarded by a mutex, and `wg.Wait()`
> ensures every worker has finished before the assertions run.

See [`v1/iter/iter_test.go`](v1/iter/iter_test.go) and
[`v1/iter/iter_fuzz_test.go`](v1/iter/iter_fuzz_test.go) for the full set of
examples, including bounded-concurrency, idempotent-callback, channel-source,
and empty-source tests.
