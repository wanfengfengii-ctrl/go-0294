// Package instrument isolates scripted measurement devices from state
// progression. A rejected, disconnected, timed-out or malformed call only
// produces a deterministic pending-retry record with a fixed logical-time
// backoff; it never advances a manufacturing stage.
package instrument

import (
	"curtainwall.example/assembly-gate/internal/domain"
)

// Device names the scripted measurement device family.
type Device string

const (
	DeviceStressMeter Device = "stress_meter"
	DeviceFurnace     Device = "furnace_probe"
	DevicePressure    Device = "pressure_sensor"
	DeviceOptical     Device = "optical_scanner"
	DeviceDestructive Device = "destructive_rig"
)

// Outcome is the classified result of a single scripted call.
type Outcome string

const (
	OutcomeOK         Outcome = "ok"
	OutcomeRejected   Outcome = "rejected"
	OutcomeDisconnect Outcome = "disconnected"
	OutcomeTimeout    Outcome = "timeout"
	OutcomeMalformed  Outcome = "malformed"
)

// Result is the raw, scripted outcome of one device call. When OK, Reading
// carries the parsed, well-formed measurement; otherwise the failure kind is
// recorded and no qualified reading is produced.
type Result struct {
	Outcome Outcome             `json:"outcome"`
	Reading *domain.Measurement `json:"reading,omitempty"`
}

// Adapter runs a scripted device call. It is the seam that keeps device
// behaviour (which may reject, hang or return garbage) out of the state
// machine.
type Adapter interface {
	Run(device Device, payload string) Result
}

// ScriptedAdapter returns predetermined results in sequence, then repeats the
// final result. It makes instrument behaviour fully deterministic for tests
// and recovery verification.
type ScriptedAdapter struct {
	results []Result
	next    int
}

// NewScriptedAdapter returns an adapter replaying the given results in order.
func NewScriptedAdapter(results ...Result) *ScriptedAdapter {
	return &ScriptedAdapter{results: results}
}

// Run returns the next scripted result, clamping to the final entry so the
// sequence is repeatable.
func (a *ScriptedAdapter) Run(_ Device, _ string) Result {
	if len(a.results) == 0 {
		return Result{Outcome: OutcomeMalformed}
	}
	r := a.results[a.next]
	if a.next < len(a.results)-1 {
		a.next++
	}
	return r
}

// Call is a persisted instrument invocation with its logical retry state.
type Call struct {
	ID          string              `json:"id"`
	TaskID      string              `json:"task_id"`
	Device      Device              `json:"device"`
	Payload     string              `json:"payload"`
	Attempt     int                 `json:"attempt"`
	LogicalTime int64               `json:"logical_time"`
	NextTime    int64               `json:"next_time"`
	Status      string              `json:"status"` // pending | done | exhausted
	Reading     *domain.Measurement `json:"reading,omitempty"`
}

// IsPending reports whether the call is still awaiting a successful result.
func (c *Call) IsPending() bool {
	return c.Status == "pending"
}
