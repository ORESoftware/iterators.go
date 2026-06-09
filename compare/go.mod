// Separate module so the published library (github.com/oresoftware/go-iterators)
// stays dependency-free. This module exists only to benchmark and
// differential-test the iterator against idiomatic Go concurrency patterns
// (golang.org/x/sync/errgroup and a stdlib semaphore worker pool).
module oresoftware.test/iterators-compare

go 1.24.0

require (
	github.com/oresoftware/go-iterators v0.0.0
	golang.org/x/sync v0.18.0
)

replace github.com/oresoftware/go-iterators => ../
