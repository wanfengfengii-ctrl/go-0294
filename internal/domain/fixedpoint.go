package domain

import "math"

// Fixed-point arithmetic for all physical quality metrics. Every input is a
// signed integer in its published scale (microns, kPa*scale, °C*scale, ...).
// Rounding is uniform half-away-from-zero, and every multiply-add is checked
// for signed 64-bit overflow before it is committed, so a rejected computation
// never yields a truncated or saturated value.

// RoundDiv divides n by d using half-away-from-zero rounding. It returns
// CodeFixedOverflow for a zero divisor.
func RoundDiv(n, d int64) (int64, error) {
	if d == 0 {
		return 0, NewError(CodeFixedOverflow, "division by zero")
	}
	if n == 0 {
		return 0, nil
	}
	q := n / d
	r := n % d
	// Half-away-from-zero: bump the quotient's magnitude when the doubled
	// remainder magnitude reaches the divisor magnitude.
	if abs64(r)*2 >= abs64(d) {
		if n > 0 {
			q++
		} else {
			q--
		}
	}
	return q, nil
}

// MulScale multiplies two scaled integers and rescales by dividing through the
// given scale, with half-away-from-zero rounding and full overflow detection.
func MulScale(a, b, scale int64) (int64, error) {
	if scale == 0 {
		return 0, NewError(CodeFixedOverflow, "zero scale factor")
	}
	if mulOverflow(a, b) {
		return 0, NewError(CodeFixedOverflow, "multiply-add overflow")
	}
	return RoundDiv(a*b, scale)
}

// mulOverflow reports whether a*b overflows signed 64-bit arithmetic.
func mulOverflow(a, b int64) bool {
	if a == 0 || b == 0 {
		return false
	}
	switch {
	case a > 0:
		if b > 0 {
			return a > math.MaxInt64/b
		}
		return b < math.MinInt64/a
	case b > 0:
		return a < math.MinInt64/b
	default: // a < 0 && b < 0
		return a < math.MaxInt64/b
	}
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
