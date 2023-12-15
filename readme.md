
### Async Iterators in Golang

Control iteration with gasp! - callbacks

To be an Sync Iterable, all you need is a next method, so that the struct adheres to the HasNext interface:

```golang
type HasNext[T any] interface {
  Next() (bool, T)
}
```

for a synchronous iterable, we can implement one like so:

```yaml
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
	return &Iterable[int]{n, 55}
}
```

For convenience, we also have an "async" iterable:

```golang
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
```

you can pass a chan to "AsyncSequence" and it will read from the chan, for example, pass it like so:


```golang

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

	return r  // return chan
}

```

and then pass this into the AsyncSequence, like so:


```golang

	lrt := longRunningTask()

	for r := range iter.AsyncSequence(lrt) {

		if r.Done {
			break
		}

		fmt.Println(r)
		time.Sleep(time.Second * 1)
		r.Contnue()  // must call this continue in the loop
	}

```

