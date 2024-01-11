
### Go-Iterators

```golang

// Define two simple interfaces
package foo

type Reader interface {
    Read(p []byte) (n int, err error)
}

type Writer interface {
    Write(p []byte) (n int, err error)
}

// Define a new interface that combines Reader and Writer
type ReadWriter interface {
    Reader
    Writer
}
```