package iter

import (
	"fmt"
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

func doUnlock(locks ...*sync.Mutex) {
	for _, lck := range locks {
		if lck != nil {
			lck.Unlock()
		}
	}
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
	var lck = sync.Mutex{}
	var closingOrClosed = false
	var maxConcurrency = make(chan int, max(1, concurrency))
	var done = false
	var count = 0

	var writeToChan func(m *sync.Mutex)

	writeToChan = func(m *sync.Mutex) {

		lck.Lock()

		if closingOrClosed {
			fmt.Println("warning channel closed (Continue called more than once?)")
			doUnlock(m, &lck)
			return
		}

		// they are all reading from the same channel
		// so if the .Next call blocks, then all the other Next() calls would block also, anyway
		// so it's ok (and probably imperative) to surround the Next() call with locks lol fml
		var b, v = h.Next()
		if b {
			// we now know the channel/stream is done reading from, etc
			closingOrClosed = true
			if !done && count <= 0 {
				done = true
				close(c)
			}
			doUnlock(m, &lck)
			return
		}

		if done {
			doUnlock(m, &lck)
			return
		}

		maxConcurrency <- 1
		count++
		doUnlock(m, &lck)

		var called1 = false
		var called2 = false
		var l = sync.Mutex{}

		c <- Ret[T]{b, v, func() {
			l.Lock()
			if !called1 {
				called1 = true
				if !closingOrClosed {
					// we pass &l pass that we block here ***+++
					go writeToChan(&l)
				}
				return
			}
			l.Unlock()
		}, func() {
			l.Lock() // need to block here ***+++
			if !called2 {
				called2 = true
				count--
				<-maxConcurrency
				if !done && count <= 0 {
					done = true
					close(c)
				}
			}
			l.Unlock()
		}}

	}

	go writeToChan(nil)
	return c

}
