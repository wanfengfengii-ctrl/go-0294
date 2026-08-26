package instrument

import (
	"encoding/json"

	"curtainwall.example/assembly-gate/internal/domain"
)

// PayloadAdapter interprets a JSON payload as a scripted device result. It is
// the production adapter: a payload may carry an explicit failure outcome (to
// model a rejected, disconnected, timed-out or malformed device) or raw inputs
// from which a well-formed fixed-point measurement is computed.
type PayloadAdapter struct{}

// NewPayloadAdapter returns the production payload-driven adapter.
func NewPayloadAdapter() *PayloadAdapter {
	return &PayloadAdapter{}
}

type payloadEnvelope struct {
	Outcome    Outcome `json:"outcome"`
	Force      int64   `json:"force"`
	Area       int64   `json:"area"`
	Deviation  int64   `json:"deviation"`
	Span       int64   `json:"span"`
	BubbleArea int64   `json:"bubble_area"`
	TotalArea  int64   `json:"total_area"`
	Passed     *bool   `json:"passed"`
}

// Run parses the payload and produces the scripted outcome. An explicit
// failure outcome short-circuits to a non-OK result with no reading; otherwise
// a measurement is computed for the device kind.
func (a *PayloadAdapter) Run(device Device, payload string) Result {
	var env payloadEnvelope
	if err := json.Unmarshal([]byte(payload), &env); err != nil {
		return Result{Outcome: OutcomeMalformed}
	}
	switch env.Outcome {
	case OutcomeRejected, OutcomeDisconnect, OutcomeTimeout, OutcomeMalformed:
		return Result{Outcome: env.Outcome}
	}

	switch device {
	case DeviceStressMeter:
		v, err := domain.SurfaceStress(env.Force, env.Area)
		if err != nil {
			return Result{Outcome: OutcomeMalformed}
		}
		return Result{Outcome: OutcomeOK, Reading: &domain.Measurement{
			Kind: domain.MeasureSurfaceStress, Value: v, WellFormed: true,
		}}
	case DeviceOptical:
		v, err := domain.Bow(env.Deviation, env.Span)
		if err != nil {
			return Result{Outcome: OutcomeMalformed}
		}
		return Result{Outcome: OutcomeOK, Reading: &domain.Measurement{
			Kind: domain.MeasureBow, Value: v, WellFormed: true,
		}}
	case DeviceDestructive:
		if env.Passed == nil {
			return Result{Outcome: OutcomeMalformed}
		}
		ok := *env.Passed
		v := int64(0)
		if !ok {
			v = 1 // failure marker; rejected by threshold check downstream
		}
		return Result{Outcome: OutcomeOK, Reading: &domain.Measurement{
			Kind: domain.MeasurementKind("destructive"), Value: v, WellFormed: true,
		}}
	default:
		// Furnace probe and pressure sensor readings are handled by the samples
		// endpoint; a bare OK with no measurement is not a qualified reading.
		return Result{Outcome: OutcomeRejected}
	}
}
