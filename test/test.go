package main

import (
	"fmt"
	"github.com/oresoftware/go-iterators/v1/iter"
	"math/rand"
	"time"
)

func longRunningTask() chan int {
	r := make(chan int)

	i := 1
	go func() {
		defer close(r)
		for i < 10 {
			// Simulate a workload.
			time.Sleep(time.Second * 1)
			r <- i
			i++
		}

	}()

	return r
}

type Iterable[T int] struct {
	Val T
	End T
}

func (n *Iterable[int]) Next() (bool, int) {
	n.Val = (n.Val) + (1)
	if n.Val >= n.End {
		return true, int(n.Val)
	}
	return false, int(n.Val)
}

func NewIterable() *Iterable[int] {
	n := 0
	return &Iterable[int]{n, 20}
}

func NewIterable0() *Iterable[int] {
	n := -1
	return &Iterable[int]{n, 0}
}

func NewIterable1() *Iterable[int] {
	n := 0
	return &Iterable[int]{n, 1}
}

func NewIterable2() *Iterable[int] {
	n := 1
	return &Iterable[int]{n, 3}
}

func main() {
	//even := NewIterable()
	//println(even.Next()) // Example usage
	//println(even.Next())

	//for {
	//	var b, v = even.Next()
	//	fmt.Println(v)
	//	if b {
	//		break
	//	}
	//}

	for r := range iter.Sequence[int](NewIterable()) {

		//time.Sleep(time.Millisecond * 500)

		r.StartNextTask()

		go func(r iter.Ret[int]) {

			fmt.Println("value z:", r)
			time.Sleep(time.Millisecond * 500)
			r.MarkTaskAsComplete()
			//fmt.Println("value z:", r)
			//if !r.Done {
			//
			//}
		}(r)

	}

	fmt.Println("exiting")

	s := make(chan int, 5)

	for r := range iter.Sequence[int](NewIterable()) {

		//time.Sleep(time.Millisecond * 500)

		s <- 1

		//if !r.Done {
		r.StartNextTask()
		//}

		//time.Sleep((1 / 1) * time.Second)

		//if true || !r.Done {
		//
		//	r.StartNextTask()
		//	r.StartNextTask()
		//
		//}
		fmt.Println("value:", r)

		if true || !r.Done {

			go func(r iter.Ret[int]) {
				source := rand.NewSource(time.Now().UnixNano())
				rnd := rand.New(source)

				// Generate a random number using the new rand instance
				i := rnd.Intn(1500) + 1
				time.Sleep(time.Duration(i) * time.Millisecond)
				<-s
				r.MarkTaskAsComplete()
			}(r)

			//r.StartNextTask()
		}

		//go func() {
		//	time.Sleep(1 * time.Second)
		//	r.StartNextTask()
		//}()

	}

	for r := range iter.Sequence[int](NewIterable0()) {
		fmt.Println("value 0:", r)

		if !r.Done {
			r.StartNextTask()
		}

		r.MarkTaskAsComplete()
	}

	for r := range iter.Sequence[int](NewIterable1()) {
		fmt.Println("value 1:", r)
		r.StartNextTask()
		r.MarkTaskAsComplete()
	}

	for r := range iter.Sequence[int](NewIterable2()) {
		fmt.Println("value 2:", r)
		r.StartNextTask()
		r.MarkTaskAsComplete()
	}

	fmt.Println("hello?")

	lrt := longRunningTask()

	for r := range iter.AsyncSequence(lrt) {

		if r.Done {
			break
		}

		fmt.Println("boof:", r)
		time.Sleep(time.Second * 1)
		r.StartNextTask()
		r.MarkTaskAsComplete()
	}

}
