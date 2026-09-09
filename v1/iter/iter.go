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
	// (exhausted, closed and count). The source is also read under this mutex,
	// so source implementations do not need their own synchronization merely
	// because the consumer drives Sequence concurrently.
	var mtx = sync.Mutex{}
	var exhausted = false // the source signalled "done"
	var closed = false    // the output channel has been closed
	var count = 0         // tasks produced but not yet marked complete (in flight)

	// maxConcurrency is a counting semaphore bounding both source reservation
	// and the number of in-flight tasks. A producer must own a slot before it
	// calls Next(), so a side-effecting source cannot be pulled one item ahead of
	// the advertised concurrency limit.
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
		// Reserve capacity before touching the source. Previously Next() was
		// called before acquiring this slot, permitting one item of source
		// read-ahead beyond the configured concurrency bound.
		maxConcurrency <- struct{}{}

		mtx.Lock()

		if exhausted || closed {
			mtx.Unlock()
			<-maxConcurrency
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
			<-maxConcurrency
			return
		}

		count++
		mtx.Unlock()

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
