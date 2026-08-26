package domain

import (
	"fmt"
	"sort"
	"strings"
)

// RetestScopeKey is the composite, deterministic sort key used for both retest
// members and API rejection reasons. The field order is frozen by the domain
// rules: facade zone, plate, raw glass, furnace run, rack position, inspection
// grid, generation. Lexicographic comparison of the rendered key therefore
// yields the exact mandated ordering.
type RetestScopeKey struct {
	FacadeZone string
	Plate      string
	RawGlass   string
	FurnaceRun string
	RackPos    string
	Inspection string
	Generation int
}

// String renders the key as a single sortable composite token.
func (k RetestScopeKey) String() string {
	return fmt.Sprintf("%s|%s|%s|%s|%s|%s|%09d",
		k.FacadeZone, k.Plate, k.RawGlass, k.FurnaceRun, k.RackPos, k.Inspection, k.Generation)
}

// RetestMember is one affected material item in a deterministic retest scope.
type RetestMember struct {
	Key      RetestScopeKey `json:"key"`
	Reason   string         `json:"reason"`
	Obsolete bool           `json:"obsolete"`
}

// SortRetestMembers sorts members by their composite key in place, deduplicating
// on the key so an identical anomaly fact can only produce one member.
func SortRetestMembers(members []RetestMember) []RetestMember {
	seen := make(map[string]struct{}, len(members))
	out := members[:0]
	for _, m := range members {
		key := m.Key.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Key.String() < out[j].Key.String()
	})
	return out
}

// SortReasons deterministically sorts free-text reasons by the mandated
// composite key. Each reason is expected to be a "zone|plate|raw|run|rack|
// grid|gen" token; tokens without the full prefix sort after the structured
// ones so ordering stays stable.
func SortReasons(reasons []string) []string {
	seen := make(map[string]struct{}, len(reasons))
	dedup := make([]string, 0, len(reasons))
	for _, r := range reasons {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		dedup = append(dedup, r)
	}
	sort.SliceStable(dedup, func(i, j int) bool {
		return reasonRank(dedup[i]) < reasonRank(dedup[j])
	})
	return dedup
}

func reasonRank(r string) string {
	parts := strings.SplitN(r, "|", 7)
	if len(parts) == 7 {
		return fmt.Sprintf("0|%s", r)
	}
	return "1|" + r
}
