package httpapi

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"reflect"
	"testing"

	"curtainwall.example/assembly-gate/internal/domain"
	"curtainwall.example/assembly-gate/internal/film"
	"curtainwall.example/assembly-gate/internal/instrument"
	"curtainwall.example/assembly-gate/internal/store"
)

func TestModel_AtomicFilmLedgerRollback(t *testing.T) {
	tests := []struct {
		name string
		open func(*testing.T) store.Store
	}{
		{
			name: "memory",
			open: func(*testing.T) store.Store { return store.NewMemory() },
		},
		{
			name: "sqlite",
			open: func(t *testing.T) store.Store {
				db, err := store.NewSQLite(filepath.Join(t.TempDir(), "gate.db"), instrument.NewPayloadAdapter())
				if err != nil {
					t.Fatalf("open sqlite store: %v", err)
				}
				return db
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			backend := tc.open(t)
			t.Cleanup(func() { _ = backend.Close() })
			srv := New(backend, t.TempDir())

			snapshot := validSnapshot()
			raw, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			w := doJSON(t, srv, http.MethodPost, "/api/designs/lock", string(raw))
			if w.Code != http.StatusCreated {
				t.Fatalf("lock design: status=%d body=%s", w.Code, w.Body.String())
			}
			var task store.Task
			if err := json.Unmarshal(w.Body.Bytes(), &task); err != nil {
				t.Fatal(err)
			}

			post := func(path string, body any, want int) []byte {
				t.Helper()
				raw, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				w := doJSON(t, srv, http.MethodPost, path, string(raw))
				if w.Code != want {
					t.Fatalf("POST %s: status=%d want=%d body=%s", path, w.Code, want, w.Body.String())
				}
				return w.Body.Bytes()
			}
			operationPath := "/api/tasks/" + task.ID + "/operations"
			base := store.OperationRequest{
				RuleDigest: task.Snapshot.RuleDigest,
				Generation: task.Generation,
				Operator:   "operator-1",
			}
			for i, stage := range []string{"edge_confirm", "temper"} {
				req := base
				req.OperationID = "setup-" + stage
				req.LogicalTime = int64(i + 1)
				req.Stage = stage
				post(operationPath, req, http.StatusOK)
			}
			post("/api/tasks/"+task.ID+"/instrument-calls", store.InstrumentRequest{
				Device: instrument.DeviceStressMeter, Payload: `{"force":5000,"area":1000}`,
				RuleDigest: task.Snapshot.RuleDigest, Generation: task.Generation,
				LogicalTime: 2, Operator: "operator-1",
			}, http.StatusOK)
			heat := base
			heat.OperationID = "setup-heat"
			heat.LogicalTime = 3
			heat.Stage = "heat_soak"
			heat.ResourceKey = "occupied-table"
			heat.LeaseStart = 3
			heat.LeaseEnd = 100
			post(operationPath, heat, http.StatusOK)

			readTask := func() store.Task {
				t.Helper()
				w := doJSON(t, srv, http.MethodGet, "/api/tasks/"+task.ID, "")
				if w.Code != http.StatusOK {
					t.Fatalf("get task: status=%d body=%s", w.Code, w.Body.String())
				}
				var got store.Task
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				return got
			}
			readLedger := func() film.Ledger {
				t.Helper()
				w := doJSON(t, srv, http.MethodGet, "/api/film-ledger?batch="+snapshot.FilmBatch, "")
				if w.Code != http.StatusOK {
					t.Fatalf("get film ledger: status=%d body=%s", w.Code, w.Body.String())
				}
				var got film.Ledger
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				return got
			}

			beforeTask := readTask()
			beforeLedger := readLedger()
			conflict := base
			conflict.OperationID = "lamination-conflict"
			conflict.LogicalTime = 4
			conflict.Stage = "lamination"
			conflict.ResourceKey = "occupied-table"
			conflict.LeaseStart = 4
			conflict.LeaseEnd = 90
			conflict.FilmEntry = &store.FilmEntryRequest{Kind: "issue", AmountUM2: 300000}
			body := post(operationPath, conflict, http.StatusConflict)
			var rejection domain.Error
			if err := json.Unmarshal(body, &rejection); err != nil {
				t.Fatal(err)
			}
			if rejection.Code != domain.CodeLeaseConflict {
				t.Fatalf("error code=%q want=%q", rejection.Code, domain.CodeLeaseConflict)
			}

			afterFailureTask := readTask()
			afterFailureLedger := readLedger()
			if !reflect.DeepEqual(afterFailureLedger, beforeLedger) {
				t.Fatalf("failed operation changed film ledger: before=%+v after=%+v", beforeLedger, afterFailureLedger)
			}
			if !reflect.DeepEqual(afterFailureTask.Completed, beforeTask.Completed) ||
				!reflect.DeepEqual(afterFailureTask.Lineage, beforeTask.Lineage) {
				t.Fatalf("failed operation changed task prefix or lineage: before=%+v after=%+v", beforeTask, afterFailureTask)
			}

			retry := conflict
			retry.OperationID = "lamination-valid"
			retry.ResourceKey = "available-table"
			post(operationPath, retry, http.StatusOK)
			afterSuccessTask := readTask()
			afterSuccessLedger := readLedger()
			if afterSuccessLedger.Opening != snapshot.FilmOpeningUM2 || !afterSuccessLedger.Balanced() ||
				afterSuccessLedger.Account.Available != snapshot.FilmOpeningUM2-300000 ||
				afterSuccessLedger.Account.InProgress != 300000 {
				t.Fatalf("valid retry did not commit exactly one conserved issue: %+v", afterSuccessLedger)
			}
			wantCompleted := append(append([]string(nil), beforeTask.Completed...), "lamination")
			if !reflect.DeepEqual(afterSuccessTask.Completed, wantCompleted) ||
				len(afterSuccessTask.Lineage.Nodes) != len(beforeTask.Lineage.Nodes)+1 ||
				len(afterSuccessTask.Lineage.Edges) != len(beforeTask.Lineage.Edges)+1 {
				t.Fatalf("valid retry did not advance prefix and lineage once: before=%+v after=%+v", beforeTask, afterSuccessTask)
			}
		})
	}
}
