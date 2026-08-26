package store

import (
	"path/filepath"
	"sync"
	"testing"

	"curtainwall.example/assembly-gate/internal/arbiter"
	"curtainwall.example/assembly-gate/internal/domain"
	"curtainwall.example/assembly-gate/internal/evidence"
	"curtainwall.example/assembly-gate/internal/instrument"
)

func validSnapshot() domain.DesignSnapshot {
	return domain.DesignSnapshot{
		Project: "Tower-A", FacadeZone: "F1", PlateNumber: "P-001",
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
			Adjacency:  []domain.AdjacencyPair{},
		},
		Inspection: domain.InspectionPlan{Grid: []string{"G1"}, Sampling: map[string]string{"G1": "P-001"}, Destructive: 1},
	}
}

func lock(t *testing.T, m Store) *Task {
	t.Helper()
	task, err := m.LockDesign(validSnapshot())
	if err != nil {
		t.Fatalf("lock failed: %v", err)
	}
	return task
}

func advance(t *testing.T, m Store, id string, req OperationRequest) {
	t.Helper()
	if _, err := m.Advance(id, req); err != nil {
		t.Fatalf("advance %s failed: %v", req.Stage, err)
	}
}

func instrumentOK(t *testing.T, m Store, id string, gen int, digest string, dev instrument.Device, payload string) {
	t.Helper()
	res, err := m.SubmitInstrumentCall(id, InstrumentRequest{
		Device: dev, Payload: payload, RuleDigest: digest, Generation: gen, LogicalTime: 1, Operator: "op",
	})
	if err != nil {
		t.Fatalf("instrument %s failed: %v", dev, err)
	}
	if res.Call.Status != "done" {
		t.Fatalf("instrument %s not done: %+v", dev, res.Call)
	}
}

func heatSamples() []evidence.SamplePoint {
	return []evidence.SamplePoint{
		{LogicalTime: 1, Value: 100, RackPosition: "R1", Segment: "ramp_up"},
		{LogicalTime: 2, Value: 200, RackPosition: "R1", Segment: "hold"},
		{LogicalTime: 3, Value: 150, RackPosition: "R1", Segment: "ramp_down"},
	}
}

func autoclaveSamples() []evidence.SamplePoint {
	return []evidence.SamplePoint{
		{LogicalTime: 0, Value: 100, Segment: "preheat"},
		{LogicalTime: 10, Value: 200, Segment: "pressurize"},
		{LogicalTime: 20, Value: 200, Segment: "hold"},
		{LogicalTime: 30, Value: 100, Segment: "depressurize"},
		{LogicalTime: 40, Value: 50, Segment: "cool"},
	}
}

// runManufacturing drives a task through every manufacturing stage up to and
// including destructive test, leaving it ready for review and verdict.
func runManufacturing(t *testing.T, m Store, task *Task) {
	t.Helper()
	id := task.ID
	digest := task.Snapshot.RuleDigest
	gen := task.Generation

	advance(t, m, id, OperationRequest{OperationID: "op-edge", RuleDigest: digest, Generation: gen, LogicalTime: 1, Operator: "op", Stage: "edge_confirm"})
	advance(t, m, id, OperationRequest{OperationID: "op-temper", RuleDigest: digest, Generation: gen, LogicalTime: 2, Operator: "op", Stage: "temper"})
	instrumentOK(t, m, id, gen, digest, instrument.DeviceStressMeter, `{"force":5000,"area":1000}`)
	advance(t, m, id, OperationRequest{OperationID: "op-heat", RuleDigest: digest, Generation: gen, LogicalTime: 3, Operator: "op", Stage: "heat_soak", ResourceKey: "rack-1", LeaseStart: 3, LeaseEnd: 100})
	if _, err := m.SubmitSamples(id, SampleRequest{Stage: "heat_soak", RuleDigest: digest, Generation: gen, Samples: heatSamples()}); err != nil {
		t.Fatalf("heat samples failed: %v", err)
	}
	advance(t, m, id, OperationRequest{OperationID: "op-lam", RuleDigest: digest, Generation: gen, LogicalTime: 4, Operator: "op", Stage: "lamination", ResourceKey: "table-1", LeaseStart: 4, LeaseEnd: 100, FilmEntry: &FilmEntryRequest{Kind: "issue", AmountUM2: 300000}})
	advance(t, m, id, OperationRequest{OperationID: "op-prepress", RuleDigest: digest, Generation: gen, LogicalTime: 5, Operator: "op", Stage: "pre_press", FilmEntry: &FilmEntryRequest{Kind: "cut", AmountUM2: 300000}})
	advance(t, m, id, OperationRequest{OperationID: "op-auto", RuleDigest: digest, Generation: gen, LogicalTime: 6, Operator: "op", Stage: "autoclave", ResourceKey: "autoclave-1", LeaseStart: 6, LeaseEnd: 100})
	if _, err := m.SubmitSamples(id, SampleRequest{Stage: "autoclave", RuleDigest: digest, Generation: gen, Samples: autoclaveSamples()}); err != nil {
		t.Fatalf("autoclave samples failed: %v", err)
	}
	instrumentOK(t, m, id, gen, digest, instrument.DeviceOptical, `{"deviation":3,"span":1000}`)
	instrumentOK(t, m, id, gen, digest, instrument.DeviceDestructive, `{"passed":true}`)
}

