package evidence

import (
	"curtainwall.example/assembly-gate/internal/domain"
)

// AutoclaveResult is the deterministic, fixed-point outcome of an autoclave
// cure evidence batch: the scaled pressure-time integral, the maximum
// pressurization rate and the hold duration, all overflow-checked.
type AutoclaveResult struct {
	PressureIntegral int64 `json:"pressure_integral"`
	MaxRampRate      int64 `json:"max_ramp_rate"`
	HoldDuration     int64 `json:"hold_duration"`
}

// ComputeAutoclave validates the autoclave continuous prefix and derives the
// fixed-point metrics from the ordered sample sequence. The pressure integral
// uses the trapezoid rule over strictly increasing logical times; the ramp
// rate is the maximum magnitude over pressurize/depressurize transitions; the
// hold duration is the time spanned by the hold phase.
func ComputeAutoclave(samples []SamplePoint) (*AutoclaveResult, error) {
	if err := ValidateSequence(samples); err != nil {
		return nil, err
	}
	orders, err := SegmentOrders(samples, true)
	if err != nil {
		return nil, err
	}
	if err := ValidateContinuousPrefix(orders); err != nil {
		return nil, err
	}

	ds := make([]domain.Sample, len(samples))
	for i, s := range samples {
		ds[i] = domain.Sample{LogicalTime: s.LogicalTime, Value: s.Value}
	}
	integral, err := domain.PressureIntegral(ds)
	if err != nil {
		return nil, err
	}

	result := &AutoclaveResult{PressureIntegral: integral}

	// Max ramp magnitude over any adjacent transition and hold duration.
	holdStart, holdEnd := int64(-1), int64(-1)
	for i := 1; i < len(samples); i++ {
		dt := samples[i].LogicalTime - samples[i-1].LogicalTime
		dv := samples[i].Value - samples[i-1].Value
		rate, err := domain.RampRate(dv, dt)
		if err != nil {
			return nil, err
		}
		if rate < 0 {
			rate = -rate
		}
		if rate > result.MaxRampRate {
			result.MaxRampRate = rate
		}
		if samples[i].Segment == "hold" && samples[i-1].Segment == "hold" {
			if holdStart < 0 {
				holdStart = samples[i-1].LogicalTime
			}
			holdEnd = samples[i].LogicalTime
		}
	}
	if holdStart >= 0 && holdEnd > holdStart {
		result.HoldDuration = holdEnd - holdStart
	}
	return result, nil
}
