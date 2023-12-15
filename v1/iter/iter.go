package iter

import (
	"fmt"
	"sync"
)

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

type HasNext[T any] interface {
	Next() (bool, T)
}

func Sequence[T any](even HasNext[T]) chan Ret[T] {

	var c = make(chan Ret[T], 1)
	var lck = sync.Mutex{}
	var closingOrClosed = false
	var concurrency = make(chan int, 5)
	var done = false
	var count = 0

	var x func()

	x = func() {
		lck.Lock()
		if closingOrClosed {
			fmt.Println("warning channel closed (Continue called1 twice?)")
			lck.Unlock()
			return
		}
		concurrency <- 1

		var b, v = even.Next()
		if b {
			closingOrClosed = true
			if !done && count <= 0 {
				done = true
				close(c)
			}
			lck.Unlock()
			return
		}

		lck.Unlock()

		if done {
			return
		}

		var called1 = false
		var called2 = false

		var l = sync.Mutex{}
		count++
		c <- Ret[T]{b, v, func() {
			l.Lock()
			if !called1 {
				called1 = true
				l.Unlock()
				if !closingOrClosed {
					go x()
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

	go x()
	return c

}
