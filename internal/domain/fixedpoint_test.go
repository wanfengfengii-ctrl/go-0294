package domain

import (
	"math"
	"testing"
)

func TestRoundDivHalfAwayFromZero(t *testing.T) {
	cases := []struct {
		n, d, want int64
	}{
		{5, 2, 3},
		{-5, 2, -3},
		{3, 2, 2},
		{-3, 2, -2},
		{7, 3, 2},
		{-7, 3, -2},
		{4, 2, 2},
		{-4, 2, -2},
		{1, 3, 0},
		{-1, 3, 0},
	}
	for _, c := range cases {
		got, err := RoundDiv(c.n, c.d)
		if err != nil {
			t.Fatalf("RoundDiv(%d,%d) error: %v", c.n, c.d, err)
		}
		if got != c.want {
			t.Errorf("RoundDiv(%d,%d) = %d, want %d", c.n, c.d, got, c.want)
		}
	}
}

func TestRoundDivZeroDivisor(t *testing.T) {
	_, err := RoundDiv(10, 0)
	if err == nil || err.(*Error).Code != CodeFixedOverflow {
		t.Fatalf("want CodeFixedOverflow, got %v", err)
	}
}

func TestMulScaleRescalesWithRounding(t *testing.T) {
	got, err := MulScale(3, 4, 2) // (3*4)/2 = 6
	if err != nil {
		t.Fatal(err)
	}
	if got != 6 {
		t.Fatalf("MulScale = %d, want 6", got)
	}
}

func TestMulScaleOverflowRejected(t *testing.T) {
	_, err := MulScale(math.MaxInt64, 2, 1)
	if err == nil || err.(*Error).Code != CodeFixedOverflow {
		t.Fatalf("want CodeFixedOverflow, got %v", err)
	}
}

func TestMulScaleZeroScale(t *testing.T) {
	_, err := MulScale(10, 10, 0)
	if err == nil || err.(*Error).Code != CodeFixedOverflow {
		t.Fatalf("want CodeFixedOverflow, got %v", err)
	}
}
