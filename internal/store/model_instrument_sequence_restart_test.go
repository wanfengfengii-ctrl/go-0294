package store

import (
	"path/filepath"
	"testing"

	"curtainwall.example/assembly-gate/internal/instrument"
)

func TestModel_SQLiteRestartPreservesInstrumentCallSequence(t *testing.T) {
	tests := []struct {
		name         string
		firstDevice  instrument.Device
		secondDevice instrument.Device
	}{
		{
			name:         "different devices at the same logical time",
			firstDevice:  instrument.DeviceStressMeter,
			secondDevice: instrument.DeviceOptical,
		},
		{
			name:         "repeated device at the same logical time",
			firstDevice:  instrument.DeviceStressMeter,
			secondDevice: instrument.DeviceStressMeter,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "gate.db")
			firstPayload := `{"outcome":"timeout","request":"before-restart"}`
			secondPayload := `{"outcome":"timeout","request":"after-restart"}`

			before, err := NewSQLite(path, instrument.NewPayloadAdapter())
			if err != nil {
				t.Fatalf("open store before restart: %v", err)
			}
			task, err := before.LockDesign(validSnapshot())
			if err != nil {
				t.Fatalf("lock design: %v", err)
			}
			first, err := before.SubmitInstrumentCall(task.ID, InstrumentRequest{
				Device: tc.firstDevice, Payload: firstPayload,
				RuleDigest: task.Snapshot.RuleDigest, Generation: task.Generation,
				LogicalTime: 20, Operator: "operator-before-restart",
			})
			if err != nil {
				t.Fatalf("submit call before restart: %v", err)
			}
			if err := before.Close(); err != nil {
				t.Fatalf("close store before restart: %v", err)
			}

			after, err := NewSQLite(path, instrument.NewPayloadAdapter())
			if err != nil {
				t.Fatalf("reopen store: %v", err)
			}
			defer after.Close()

			second, err := after.SubmitInstrumentCall(task.ID, InstrumentRequest{
				Device: tc.secondDevice, Payload: secondPayload,
				RuleDigest: task.Snapshot.RuleDigest, Generation: task.Generation,
				LogicalTime: 20, Operator: "operator-after-restart",
			})
			if err != nil {
				t.Fatalf("submit call after restart: %v", err)
			}
			if first.Call.ID == second.Call.ID {
				t.Fatalf("independent calls reused ID %q", first.Call.ID)
			}

			pending := after.PendingRetries()
			if len(pending) != 2 {
				t.Fatalf("pending calls = %d, want 2", len(pending))
			}
			byID := make(map[string]*instrument.Call, len(pending))
			for _, call := range pending {
				byID[call.ID] = call
			}
			want := []struct {
				id      string
				device  instrument.Device
				payload string
			}{
				{id: first.Call.ID, device: tc.firstDevice, payload: firstPayload},
				{id: second.Call.ID, device: tc.secondDevice, payload: secondPayload},
			}
			for _, expected := range want {
				call, ok := byID[expected.id]
				if !ok {
					t.Errorf("pending call %q was not preserved", expected.id)
					continue
				}
				if call.TaskID != task.ID || call.Device != expected.device || call.Payload != expected.payload ||
					call.Attempt != 0 || call.LogicalTime != 20 || call.NextTime != 30 || call.Status != "pending" {
					t.Errorf("call %q changed across restart: %+v", expected.id, call)
				}
			}
		})
	}
}
