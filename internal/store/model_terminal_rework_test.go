package store_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"curtainwall.example/assembly-gate/internal/arbiter"
	"curtainwall.example/assembly-gate/internal/domain"
	"curtainwall.example/assembly-gate/internal/evidence"
	"curtainwall.example/assembly-gate/internal/instrument"
	"curtainwall.example/assembly-gate/internal/store"
)

func TestModel_TerminalVerdictRejectsReworkAcrossRestart(t *testing.T) {
	snapshot := domain.DesignSnapshot{
		Project: "Terminal-Gate", FacadeZone: "F1", PlateNumber: "P-001",
		Version: 1, ThicknessUM: 12000, WidthUM: 100010, HeightUM: 200010,
		EdgeMarginUM: 5, EdgeScheme: "flat-polish",
		Geometry: domain.Polygon{Outline: domain.Ring{
			{X: 5, Y: 5}, {X: 100005, Y: 5}, {X: 100005, Y: 200005}, {X: 5, Y: 200005},
		}},
		FurnaceLot: "LOT-7", FilmBatch: "FILM-9", FilmOpeningUM2: 1000000,
		Thresholds: map[string]int64{"surface_stress": 1000, "bow": 1000000, "bubble_rate": 1000},
		Rack: domain.RackPlan{
			FurnaceRun: "RUN-1",
			Positions:  []domain.RackPosition{{ID: "R1", Level: 1}},
		},
		Inspection: domain.InspectionPlan{
			Grid: []string{"G1"}, Sampling: map[string]string{"G1": "P-001"}, Destructive: 1,
		},
	}

	advance := func(t *testing.T, s store.Store, task *store.Task, operationID, stage string, logicalTime int64, resource string, filmEntry *store.FilmEntryRequest) {
		t.Helper()
		_, err := s.Advance(task.ID, store.OperationRequest{
			OperationID: operationID, RuleDigest: task.Snapshot.RuleDigest,
			Generation: task.Generation, LogicalTime: logicalTime, Operator: "operator",
			Stage: stage, ResourceKey: resource, LeaseStart: logicalTime, LeaseEnd: 100,
			FilmEntry: filmEntry,
		})
		if err != nil {
			t.Fatalf("advance %s: %v", stage, err)
		}
	}
	runToClosure := func(t *testing.T, s store.Store, task *store.Task) {
		t.Helper()
		advance(t, s, task, "edge", "edge_confirm", 1, "", nil)
		advance(t, s, task, "temper", "temper", 2, "", nil)
		if _, err := s.SubmitInstrumentCall(task.ID, store.InstrumentRequest{
			Device: instrument.DeviceStressMeter, Payload: `{"force":5000,"area":1000}`,
			RuleDigest: task.Snapshot.RuleDigest, Generation: task.Generation, LogicalTime: 2, Operator: "operator",
		}); err != nil {
			t.Fatalf("stress measurement: %v", err)
		}
		advance(t, s, task, "heat", "heat_soak", 3, "rack-1", nil)
		if _, err := s.SubmitSamples(task.ID, store.SampleRequest{
			Stage: "heat_soak", RuleDigest: task.Snapshot.RuleDigest, Generation: task.Generation,
			Samples: []evidence.SamplePoint{
				{LogicalTime: 1, Value: 100, RackPosition: "R1", Segment: "ramp_up"},
				{LogicalTime: 2, Value: 200, RackPosition: "R1", Segment: "hold"},
				{LogicalTime: 3, Value: 150, RackPosition: "R1", Segment: "ramp_down"},
			},
		}); err != nil {
			t.Fatalf("heat-soak samples: %v", err)
		}
		advance(t, s, task, "laminate", "lamination", 4, "table-1", &store.FilmEntryRequest{Kind: "issue", AmountUM2: 300000})
		advance(t, s, task, "prepress", "pre_press", 5, "", &store.FilmEntryRequest{Kind: "cut", AmountUM2: 300000})
		advance(t, s, task, "autoclave", "autoclave", 6, "autoclave-1", nil)
		if _, err := s.SubmitSamples(task.ID, store.SampleRequest{
			Stage: "autoclave", RuleDigest: task.Snapshot.RuleDigest, Generation: task.Generation,
			Samples: []evidence.SamplePoint{
				{LogicalTime: 0, Value: 100, Segment: "preheat"},
				{LogicalTime: 10, Value: 200, Segment: "pressurize"},
				{LogicalTime: 20, Value: 200, Segment: "hold"},
				{LogicalTime: 30, Value: 100, Segment: "depressurize"},
				{LogicalTime: 40, Value: 50, Segment: "cool"},
			},
		}); err != nil {
			t.Fatalf("autoclave samples: %v", err)
		}
		for _, call := range []store.InstrumentRequest{
			{Device: instrument.DeviceOptical, Payload: `{"deviation":3,"span":1000}`},
			{Device: instrument.DeviceDestructive, Payload: `{"passed":true}`},
		} {
			call.RuleDigest = task.Snapshot.RuleDigest
			call.Generation = task.Generation
			call.LogicalTime = 7
			call.Operator = "operator"
			if _, err := s.SubmitInstrumentCall(task.ID, call); err != nil {
				t.Fatalf("instrument %s: %v", call.Device, err)
			}
		}
		for _, reviewer := range []string{"alice", "bob"} {
			if err := s.AddReview(task.ID, store.ReviewRequest{Reviewer: reviewer, Qualified: true, Generation: task.Generation}); err != nil {
				t.Fatalf("review %s: %v", reviewer, err)
			}
		}
	}

	cases := []struct {
		name    string
		verdict string
	}{
		{name: "anomaly before a verdict still starts rework"},
		{name: "admit remains terminal", verdict: "admit"},
		{name: "isolate remains terminal", verdict: "isolate"},
		{name: "cancel remains terminal", verdict: "cancel"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "gate.db")
			s, err := store.NewSQLite(path, instrument.NewPayloadAdapter())
			if err != nil {
				t.Fatal(err)
			}
			task, err := s.LockDesign(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			initialGeneration := task.Generation

			if tc.verdict == "" {
				advance(t, s, task, "before-final", "edge_confirm", 1, "", nil)
				if _, err := s.RegisterAnomaly(task.ID, store.AnomalyRequest{
					Kind: arbiter.AnomalyBurst, RackPos: "R1", Generation: task.Generation,
					RuleDigest: task.Snapshot.RuleDigest,
				}); err != nil {
					t.Fatalf("pre-terminal anomaly was rejected: %v", err)
				}
				got, err := s.GetTask(task.ID)
				if err != nil {
					t.Fatal(err)
				}
				set, err := s.GetRetests(task.ID)
				if err != nil {
					t.Fatal(err)
				}
				if got.Generation != initialGeneration+1 || len(got.Completed) != 0 || set == nil || len(set.Members) == 0 {
					t.Fatalf("pre-terminal rework state = generation %d, completed %v, retest %+v", got.Generation, got.Completed, set)
				}
				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
				return
			}

			runToClosure(t, s, task)
			result, err := s.SubmitVerdict(task.ID, store.VerdictRequest{Verdict: tc.verdict, Generation: task.Generation})
			if err != nil {
				t.Fatalf("%s verdict: %v", tc.verdict, err)
			}
			before, err := s.GetTask(task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}

			s, err = store.NewSQLite(path, instrument.NewPayloadAdapter())
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			recovered, err := s.GetTask(task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(recovered, before) {
				t.Fatalf("task changed during restart: before=%+v after=%+v", before, recovered)
			}

			_, err = s.RegisterAnomaly(task.ID, store.AnomalyRequest{
				Kind: arbiter.AnomalyBurst, RackPos: "R1", Generation: initialGeneration,
				RuleDigest: task.Snapshot.RuleDigest,
			})
			var domainErr *domain.Error
			if !errors.As(err, &domainErr) || domainErr.Code != domain.CodeFinalExists {
				t.Fatalf("late anomaly error = %v, want %s", err, domain.CodeFinalExists)
			}
			after, err := s.GetTask(task.ID)
			if err != nil {
				t.Fatal(err)
			}
			set, err := s.GetRetests(task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after, before) || set != nil {
				t.Fatalf("late anomaly mutated terminal state: before=%+v after=%+v retest=%+v", before, after, set)
			}

			if tc.verdict == "admit" {
				sum := sha256.Sum256([]byte(task.ID))
				credentialID := hex.EncodeToString(sum[:8])
				credential, err := s.GetCredential(credentialID)
				if err != nil {
					t.Fatalf("credential after restart and rejected anomaly: %v", err)
				}
				if credential.Generation != initialGeneration || credential.Value != result.Credential {
					t.Fatalf("credential mutated: got %+v, original value %q", credential, result.Credential)
				}
			}
		})
	}
}
