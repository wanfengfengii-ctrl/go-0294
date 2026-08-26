package domain

import "math"

// Fixed-point physical quality metric computation. Every metric uses a
// documented scale factor, signed 64-bit integer arithmetic, uniform
// half-away-from-zero rounding and full overflow/div-zero/time-regression
// checks. A rejected computation never returns a truncated or saturated value.

// Published scale factors. All results are integers at these scales so they
// can round-trip through JSON and the relational store without floating point.
const (
	ScaleStress = 1000 // surface/edge stress in MPa * 1000
	ScaleBow    = 1000 // overall bow in micron * 1000
	ScaleRate   = 1000 // temperature/pressure rate in unit/min * 1000
	ScaleArea   = 1000 // bubble area fraction in per-mille
	ScaleEnergy = 1000 // pressure-time integral in kPa*s * 1000
)

// SurfaceStress computes stress = force / area * ScaleStress with rounding and
// overflow protection. A non-positive area rejects the call.
func SurfaceStress(force, area int64) (int64, error) {
	if area <= 0 {
		return 0, NewError(CodeFixedOverflow, "stress area must be positive")
	}
	return MulScale(force, ScaleStress, area)
}

// EdgeStress computes the edge-stress metric using the same fixed-point path as
// SurfaceStress; it exists as a named entry point so call sites are explicit
// about which threshold the reading is compared against.
func EdgeStress(force, area int64) (int64, error) {
	return SurfaceStress(force, area)
}

// Bow computes overall bow as deviation / span * ScaleBow. A zero or negative
// span rejects the call.
func Bow(deviation, span int64) (int64, error) {
	if span <= 0 {
		return 0, NewError(CodeFixedOverflow, "bow span must be positive")
	}
	return MulScale(deviation, ScaleBow, span)
}

// RampRate computes a temperature or pressure rate as delta / deltaTime in
// scaled units per minute. A non-positive interval rejects the call.
func RampRate(delta, deltaTime int64) (int64, error) {
	if deltaTime <= 0 {
		return 0, NewError(CodeFixedOverflow, "rate interval must be positive")
	}
	return MulScale(delta, ScaleRate, deltaTime)
}

// HoldTime returns the scaled equivalent hold duration between two strictly
// increasing logical times. A non-positive or regressing interval rejects.
func HoldTime(start, end int64) (int64, error) {
	if end <= start {
		return 0, NewError(CodeFixedOverflow, "hold interval must be positive")
	}
	return end - start, nil
}

// PressureIntegral computes the scaled pressure-time integral over a strictly
// increasing sample sequence using the trapezoid rule in integer arithmetic:
// sum over intervals of (p_i + p_{i+1}) * dt_i / 2, rescaled by ScaleEnergy.
// Out-of-order or duplicate logical times and overflow are rejected.
func PressureIntegral(samples []Sample) (int64, error) {
	if len(samples) < 2 {
		return 0, NewError(CodeSampleGap, "pressure integral needs at least two samples")
	}
	var total int64
	for i := 1; i < len(samples); i++ {
		dt := samples[i].LogicalTime - samples[i-1].LogicalTime
		if dt <= 0 {
			return 0, NewError(CodeSampleGap, "sample logical time not strictly increasing")
		}
		if addOverflow(samples[i-1].Value, samples[i].Value) {
			return 0, NewError(CodeFixedOverflow, "pressure sum overflow")
		}
		sum := samples[i-1].Value + samples[i].Value
		area, err := MulScale(sum, dt, 2)
		if err != nil {
			return 0, err
		}
		if addOverflow(total, area) {
			return 0, NewError(CodeFixedOverflow, "integral overflow")
		}
		total += area
	}
	return total, nil
}

// BubbleRate computes the bubble area fraction as bubbleArea / totalArea *
// ScaleArea (per-mille). A non-positive total area rejects.
func BubbleRate(bubbleArea, totalArea int64) (int64, error) {
	if totalArea <= 0 {
		return 0, NewError(CodeFixedOverflow, "bubble total area must be positive")
	}
	if bubbleArea < 0 {
		return 0, NewError(CodeFixedOverflow, "bubble area must be non-negative")
	}
	return MulScale(bubbleArea, ScaleArea, totalArea)
}

// addOverflow reports whether a+b overflows signed 64-bit arithmetic.
func addOverflow(a, b int64) bool {
	if (b > 0 && a > math.MaxInt64-b) || (b < 0 && a < math.MinInt64-b) {
		return true
	}
	return false
}
