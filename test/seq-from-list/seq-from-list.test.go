package main

import (
	"github.com/oresoftware/go-iterators/v1/iter"
	"fmt"
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

func main() {

	for r := range iter.SeqFromList([]int{1, 2, 3}) {

		if r.Done {
			panic("never should be done")
		}

		// time.Sleep(time.Millisecond * 500)

		go func(r iter.Ret[int]) {

			fmt.Println("value e:", r)
			time.Sleep(time.Millisecond * 10)
			r.StartNextTask()

			go func(r iter.Ret[int]) {
				time.Sleep(time.Millisecond * 500)
				r.StartNextTask()
				r.MarkTaskAsComplete()
			}(r)

			// fmt.Println("value z:", r)
			// if !r.Done {
			//
			// }
		}(r)

	}

}
