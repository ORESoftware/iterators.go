package iter

import (
	"fmt"
	"sync"
)

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

//func (t *TransformStream[int, int]) Transform(c chan int) chan int {
//	k := make(chan int)
//	for x := range c {
//		k <- x
//	}
//	return k
//}

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

type HasNexter[T any] struct {
	c chan T
}

func (h HasNexter[T]) Next() (bool, T) {
	value, ok := <-h.c
	return !ok, value
}

func AsyncSequence[T any](v chan T) chan Ret[T] {
	return Sequence[T](HasNexter[T]{v})
}

//func SequenceFromROChan[T any](v <-chan T) chan Ret[T] {
//	return Sequence[T](HasNexter[T]{v})
//}

// TODO: do Read() interface

func doUnlock(m *sync.Mutex) {
	if m != nil {
		m.Unlock()
	}
}

type HasNext[T any] interface {
	Next() (bool, T)
}

type internalSeq[T any] struct {
	n struct{ Next func() (bool, T) }
}

func (s *internalSeq[T]) Next() (bool, T) {
	return s.n.Next()
}

func Seq[T any](req struct{ Next func() (bool, T) }) chan Ret[T] {
	return Sequence[T](&internalSeq[T]{req})
}

func Sequence[T any](even HasNext[T]) chan Ret[T] {

	var c = make(chan Ret[T], 1)
	var lck = sync.Mutex{}
	var closingOrClosed = false
	var concurrency = make(chan int, 5)
	var done = false
	var count = 0

	var x func(m *sync.Mutex)

	x = func(m *sync.Mutex) {

		lck.Lock()

		if closingOrClosed {
			fmt.Println("warning channel closed (Continue called1 twice?)")
			doUnlock(m)
			lck.Unlock()
			return
		}

		// they are all reading from the same channel
		// so if this call blocks, then all the other Next() calls would block too anyway
		// so it's ok (and probably imperative) to surround the Next() call with locks lol fml
		var b, v = even.Next()
		if b {
			// we now know the channel/stream is done reading from, etc
			closingOrClosed = true
			if !done && count <= 0 {
				done = true
				close(c)
			}
			doUnlock(m)
			lck.Unlock()
			return
		}

		if done {
			doUnlock(m)
			lck.Unlock()
			return
		}

		concurrency <- 1
		count++
		doUnlock(m)
		lck.Unlock()

		var called1 = false
		var called2 = false
		var l = sync.Mutex{}

		c <- Ret[T]{b, v, func() {
			l.Lock()
			if !called1 {
				called1 = true
				if !closingOrClosed {
					go x(&l)
				}
				return
			}
			l.Unlock()
		}, func() {
			l.Lock()
			if !called2 {
				called2 = true
				count--
				<-concurrency
				if !done && count <= 0 {
					done = true
					close(c)
				}
			}
			l.Unlock()
		}}

		//if closingOrClosed {
		//	close(c)
		//}
	}

	go x(nil)
	return c

}
