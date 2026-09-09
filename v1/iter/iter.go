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
		var zero T
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
		var zero T
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

type IOReader interface {
	Read(p []byte) (n int, err error)
}

type Reader[T any] interface {
	Read(p []T) (n int, err error)
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

	// mtx guards every piece of shared mutable state below. Source Next calls are
	// serialized as well, so a source implementation does not need its own
	// synchronization merely because the consumer drives Sequence concurrently.
	var mtx = sync.Mutex{}
	var exhausted = false
	var closed = false
	var count = 0

	// maxConcurrency is both the task concurrency bound and the source-reservation
	// bound. A producer must own a slot before calling Next(), so a side-effecting
	// or blocking source can never be pulled for item N+1 while N tasks already
	// hold all configured slots.
	var maxConcurrency = make(chan struct{}, max(1, concurrency))

	var maybeClose = func() {
		if !closed && exhausted && count <= 0 {
			closed = true
			close(c)
		}
	}

	var produce func()

	produce = func() {
		// Reserve capacity before touching the source. The previous order called
		// Next() and incremented count before acquiring this slot, which allowed
		// one item of read-ahead beyond the advertised concurrency limit.
		maxConcurrency <- struct{}{}

		mtx.Lock()
		if exhausted || closed {
			mtx.Unlock()
			<-maxConcurrency
			return
		}

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
			startOnce.Do(func() {
				go produce()
			})
		}

		var markTaskAsComplete = func() {
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
