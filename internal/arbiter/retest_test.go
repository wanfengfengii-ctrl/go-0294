package arbiter

import (
	"testing"

	"curtainwall.example/assembly-gate/internal/domain"
)

func rackSnapshot() domain.DesignSnapshot {
	return domain.DesignSnapshot{
		FacadeZone: "F1", PlateNumber: "P-1", FurnaceLot: "LOT-1", FilmBatch: "FILM-1",
		Rack: domain.RackPlan{
			FurnaceRun: "RUN-1",
			Positions:  []domain.RackPosition{{ID: "R1", Level: 1}, {ID: "R2", Level: 2}, {ID: "R3", Level: 3}},
			Adjacency:  []domain.AdjacencyPair{{A: "R1", B: "R2"}, {A: "R2", B: "R3"}},
		},
	}
}

func TestRetestScopeDeterministicSorted(t *testing.T) {
	trigger := domain.RetestScopeKey{FacadeZone: "F1", Plate: "P-1", RawGlass: "LOT-1", FurnaceRun: "RUN-1", RackPos: "R2", Generation: 1}
	b := NewScopeBuilder(rackSnapshot(), trigger, AnomalyBurst)
	set := b.Build()

	if len(set.Members) == 0 {
		t.Fatal("empty retest scope")
	}
	// The trigger and every rack-adjacent position must be present, sorted by key.
	wantRacks := map[string]bool{"R1": true, "R2": true, "R3": true}
	for _, m := range set.Members {
		if m.Key.RackPos != "" {
			wantRacks[m.Key.RackPos] = false
		}
	}
	for rack, remaining := range wantRacks {
		if remaining {
			t.Fatalf("rack %s missing from retest scope", rack)
		}
	}
	// Deterministic ordering: members are sorted by their composite key.
	for i := 1; i < len(set.Members); i++ {
		if set.Members[i-1].Key.String() >= set.Members[i].Key.String() {
			t.Fatalf("members not sorted at %d", i)
		}
	}
}

func TestRetestScopeDeduplicates(t *testing.T) {
	trigger := domain.RetestScopeKey{FacadeZone: "F1", Plate: "P-1", RawGlass: "LOT-1", FurnaceRun: "RUN-1", RackPos: "R1", Generation: 1}
	b := NewScopeBuilder(rackSnapshot(), trigger, AnomalyEdgeChip)
	// Inject a duplicate member that should collapse.
	b.AddMember(trigger, "trigger")
	b.AddMember(trigger, "trigger")
	set := b.Build()
	seen := map[string]bool{}
	for _, m := range set.Members {
		if seen[m.Key.String()] {
			t.Fatalf("duplicate member %s", m.Key.String())
		}
		seen[m.Key.String()] = true
	}
}