func TestFullFlowAdmitsAndMintsCredential(t *testing.T) {
	m := NewMemory()
	task := lock(t, m)
	runManufacturing(t, m, task)

	if err := m.AddReview(task.ID, ReviewRequest{Reviewer: "alice", Qualified: true, Generation: task.Generation}); err != nil {
		t.Fatal(err)
	}
	if err := m.AddReview(task.ID, ReviewRequest{Reviewer: "bob", Qualified: true, Generation: task.Generation}); err != nil {
		t.Fatal(err)
	}
	res, err := m.SubmitVerdict(task.ID, VerdictRequest{Verdict: "admit", Generation: task.Generation})
	if err != nil {
		t.Fatalf("verdict failed: %v", err)
	}
	if res.Verdict != arbiter.VerdictAdmit || res.Credential == "" {
		t.Fatalf("expected admit with credential, got %+v", res)
	}
}

func TestRestartRecoveryPreservesState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gate.db")

	s1, err := NewSQLite(path, instrument.NewPayloadAdapter())
	if err != nil {
		t.Fatal(err)
	}
	task, err := s1.LockDesign(validSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s1.Advance(task.ID, OperationRequest{OperationID: "op-1", RuleDigest: task.Snapshot.RuleDigest, Generation: task.Generation, LogicalTime: 1, Operator: "op", Stage: "edge_confirm"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s1.SubmitInstrumentCall(task.ID, InstrumentRequest{Device: instrument.DeviceStressMeter, Payload: `{"outcome":"timeout"}`, RuleDigest: task.Snapshot.RuleDigest, Generation: task.Generation, LogicalTime: 2, Operator: "op"}); err != nil {
		t.Fatal(err)
	}
	s1.Close()

	s2, err := NewSQLite(path, instrument.NewPayloadAdapter())
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	got, err := s2.GetTask(task.ID)
	if err != nil {
		t.Fatalf("task not recovered: %v", err)
	}
	if len(got.Completed) == 0 {
		t.Fatal("completed stages not recovered")
	}
	if len(s2.PendingRetries()) != 1 {
		t.Fatalf("pending retries not recovered: %d", len(s2.PendingRetries()))
	}
}

func TestIdempotentAdvance(t *testing.T) {
	m := NewMemory()
	task := lock(t, m)
	req := OperationRequest{OperationID: "op-1", RuleDigest: task.Snapshot.RuleDigest, Generation: task.Generation, LogicalTime: 1, Operator: "op", Stage: "edge_confirm"}
	advance(t, m, task.ID, req)
	if _, err := m.Advance(task.ID, req); err != nil {
		t.Fatalf("idempotent replay failed: %v", err)
	}
	req2 := req
	req2.Operator = "other"
	if _, err := m.Advance(task.ID, req2); err == nil || err.(*domain.Error).Code != domain.CodeIdempotencyConflict {
		t.Fatalf("want CodeIdempotencyConflict, got %v", err)
	}
}

func TestGenerationIsolationAfterAnomaly(t *testing.T) {
	m := NewMemory()
	task := lock(t, m)
	oldGen := task.Generation
	advance(t, m, task.ID, OperationRequest{OperationID: "op-1", RuleDigest: task.Snapshot.RuleDigest, Generation: oldGen, LogicalTime: 1, Operator: "op", Stage: "edge_confirm"})

	if _, err := m.RegisterAnomaly(task.ID, AnomalyRequest{Kind: arbiter.AnomalyBurst, RackPos: "R1", Generation: oldGen, RuleDigest: task.Snapshot.RuleDigest}); err != nil {
		t.Fatal(err)
	}
	// A late receipt for the superseded generation must be rejected.
	if _, err := m.Advance(task.ID, OperationRequest{OperationID: "op-2", RuleDigest: task.Snapshot.RuleDigest, Generation: oldGen, LogicalTime: 2, Operator: "op", Stage: "temper"}); err == nil || err.(*domain.Error).Code != domain.CodeRetestGenerationConflict {
		t.Fatalf("want CodeRetestGenerationConflict, got %v", err)
	}
}

func TestConcurrentVerdictSingleWinner(t *testing.T) {
	m := NewMemory()
	task := lock(t, m)
	runManufacturing(t, m, task)
	for _, r := range []string{"alice", "bob"} {
		if err := m.AddReview(task.ID, ReviewRequest{Reviewer: r, Qualified: true, Generation: task.Generation}); err != nil {
			t.Fatal(err)
		}
	}

	verdicts := []string{"admit", "isolate", "cancel"}
	var wg sync.WaitGroup
	errs := make([]error, len(verdicts))
	for i, v := range verdicts {
		wg.Add(1)
		go func(i int, v string) {
			defer wg.Done()
			_, errs[i] = m.SubmitVerdict(task.ID, VerdictRequest{Verdict: v, Generation: task.Generation})
		}(i, v)
	}
	wg.Wait()

	ok := 0
	for _, err := range errs {
		if err == nil {
			ok++
		} else if err.(*domain.Error).Code != domain.CodeFinalExists {
			t.Fatalf("unexpected verdict error: %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("expected exactly one winning verdict, got %d", ok)
	}
}

func TestFilmConservationAcrossFlow(t *testing.T) {
	m := NewMemory()
	task := lock(t, m)
	runManufacturing(t, m, task)
	ledger, err := m.GetFilmLedger("FILM-9")
	if err != nil {
		t.Fatal(err)
	}
	if !ledger.Balanced() {
		t.Fatal("film conservation violated")
	}
	if ledger.Account.Finished != 300000 || ledger.Account.Available != 700000 {
		t.Fatalf("unexpected balances: %+v", ledger.Account)
	}
}
