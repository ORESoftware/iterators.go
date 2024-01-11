package basic_test

import (
	"testing"
	"fmt"
)

// Example function to test
func Sum(a, b int) int {
	return a + b
}

// TestSum tests the Sum function
func TestSum(t *testing.T) {
	got := Sum(5, 5)
	want := 10

	if got != want {
		t.Errorf("Sum(5, 5) = %d; want %d", got, want)
	}
}

// TestSumTableDriven is a table-driven test for the Sum function
func TestSumTableDriven(t *testing.T) {
	var tests = []struct {
		a, b int
		want int
	}{
		{1, 2, 3},
		{10, 20, 30},
		{0, 0, 0},
		{-1, -1, -2},
	}

	for _, tt := range tests {
		testname := fmt.Sprintf("%d+%d", tt.a, tt.b)
		t.Run(testname, func(t *testing.T) {
			ans := Sum(tt.a, tt.b)
			if ans != tt.want {
				t.Errorf("got %d, want %d", ans, tt.want)
			}
		})
	}
}

// ExampleSum is an example function (demonstration) for the Sum function
func ExampleSum() {
	sum := Sum(3, 4)
	fmt.Println(sum)
	// Output: 7
}
