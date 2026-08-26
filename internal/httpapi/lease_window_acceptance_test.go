package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"reflect"
	"testing"

	"curtainwall.example/assembly-gate/internal/domain"
	"curtainwall.example/assembly-gate/internal/instrument"
	"curtainwall.example/assembly-gate/internal/store"
)

func TestModel_ResourceLeaseWindowGuardsOperationEvidence(t *testing.T) {
	tests := []struct {
		name        string
		stage       string
		resource    string
		logicalTime int64
		leaseStart  int64
		leaseEnd    int64
		wantStatus  int
		wantCode    domain.ErrorCode
	}{
		{name: "expired heat-soak rack lease", stage: "heat_soak", resource: "rack-target", logicalTime: 50, leaseStart: 1, leaseEnd: 2, wantStatus: http.StatusUnprocessableEntity, wantCode: domain.CodeLeaseExpired},
		{name: "expired lamination table lease", stage: "lamination", resource: "table-target", logicalTime: 50, leaseStart: 1, leaseEnd: 2, wantStatus: http.StatusUnprocessableEntity, wantCode: domain.CodeLeaseExpired},
		{name: "expired autoclave lease", stage: "autoclave", resource: "autoclave-target", logicalTime: 50, leaseStart: 1, leaseEnd: 2, wantStatus: http.StatusUnprocessableEntity, wantCode: domain.CodeLeaseExpired},
		{name: "covered heat-soak rack lease", stage: "heat_soak", resource: "rack-target", logicalTime: 3, leaseStart: 3, leaseEnd: 4, wantStatus: http.StatusOK},
		{name: "covered non-overlapping lamination lease", stage: "lamination", resource: "table-target", logicalTime: 4, leaseStart: 4, leaseEnd: 5, wantStatus: http.StatusOK},
		{name: "covered non-overlapping autoclave lease", stage: "autoclave", resource: "autoclave-target", logicalTime: 6, leaseStart: 6, leaseEnd: 7, wantStatus: http.StatusOK},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db, err := store.NewSQLite(filepath.Join(t.TempDir(), "gate.db"), instrument.NewPayloadAdapter())
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			defer db.Close()
			srv := New(db, t.TempDir())

			task, err := db.LockDesign(validSnapshot())
			if err != nil {
				t.Fatalf("lock design: %v", err)
			}
			base := store.OperationRequest{
				RuleDigest: task.Snapshot.RuleDigest,
				Generation: task.Generation,
				Operator:   "acceptance-test",
			}
			advance := func(stage string, logicalTime int64, resource string, leaseStart, leaseEnd int64, filmEntry *store.FilmEntryRequest) {
				t.Helper()
				req := base
				req.OperationID = "setup-" + stage
				req.Stage = stage
				req.LogicalTime = logicalTime
				req.ResourceKey = resource
				req.LeaseStart = leaseStart
				req.LeaseEnd = leaseEnd
				req.FilmEntry = filmEntry
				if _, err := db.Advance(task.ID, req); err != nil {
					t.Fatalf("setup %s: %v", stage, err)
				}
			}

			advance("edge_confirm", 1, "", 0, 0, nil)
			advance("temper", 2, "", 0, 0, nil)
			if _, err := db.SubmitInstrumentCall(task.ID, store.InstrumentRequest{
				Device: instrument.DeviceStressMeter, Payload: `{"force":5000,"area":1000}`,
				RuleDigest: task.Snapshot.RuleDigest, Generation: task.Generation,
				LogicalTime: 2, Operator: "acceptance-test",
			}); err != nil {
				t.Fatalf("setup stress stage: %v", err)
			}
			if tc.stage == "lamination" || tc.stage == "autoclave" {
				advance("heat_soak", 3, "rack-setup", 3, 4, nil)
			}
			if tc.stage == "autoclave" {
				advance("lamination", 4, "table-setup", 4, 5, &store.FilmEntryRequest{Kind: "issue", AmountUM2: 300000})
				advance("pre_press", 5, "", 0, 0, &store.FilmEntryRequest{Kind: "cut", AmountUM2: 300000})
			}

			beforeTask, err := db.GetTask(task.ID)
			if err != nil {
				t.Fatalf("read task before operation: %v", err)
			}
			beforeLineage, err := db.GetLineage(task.ID)
			if err != nil {
				t.Fatalf("read lineage before operation: %v", err)
			}
			beforeLineageJSON, err := json.Marshal(beforeLineage)
			if err != nil {
				t.Fatalf("snapshot lineage: %v", err)
			}
			beforeFilm, err := db.GetFilmLedger(task.Snapshot.FilmBatch)
			if err != nil {
				t.Fatalf("read film ledger before operation: %v", err)
			}

			req := base
			req.OperationID = "target-" + tc.stage
			req.Stage = tc.stage
			req.LogicalTime = tc.logicalTime
			req.ResourceKey = tc.resource
			req.LeaseStart = tc.leaseStart
			req.LeaseEnd = tc.leaseEnd
			if tc.stage == "lamination" {
				req.FilmEntry = &store.FilmEntryRequest{Kind: "issue", AmountUM2: 100000}
			}
			raw, err := json.Marshal(req)
			if err != nil {
				t.Fatalf("marshal operation: %v", err)
			}
			w := doJSON(t, srv, http.MethodPost, fmt.Sprintf("/api/tasks/%s/operations", task.ID), string(raw))
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, tc.wantStatus, w.Body.String())
			}

			afterTask, err := db.GetTask(task.ID)
			if err != nil {
				t.Fatalf("read task after operation: %v", err)
			}
			if tc.wantCode == "" {
				found := false
				for _, completed := range afterTask.Completed {
					if completed == tc.stage {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("successful stage %q not completed: %v", tc.stage, afterTask.Completed)
				}
				return
			}

			var gotErr domain.Error
			if err := json.Unmarshal(w.Body.Bytes(), &gotErr); err != nil {
				t.Fatalf("decode lease error: %v", err)
			}
			if gotErr.Code != tc.wantCode {
				t.Fatalf("error code = %q, want %q", gotErr.Code, tc.wantCode)
			}
			if !reflect.DeepEqual(afterTask.Completed, beforeTask.Completed) {
				t.Fatalf("rejected operation advanced stages: before=%v after=%v", beforeTask.Completed, afterTask.Completed)
			}
			afterLineage, err := db.GetLineage(task.ID)
			if err != nil {
				t.Fatalf("read lineage after rejection: %v", err)
			}
			afterLineageJSON, err := json.Marshal(afterLineage)
			if err != nil {
				t.Fatalf("snapshot lineage after rejection: %v", err)
			}
			if string(afterLineageJSON) != string(beforeLineageJSON) {
				t.Fatalf("rejected operation changed lineage: before=%s after=%s", beforeLineageJSON, afterLineageJSON)
			}
			afterFilm, err := db.GetFilmLedger(task.Snapshot.FilmBatch)
			if err != nil {
				t.Fatalf("read film ledger after rejection: %v", err)
			}
			if !reflect.DeepEqual(afterFilm, beforeFilm) {
				t.Fatalf("rejected operation changed film ledger: before=%+v after=%+v", beforeFilm, afterFilm)
			}
		})
	}
}
