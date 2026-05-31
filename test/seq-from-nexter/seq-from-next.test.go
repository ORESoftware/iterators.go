package main

import (
	"fmt"
	"github.com/oresoftware/go-iterators/v1/iter"
	"sync"
	"time"
)

func main() {

	// A simple source: emit the integers 1..5, then signal done.
	i := 0
	var mtx sync.Mutex

	n := iter.HsNext[int]{
		Next: func() (bool, int) {
			mtx.Lock()
			defer mtx.Unlock()
			if i >= 5 {
				return true, 0
			}
			i++
			return false, i
		},
	}

	for r := range iter.FromNext[int](3, n) {

		if r.Done {
			panic("never should be done")
		}

		go func(r iter.Ret[int]) {
			fmt.Println("value e:", r.Value)
			time.Sleep(time.Millisecond * 10)
			r.StartNextTask()
			r.MarkTaskAsComplete()
		}(r)

	}

}
