package httpapi

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"curtainwall.example/assembly-gate/internal/arbiter"
	"curtainwall.example/assembly-gate/internal/domain"
	"curtainwall.example/assembly-gate/internal/instrument"
	"curtainwall.example/assembly-gate/internal/store"
)

func TestModel_StaleRetryCannotAdvanceReworkedGeneration(t *testing.T) {
	cases := []struct {
		name          string
		newStages     []string
		failedOutcome instrument.Outcome
		wantCompleted []string
	}{
		{
			name:          "cannot append a reading before the new stress stage is ready",
			newStages:     []string{"edge_confirm"},
			failedOutcome: instrument.OutcomeTimeout,
			wantCompleted: []string{"edge_confirm"},
		},
		{
			name:          "cannot complete stress after the new generation reaches temper",
			newStages:     []string{"edge_confirm", "temper"},
			failedOutcome: instrument.OutcomeTimeout,
			wantCompleted: []string{"edge_confirm", "temper"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adapter := instrument.NewScriptedAdapter(
				instrument.Result{Outcome: tc.failedOutcome},
				instrument.Result{Outcome: instrument.OutcomeOK, Reading: &domain.Measurement{
					Kind: domain.MeasureSurfaceStress, Value: 5000, WellFormed: true,
				}},
			)
			mem := store.NewMemoryWithAdapter(adapter)
			srv := New(mem, t.TempDir())

			snapshotJSON, err := json.Marshal(validSnapshot())
			if err != nil {
				t.Fatal(err)
			}
			w := doJSON(t, srv, http.MethodPost, "/api/designs/lock", string(snapshotJSON))
			if w.Code != http.StatusCreated {
				t.Fatalf("lock status = %d, body = %s", w.Code, w.Body.String())
			}
			var task store.Task
			if err := json.Unmarshal(w.Body.Bytes(), &task); err != nil {
				t.Fatal(err)
			}

			post := func(path string, body any, wantStatus int) []byte {
				t.Helper()
				raw, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				w := doJSON(t, srv, http.MethodPost, path, string(raw))
				if w.Code != wantStatus {
					t.Fatalf("POST %s status = %d, want %d, body = %s", path, w.Code, wantStatus, w.Body.String())
				}
				return w.Body.Bytes()
			}

			oldGeneration := task.Generation
			for i, stage := range []string{"edge_confirm", "temper"} {
				post("/api/tasks/"+task.ID+"/operations", store.OperationRequest{
					OperationID: "old-" + stage,
					RuleDigest:  task.Snapshot.RuleDigest,
					Generation:  oldGeneration,
					LogicalTime: int64(i + 1),
					Operator:    "acceptance",
					Stage:       stage,
				}, http.StatusOK)
			}

			callBody := post("/api/tasks/"+task.ID+"/instrument-calls", store.InstrumentRequest{
				Device:      instrument.DeviceStressMeter,
				Payload:     "old-generation-stress",
				RuleDigest:  task.Snapshot.RuleDigest,
				Generation:  oldGeneration,
				LogicalTime: 3,
				Operator:    "acceptance",
			}, http.StatusOK)
			var failed store.InstrumentResult
			if err := json.Unmarshal(callBody, &failed); err != nil {
				t.Fatal(err)
			}
			if failed.Call == nil || failed.Call.ID == "" || failed.Call.Status != "pending" {
				t.Fatalf("expected a pending old-generation retry, got %+v", failed.Call)
			}

			post("/api/tasks/"+task.ID+"/anomalies", store.AnomalyRequest{
				Kind:       arbiter.AnomalyBurst,
				RackPos:    "R1",
				Generation: oldGeneration,
				RuleDigest: task.Snapshot.RuleDigest,
			}, http.StatusOK)

			newGeneration := oldGeneration + 1
			for i, stage := range tc.newStages {
				post("/api/tasks/"+task.ID+"/operations", store.OperationRequest{
					OperationID: "new-" + stage,
					RuleDigest:  task.Snapshot.RuleDigest,
					Generation:  newGeneration,
					LogicalTime: int64(10 + i),
					Operator:    "acceptance",
					Stage:       stage,
				}, http.StatusOK)
			}

			w = doJSON(t, srv, http.MethodGet, "/api/tasks/"+task.ID, "")
			if w.Code != http.StatusOK {
				t.Fatalf("get task before retry status = %d, body = %s", w.Code, w.Body.String())
			}
			var before store.Task
			if err := json.Unmarshal(w.Body.Bytes(), &before); err != nil {
				t.Fatal(err)
			}
			if before.Generation != newGeneration || !reflect.DeepEqual(before.Completed, tc.wantCompleted) {
				t.Fatalf("unexpected new-generation state before retry: generation=%d completed=%v", before.Generation, before.Completed)
			}

			w = doJSON(t, srv, http.MethodPost, "/api/retries/"+failed.Call.ID+"/run", "")
			if w.Code != http.StatusUnprocessableEntity {
				t.Fatalf("stale retry status = %d, want %d, body = %s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
			}
			var rejection domain.Error
			if err := json.Unmarshal(w.Body.Bytes(), &rejection); err != nil {
				t.Fatal(err)
			}
			if rejection.Code != domain.CodeRetestGenerationConflict {
				t.Fatalf("stale retry code = %q, want %q", rejection.Code, domain.CodeRetestGenerationConflict)
			}

			w = doJSON(t, srv, http.MethodGet, "/api/tasks/"+task.ID, "")
			if w.Code != http.StatusOK {
				t.Fatalf("get task after retry status = %d, body = %s", w.Code, w.Body.String())
			}
			var after store.Task
			if err := json.Unmarshal(w.Body.Bytes(), &after); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after.Completed, before.Completed) {
				t.Fatalf("stale retry changed current stages: before=%v after=%v", before.Completed, after.Completed)
			}
			if !reflect.DeepEqual(after.Measurements, before.Measurements) {
				t.Fatalf("stale retry appended a current-generation measurement: before=%v after=%v", before.Measurements, after.Measurements)
			}

			w = doJSON(t, srv, http.MethodGet, "/api/retries", "")
			if w.Code != http.StatusOK {
				t.Fatalf("list retries status = %d, body = %s", w.Code, w.Body.String())
			}
			var pending []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &pending); err != nil {
				t.Fatal(err)
			}
			if len(pending) != 1 || pending[0].ID != failed.Call.ID || pending[0].Status != "pending" {
				t.Fatalf("rejected stale retry was not safely preserved: %+v", pending)
			}
		})
	}
}
