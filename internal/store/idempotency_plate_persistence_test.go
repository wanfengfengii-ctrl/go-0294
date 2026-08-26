package store

import (
	"path/filepath"
	"reflect"
	"testing"

	"curtainwall.example/assembly-gate/internal/domain"
	"curtainwall.example/assembly-gate/internal/instrument"
)

func TestModel_OperationIDRemainsBoundToPlateAcrossRestart(t *testing.T) {
	tests := []struct {
		name    string
		restart bool
	}{
		{name: "memory"},
		{name: "sqlite_restart", restart: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				m    Store
				path string
			)
			if tt.restart {
				path = filepath.Join(t.TempDir(), "gate.db")
				s, err := NewSQLite(path, instrument.NewPayloadAdapter())
				if err != nil {
					t.Fatalf("open sqlite store: %v", err)
				}
				m = s
			} else {
				m = NewMemory()
			}

			firstSnapshot := validSnapshot()
			first, err := m.LockDesign(firstSnapshot)
			if err != nil {
				t.Fatalf("lock first plate: %v", err)
			}
			secondSnapshot := validSnapshot()
			secondSnapshot.PlateNumber = "P-002"
			secondSnapshot.FurnaceLot = "LOT-8"
			secondSnapshot.FilmBatch = "FILM-10"
			second, err := m.LockDesign(secondSnapshot)
			if err != nil {
				t.Fatalf("lock second plate: %v", err)
			}

			const operationID = "shared-edge-confirm"
			firstRequest := OperationRequest{
				OperationID: operationID,
				RuleDigest:  first.Snapshot.RuleDigest,
				Generation:  first.Generation,
				LogicalTime: 11,
				Operator:    "edge-inspector",
				Stage:       "edge_confirm",
			}
			if _, err := m.Advance(first.ID, firstRequest); err != nil {
				t.Fatalf("advance first plate: %v", err)
			}

			if tt.restart {
				if err := m.Close(); err != nil {
					t.Fatalf("close sqlite store: %v", err)
				}
				s, err := NewSQLite(path, instrument.NewPayloadAdapter())
				if err != nil {
					t.Fatalf("reopen sqlite store: %v", err)
				}
				m = s
			}
			defer m.Close()

			replayed, err := m.Advance(first.ID, firstRequest)
			if err != nil {
				t.Fatalf("exact replay should return the committed result: %v", err)
			}
			if !reflect.DeepEqual(replayed.Completed, []string{"edge_confirm"}) {
				t.Fatalf("exact replay completed stages = %v", replayed.Completed)
			}

			secondRequest := firstRequest
			secondRequest.RuleDigest = second.Snapshot.RuleDigest
			_, err = m.Advance(second.ID, secondRequest)
			if err == nil {
				t.Fatal("operation id reused for another plate unexpectedly succeeded")
			}
			domainErr, ok := err.(*domain.Error)
			if !ok || domainErr.Code != domain.CodeIdempotencyConflict {
				t.Fatalf("cross-plate reuse error = %v, want %s", err, domain.CodeIdempotencyConflict)
			}

			firstAfter, err := m.GetTask(first.ID)
			if err != nil {
				t.Fatalf("get first plate: %v", err)
			}
			secondAfter, err := m.GetTask(second.ID)
			if err != nil {
				t.Fatalf("get second plate: %v", err)
			}
			if !reflect.DeepEqual(firstAfter.Completed, []string{"edge_confirm"}) {
				t.Fatalf("first plate prefix changed after conflict: %v", firstAfter.Completed)
			}
			if len(secondAfter.Completed) != 0 {
				t.Fatalf("second plate advanced after conflict: %v", secondAfter.Completed)
			}
		})
	}
}
