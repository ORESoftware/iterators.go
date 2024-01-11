package struct_test

import (
	"fmt"
	"github.com/oresoftware/go-iterators/v1/iter"
	"time"
	"testing"
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

type Iterable[T any] struct {
	Val  T
	End  T
	Next func() (bool, T)
}

func acceptStructWithNext[T any](r struct{ Next func() (bool, T) }) {
	fmt.Println(r)
}

func TestStruct(t *testing.T) {

	// iterable := Iterable[int]{
	//	Val: 0,
	//	End: 100,
	//	Next: func() (bool, int) {
	//		return true, 0
	//	},
	// }

	iterable := struct {
		Next func() (bool, int)
	}{
		Next: func() (bool, int) {
			return true, 0
		},
	}

	acceptStructWithNext[int](iterable)

	for r := range iter.Seq[int](3, iterable) {

		if r.Done {
			panic("never should be done")
		}

		// time.Sleep(time.Millisecond * 500)

		go func(r iter.Ret[int]) {

			fmt.Println("value e:", r)
			time.Sleep(time.Millisecond * 500)
			r.StartNextTask()
			r.MarkTaskAsComplete()

			go func(r iter.Ret[int]) {
				time.Sleep(time.Millisecond * 500)
				r.StartNextTask()
			}(r)

			// fmt.Println("value z:", r)
			// if !r.Done {
			//
			// }
		}(r)

	}

}
