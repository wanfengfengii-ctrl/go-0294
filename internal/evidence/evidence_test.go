package evidence

import (
	"testing"

	"curtainwall.example/assembly-gate/internal/domain"
)

func rackPlan() domain.RackPlan {
	return domain.RackPlan{
		FurnaceRun: "RUN-1",
		Positions:  []domain.RackPosition{{ID: "R1", Level: 1}, {ID: "R2", Level: 2}},
		Adjacency:  []domain.AdjacencyPair{{A: "R1", B: "R2"}},
	}
}

func TestBuildCoverageFullCoverage(t *testing.T) {
	samples := []SamplePoint{
		{LogicalTime: 1, Value: 100, RackPosition: "R1", Segment: "ramp_up"},
		{LogicalTime: 2, Value: 200, RackPosition: "R1", Segment: "hold"},
		{LogicalTime: 3, Value: 150, RackPosition: "R1", Segment: "ramp_down"},
		{LogicalTime: 4, Value: 100, RackPosition: "R2", Segment: "ramp_up"},
		{LogicalTime: 5, Value: 200, RackPosition: "R2", Segment: "hold"},
		{LogicalTime: 6, Value: 150, RackPosition: "R2", Segment: "ramp_down"},
	}
	m, err := BuildCoverage(rackPlan(), samples)
	if err != nil {
		t.Fatal(err)
	}
	if !m.FullyCovered() {
		t.Fatalf("matrix should be fully covered: %+v", m.Cells)
	}
}

func TestBuildCoverageRejectsUnknownRack(t *testing.T) {
	samples := []SamplePoint{
		{LogicalTime: 1, Value: 100, RackPosition: "R9", Segment: "ramp_up"},
	}
	if _, err := BuildCoverage(rackPlan(), samples); err == nil {
		t.Fatal("unknown rack position must be rejected")
	}
}

func TestContinuousPrefixRejectsGap(t *testing.T) {
	// hold without ramp_up is a gap.
	if err := ValidateContinuousPrefix([]int{1}); err == nil {
		t.Fatal("gap must be rejected")
	}
	if err := ValidateContinuousPrefix([]int{0, 1, 2}); err != nil {
		t.Fatalf("valid prefix rejected: %v", err)
	}
	if err := ValidateContinuousPrefix([]int{0, 2}); err == nil {
		t.Fatal("missing hold must be rejected")
	}
}

func TestAutoclavePressureIntegral(t *testing.T) {
	samples := []SamplePoint{
		{LogicalTime: 0, Value: 100, Segment: "preheat"},
		{LogicalTime: 10, Value: 200, Segment: "pressurize"},
		{LogicalTime: 20, Value: 200, Segment: "hold"},
		{LogicalTime: 30, Value: 200, Segment: "hold"},
		{LogicalTime: 40, Value: 100, Segment: "depressurize"},
		{LogicalTime: 50, Value: 50, Segment: "cool"},
	}
	r, err := ComputeAutoclave(samples)
	if err != nil {
		t.Fatal(err)
	}
	// Trapezoid integral over the five intervals, each of width 10.
	want := int64((100+200)/2*10 + (200+200)/2*10 + (200+200)/2*10 + (200+100)/2*10 + (100+50)/2*10)
	if r.PressureIntegral != want {
		t.Fatalf("integral = %d, want %d", r.PressureIntegral, want)
	}
	if r.HoldDuration != 10 {
		t.Fatalf("hold duration = %d, want 10", r.HoldDuration)
	}
}

func TestAutoclaveRejectsZeroInterval(t *testing.T) {
	samples := []SamplePoint{
		{LogicalTime: 10, Value: 100, Segment: "preheat"},
		{LogicalTime: 10, Value: 200, Segment: "pressurize"},
	}
	if _, err := ComputeAutoclave(samples); err == nil {
		t.Fatal("zero time interval must be rejected")
	}
}
