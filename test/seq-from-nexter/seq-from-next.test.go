package main

import (
	"github.com/oresoftware/go-iterators/v1/iter"
	"fmt"
	"time"
)

type Nextr struct {
	Val func() (bool, int)
}

func (n *Nextr) Next() (bool, int) {
	return true, 0
}

func main() {

	var n = Nextr{
		Val: func() (bool, int) {
			return true, 3
		}}

	for r := range iter.FromNext[Nextr](n) {

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
