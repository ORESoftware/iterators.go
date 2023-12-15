package iter

import (
	"fmt"
	"sync"
)

type HasNext[T any] interface {
	Next() (bool, T)
}

type Ret[T any] struct {
	Done    bool
	Value   T
	Contnue func()
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

func Sequence[T any](even HasNext[T]) chan Ret[T] {

	var c = make(chan Ret[T], 1)
	var lck = sync.Mutex{}
	var closingOrClosed = false

	var x func()

	x = func() {
		lck.Lock()
		if closingOrClosed {
			fmt.Println("warning channel closed (Continue called twice?)")
			lck.Unlock()
			return
		}
		var b, v = even.Next()
		if b {
			closingOrClosed = true
		}
		var called = false
		lck.Unlock()
		var l = sync.Mutex{}
		c <- Ret[T]{b, v, func() {
			l.Lock()
			if !called {
				called = true
				l.Unlock()
				x()
				return
			}
			l.Unlock()
		}}
		if closingOrClosed {
			close(c)
		}
	}

	go x()
	return c

}
