package iter

import (
	"sync"
)

type IReader interface {
	Read(p []byte) (n int, err error)
}

type IWriter interface {
	Write(p []byte) (n int, err error)
}

// Define a new interface that combines Reader and Writer
type IReadWriter interface {
	IReader
	IWriter
}

type ConnectToProducer[T any] interface {
	ConnectToProducer() chan T
}

type ConnectToConsumer[T any] interface {
	ConnectToConsumer() chan T
}

type ReadStream[T any, K any] struct {
	c <-chan T
}

type DuplexStream[T any] struct {
	c chan T
}

type ITransformStream[T any, K any] interface {
	Transform(c chan T) chan K
}

type TransformStream[K any, T any] struct {
	c chan T
}

// func (t *TransformStream[int, int]) Transform(c chan int) chan int {
//	k := make(chan int)
//	for x := range c {
//		k <- x
//	}
//	return k
// }

func (r *ReadStream[T, K]) Pipe() {

}

func (t *TransformStream[T, K]) Pipe() {

}

type Ret[T any] struct {
	Done               bool
	Value              T
	StartNextTask      func()
	MarkTaskAsComplete func()
}

type FromList[T any] struct {
	list  []T
	index int
	mtx   sync.Mutex
}

func (h *FromList[T]) Next() (bool, T) {
	h.mtx.Lock()
	defer h.mtx.Unlock()
	if h.index >= len(h.list) {
		var zero T // zero value of type T
		return true, zero
	}
	el := h.list[h.index]
	h.index++
	return false, el
}

type FromListOfPointers[T any] struct {
	list  []*T
	index int
	mtx   sync.Mutex
}

func (h *FromListOfPointers[T]) Next() (bool, T) {
	h.mtx.Lock()
	defer h.mtx.Unlock()
	if h.index >= len(h.list) {
		var zero T // zero value of type T
		return true, zero
	}
	el := h.list[h.index]
	h.index++
	return false, *el
}

func SeqFromList[T any](concurrency int, v []T) chan Ret[T] {
	return Sequence[T](concurrency, &FromList[T]{v, 0, sync.Mutex{}})
}

func SeqFromListOfPointers[T any](concurrency int, v []*T) chan Ret[T] {
	return Sequence[T](concurrency, &FromListOfPointers[T]{v, 0, sync.Mutex{}})
}

type HsNext[T any] struct {
	Next func() (bool, T)
}

type FromNexter[T any] struct {
	c   HsNext[T]
	mtx sync.Mutex
}

func (h *FromNexter[T]) Next() (bool, T) {
	h.mtx.Lock()
	defer h.mtx.Unlock()
	return h.c.Next()
}

func FromNext[T any](concurrency int, v HsNext[T]) chan Ret[T] {
	return Sequence[T](concurrency, &FromNexter[T]{v, sync.Mutex{}})
}

type FromChan[T any] struct {
	c   chan T
	mtx sync.Mutex
}

func (h *FromChan[T]) Next() (bool, T) {
	h.mtx.Lock()
	defer h.mtx.Unlock()
	value, ok := <-h.c
	return !ok, value
}

func AsyncSequence[T any](concurrency int, v chan T) chan Ret[T] {
	return Sequence[T](concurrency, &FromChan[T]{v, sync.Mutex{}})
}

// func SequenceFromROChan[T any](v <-chan T) chan Ret[T] {
//	return Sequence[T](FromChan[T]{v})
// }

// TODO: do Read() interface

type HasNext[T any] interface {
	Next() (bool, T)
}

type internalSeq[T any] struct {
	n   struct{ Next func() (bool, T) }
	mtx sync.Mutex
}

func (s *internalSeq[T]) Next() (bool, T) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	return s.n.Next()
}

func Seq[T any](concurrency int, req struct{ Next func() (bool, T) }) chan Ret[T] {
	return Sequence[T](concurrency, &internalSeq[T]{req, sync.Mutex{}})
}

// IOReader
type IOReader interface {
	Read(p []byte) (n int, err error)
}

type Reader[T any] interface {
	Read(p []T) (n int, err error) // the array represents how many times reading from a chan
}

type IOWriter interface {
	Write(p []byte) (n int, err error)
}

type Writer[T any] interface {
	Write(p []T) (n int, err error)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func Sequence[T any](concurrency int, h HasNext[T]) chan Ret[T] {

	var c = make(chan Ret[T], 1)

	// mtx guards every piece of shared mutable state below
	// (exhausted, closed and count). Previously the producer mutated this
	// state under one mutex while the per-task callbacks mutated it under a
	// different (per-task) mutex, which is a data race and could also close
	// the channel twice / send on a closed channel.
	var mtx = sync.Mutex{}
	var exhausted = false // the source signalled "done"
	var closed = false    // the output channel has been closed
	var count = 0         // tasks produced but not yet marked complete (in flight)

	// maxConcurrency is a counting semaphore bounding the number of in-flight tasks.
	var maxConcurrency = make(chan struct{}, max(1, concurrency))

	// maybeClose closes the output channel exactly once, once the source is
	// exhausted and there are no in-flight tasks left. Must be called with mtx held.
	var maybeClose = func() {
		if !closed && exhausted && count <= 0 {
			closed = true
			close(c)
		}
	}

	var produce func()

	produce = func() {

		mtx.Lock()

		if exhausted || closed {
			// Source is drained (or channel already closed): nothing left to do.
			mtx.Unlock()
			return
		}

		// All producers read from the same source under the lock, so Next() is
		// serialized. If Next() blocks then every producer would block on the
		// same source anyway, so holding the lock across it is acceptable.
		var done, value = h.Next()

		if done {
			exhausted = true
			maybeClose()
			mtx.Unlock()
			return
		}

		count++
		mtx.Unlock()

		// Acquire a concurrency slot outside the lock so the semaphore (rather
		// than the lock) is what actually bounds in-flight work. While this
		// task is in flight count >= 1, so the channel cannot be closed from
		// under the pending send below.
		maxConcurrency <- struct{}{}

		var startOnce sync.Once
		var completeOnce sync.Once

		var startNextTask = func() {
			// Idempotent: requests the next item from the source exactly once.
			startOnce.Do(func() {
				go produce()
			})
		}

		var markTaskAsComplete = func() {
			// Idempotent: releases this task's concurrency slot exactly once and
			// closes the output channel if the source is drained and this was the
			// last in-flight task.
			completeOnce.Do(func() {
				<-maxConcurrency
				mtx.Lock()
				count--
				maybeClose()
				mtx.Unlock()
			})
		}

		c <- Ret[T]{done, value, startNextTask, markTaskAsComplete}
	}

	go produce()
	return c

}
