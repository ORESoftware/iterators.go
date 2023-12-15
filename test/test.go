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

func newEven() *Iterable[int] {
	n := 0
	return &Iterable[int]{n, 55}
}

func main() {
	//even := newEven()
	//println(even.Next()) // Example usage
	//println(even.Next())

	even := newEven()

	//for {
	//	var b, v = even.Next()
	//	fmt.Println(v)
	//	if b {
	//		break
	//	}
	//}

	s := make(chan int, 5)

	for r := range iter.Sequence[int](even) {

		//time.Sleep(time.Millisecond * 500)

		s <- 1

		if !r.Done {
			r.Contnue()
		}

		//time.Sleep((1 / 1) * time.Second)

		//if true || !r.Done {
		//
		//	r.Contnue()
		//	r.Contnue()
		//
		//}
		fmt.Println("value:", r.Value)

		if true || !r.Done {

			go func() {
				source := rand.NewSource(time.Now().UnixNano())
				rnd := rand.New(source)

				// Generate a random number using the new rand instance
				i := rnd.Intn(1500) + 1
				time.Sleep(time.Duration(i) * time.Millisecond)
				<-s
			}()

			//r.Contnue()
		}

		//go func() {
		//	time.Sleep(1 * time.Second)
		//	r.Contnue()
		//}()

	}

	fmt.Println("hello?")

	lrt := longRunningTask()

	for r := range iter.AsyncSequence(lrt) {

		if r.Done {
			break
		}

		fmt.Println(r)
		time.Sleep(time.Second * 1)
		r.Contnue()
	}

}
