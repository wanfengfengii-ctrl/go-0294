package store

import (
	"testing"

	"curtainwall.example/assembly-gate/internal/domain"
	"curtainwall.example/assembly-gate/internal/instrument"
)

func prepareForStress(t *testing.T, m *Memory, task *Task) {
	t.Helper()
	advance(t, m, task.ID, OperationRequest{OperationID: "edge", RuleDigest: task.Snapshot.RuleDigest, Generation: task.Generation, LogicalTime: 1, Operator: "op", Stage: "edge_confirm"})
	advance(t, m, task.ID, OperationRequest{OperationID: "temper", RuleDigest: task.Snapshot.RuleDigest, Generation: task.Generation, LogicalTime: 2, Operator: "op", Stage: "temper"})
}

func TestInstrumentFailureThenRetrySuccess(t *testing.T) {
	adapter := instrument.NewScriptedAdapter(
		instrument.Result{Outcome: instrument.OutcomeRejected},
		instrument.Result{Outcome: instrument.OutcomeTimeout},
		instrument.Result{Outcome: instrument.OutcomeMalformed},
		instrument.Result{Outcome: instrument.OutcomeOK, Reading: &domain.Measurement{
			Kind: domain.MeasureSurfaceStress, Value: 5000, WellFormed: true,
		}},
	)
	m := NewMemoryWithAdapter(adapter)
	task := lock(t, m)
	prepareForStress(t, m, task)

	req := InstrumentRequest{Device: instrument.DeviceStressMeter, Payload: "x", RuleDigest: task.Snapshot.RuleDigest, Generation: task.Generation, LogicalTime: 1, Operator: "op"}
	res, err := m.SubmitInstrumentCall(task.ID, req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Call.Status != "pending" || res.Call.Attempt != 0 {
		t.Fatalf("expected pending attempt 0, got %+v", res.Call)
	}
	// The stress stage must not have advanced after a rejected call; only the
	// two prerequisite stages are complete.
	got, _ := m.GetTask(task.ID)
	if len(got.Completed) != 2 {
		t.Fatalf("stage advanced after failure: %v", got.Completed)
	}

	// Two more failures then success via retries.
	for i := 0; i < 2; i++ {
		res, err = m.RunRetry(res.Call.ID)
		if err != nil {
			t.Fatal(err)
		}
		if res.Call.Status != "pending" {
			t.Fatalf("retry %d should stay pending, got %s", i, res.Call.Status)
		}
	}
	res, err = m.RunRetry(res.Call.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Call.Status != "done" {
		t.Fatalf("expected done, got %s", res.Call.Status)
	}
	got, _ = m.GetTask(task.ID)
	if len(got.Completed) != 3 || got.Completed[2] != "stress" {
		t.Fatalf("stress stage not advanced: %v", got.Completed)
	}
}

func TestInstrumentExhaustsAfterRetryCeiling(t *testing.T) {
	adapter := instrument.NewScriptedAdapter(
		instrument.Result{Outcome: instrument.OutcomeRejected},
		instrument.Result{Outcome: instrument.OutcomeTimeout},
		instrument.Result{Outcome: instrument.OutcomeMalformed},
		instrument.Result{Outcome: instrument.OutcomeRejected},
	)
	m := NewMemoryWithAdapter(adapter)
	task := lock(t, m)
	prepareForStress(t, m, task)

	res, err := m.SubmitInstrumentCall(task.ID, InstrumentRequest{Device: instrument.DeviceStressMeter, Payload: "x", RuleDigest: task.Snapshot.RuleDigest, Generation: task.Generation, LogicalTime: 1, Operator: "op"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < instrument.RetryCeiling()+1; i++ {
		res, err = m.RunRetry(res.Call.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if res.Call.Status != "exhausted" {
		t.Fatalf("expected exhausted, got %s", res.Call.Status)
	}
}
