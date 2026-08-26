package arbiter

import (
	"curtainwall.example/assembly-gate/internal/domain"
)

// AnomalyKind names a registered manufacturing anomaly that expands into a
// deterministic retest scope.
type AnomalyKind string

const (
	AnomalyBurst        AnomalyKind = "burst"
	AnomalyEdgeChip     AnomalyKind = "edge_chip"
	AnomalyLowStress    AnomalyKind = "low_stress"
	AnomalyHeatGap      AnomalyKind = "heat_gap"
	AnomalyAutoclave    AnomalyKind = "autoclave_interrupt"
	AnomalyDelamination AnomalyKind = "delamination"
	AnomalyBubble       AnomalyKind = "bubble"
	AnomalyOptical      AnomalyKind = "optical"
	AnomalyDestructive  AnomalyKind = "destructive_failure"
)

// RetestSet is a unique, deterministically sorted retest scope. Its summary
// digest guarantees that the same anomaly fact produces exactly one scope.
type RetestSet struct {
	SummaryDigest string                `json:"summary_digest"`
	Kind          AnomalyKind           `json:"kind"`
	Members       []domain.RetestMember `json:"members"`
}

// ScopeBuilder accumulates the affected retest members for a locked design
// snapshot and a triggering anomaly.
type ScopeBuilder struct {
	snapshot domain.DesignSnapshot
	trigger  domain.RetestScopeKey
	kind     AnomalyKind
	members  map[string]domain.RetestMember
}

// NewScopeBuilder creates a retest scope builder rooted at the anomaly's
// material identity.
func NewScopeBuilder(snapshot domain.DesignSnapshot, trigger domain.RetestScopeKey, kind AnomalyKind) *ScopeBuilder {
	return &ScopeBuilder{
		snapshot: snapshot,
		trigger:  trigger,
		kind:     kind,
		members:  make(map[string]domain.RetestMember),
	}
}

// add records a member keyed by its composite identity, with the given reason.
func (b *ScopeBuilder) add(key domain.RetestScopeKey, reason string) {
	b.members[key.String()] = domain.RetestMember{Key: key, Reason: reason}
}

// Build expands the anomaly into the affected set: the trigger itself, every
// rack position adjacent to the trigger (following the locked adjacency
// graph), every other plate sharing the same furnace lot, and every plate
// sharing the same interlayer film generation. It then deduplicates and sorts
// the members by the mandated composite key.
func (b *ScopeBuilder) Build() *RetestSet {
	b.add(b.trigger, "trigger")
	b.expandAdjacency()
	b.expandFurnaceLot()
	b.expandFilmGeneration()

	members := make([]domain.RetestMember, 0, len(b.members))
	for _, m := range b.members {
		members = append(members, m)
	}
	members = domain.SortRetestMembers(members)
	return &RetestSet{
		Kind:    b.kind,
		Members: members,
	}
}

// expandAdjacency follows the locked rack adjacency edges outward from the
// trigger rack position, adding each neighbouring position to the scope.
func (b *ScopeBuilder) expandAdjacency() {
	if b.trigger.RackPos == "" {
		return
	}
	seen := map[string]bool{b.trigger.RackPos: true}
	queue := []string{b.trigger.RackPos}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, edge := range b.snapshot.Rack.Adjacency {
			var other string
			switch {
			case edge.A == cur:
				other = edge.B
			case edge.B == cur:
				other = edge.A
			default:
				continue
			}
			if seen[other] {
				continue
			}
			seen[other] = true
			queue = append(queue, other)
			key := b.trigger
			key.RackPos = other
			b.add(key, "rack_adjacent")
		}
	}
}

// expandFurnaceLot adds a member representing the shared furnace lot. In the
// single-task flow this marks the trigger plate; in a multi-task flow the
// caller supplies additional members through AddMember.
func (b *ScopeBuilder) expandFurnaceLot() {
	key := b.trigger
	key.RawGlass = b.snapshot.FurnaceLot
	b.add(key, "furnace_lot")
}

// expandFilmGeneration adds a member keyed by the shared interlayer film
// generation identity.
func (b *ScopeBuilder) expandFilmGeneration() {
	key := b.trigger
	key.Inspection = b.snapshot.FilmBatch
	b.add(key, "film_generation")
}

// AddMember lets the store inject additional affected plates (same furnace
// lot or same film batch) discovered across the project before the scope is
// finalized.
func (b *ScopeBuilder) AddMember(key domain.RetestScopeKey, reason string) {
	b.add(key, reason)
}

// Members returns the current accumulated members for inspection before Build.
func (b *ScopeBuilder) Members() []domain.RetestMember {
	out := make([]domain.RetestMember, 0, len(b.members))
	for _, m := range b.members {
		out = append(out, m)
	}
	return domain.SortRetestMembers(out)
}
