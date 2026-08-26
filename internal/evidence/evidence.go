// Package evidence implements the tempering / heat-soak / lamination / autoclave
// evidence recorder: it validates process dependency, enforces strictly
// increasing logical-time samples, and maintains the continuous heat-soak and
// autoclave segment prefixes together with the deterministic coverage matrix.
package evidence

import (
	"sort"

	"curtainwall.example/assembly-gate/internal/domain"
)

// SamplePoint is one submitted heat-soak or autoclave sample, tagged with its
// declared segment and (for heat-soak) its rack position.
type SamplePoint struct {
	LogicalTime  int64  `json:"logical_time"`
	Value        int64  `json:"value"`
	RackPosition string `json:"rack_position,omitempty"`
	Segment      string `json:"segment"`
}

// HeatSegmentOrder returns the frozen heat-soak segment order index, or false
// for an unknown segment.
func HeatSegmentOrder(segment string) (int, bool) {
	switch segment {
	case "ramp_up":
		return 0, true
	case "hold":
		return 1, true
	case "ramp_down":
		return 2, true
	default:
		return 0, false
	}
}

// AutoclaveOrder returns the frozen autoclave phase order index, or false for
// an unknown phase.
func AutoclaveOrder(phase string) (int, bool) {
	switch phase {
	case "preheat":
		return 0, true
	case "pressurize":
		return 1, true
	case "hold":
		return 2, true
	case "depressurize":
		return 3, true
	case "cool":
		return 4, true
	default:
		return 0, false
	}
}

// AutoclavePhaseCount is the number of mandatory autoclave phases that must all
// be present before the cure timeline is considered closed for admission.
const AutoclavePhaseCount = 5

// AutoclavePrefixClosed reports whether the samples form a continuous autoclave
// prefix that includes every mandatory phase (preheat, pressurize, hold,
// depressurize, cool). A continuous-but-incomplete prefix is not enough: the
// admission gate requires the whole cure timeline to be closed.
func AutoclavePrefixClosed(samples []SamplePoint) bool {
	if len(samples) == 0 {
		return false
	}
	orders, err := SegmentOrders(samples, true)
	if err != nil {
		return false
	}
	if err := ValidateContinuousPrefix(orders); err != nil {
		return false
	}
	seen := make(map[int]bool, len(orders))
	for _, o := range orders {
		seen[o] = true
	}
	for i := 0; i < AutoclavePhaseCount; i++ {
		if !seen[i] {
			return false
		}
	}
	return true
}

// ValidateSequence enforces strictly increasing logical times across the whole
// sample slice, independent of segment or position.
func ValidateSequence(samples []SamplePoint) error {
	for i := 1; i < len(samples); i++ {
		if samples[i].LogicalTime <= samples[i-1].LogicalTime {
			return domain.NewError(domain.CodeSampleGap,
				"sample logical time not strictly increasing")
		}
	}
	return nil
}

// ValidateContinuousPrefix enforces that the ordered segment/phase indices of
// the samples form a contiguous prefix starting at zero with no gaps and no
// out-of-order transitions. It is shared by heat-soak and autoclave because
// both require a continuous phase prefix.
func ValidateContinuousPrefix(orders []int) error {
	if len(orders) == 0 {
		return domain.NewError(domain.CodeSampleGap, "no samples submitted")
	}
	seen := make(map[int]bool, len(orders))
	maxOrder := -1
	for _, o := range orders {
		if o < 0 {
			return domain.NewError(domain.CodeSampleGap, "unknown segment")
		}
		if o == 0 {
			seen[0] = true
			if o > maxOrder {
				maxOrder = o
			}
			continue
		}
		if !seen[o-1] {
			return domain.NewError(domain.CodeSampleGap,
				"segment gap: predecessor not present")
		}
		seen[o] = true
		if o > maxOrder {
			maxOrder = o
		}
	}
	for i := 0; i <= maxOrder; i++ {
		if !seen[i] {
			return domain.NewError(domain.CodeSampleGap, "missing segment in prefix")
		}
	}
	return nil
}

// SegmentOrders maps the declared segments/phases to their frozen order,
// returning a stable error for an unknown declaration.
func SegmentOrders(samples []SamplePoint, isAutoclave bool) ([]int, error) {
	orders := make([]int, len(samples))
	for i, s := range samples {
		var o int
		var ok bool
		if isAutoclave {
			o, ok = AutoclaveOrder(s.Segment)
		} else {
			o, ok = HeatSegmentOrder(s.Segment)
		}
		if !ok {
			return nil, domain.NewError(domain.CodeSampleGap, "unknown segment", s.Segment)
		}
		orders[i] = o
	}
	return orders, nil
}

// CoverageCell is one rack-position x heat-segment coverage state.
type CoverageCell struct {
	RackPosition string `json:"rack_position"`
	Segment      string `json:"segment"`
	Covered      bool   `json:"covered"`
	SampleCount  int    `json:"sample_count"`
}

// CoverageMatrix is the deterministic rack-position x segment coverage view.
type CoverageMatrix struct {
	FurnaceRun string         `json:"furnace_run"`
	Cells      []CoverageCell `json:"cells"`
}

// BuildCoverage computes the heat-soak coverage matrix from a rack plan and a
// set of samples. A cell is covered when at least one strictly-increasing
// sample falls on that position and segment.
func BuildCoverage(rack domain.RackPlan, samples []SamplePoint) (*CoverageMatrix, error) {
	if err := ValidateSequence(samples); err != nil {
		return nil, err
	}
	type cellKey struct {
		pos string
		seg string
	}
	counts := make(map[cellKey]int)
	validPos := make(map[string]bool, len(rack.Positions))
	for _, p := range rack.Positions {
		validPos[p.ID] = true
	}
	for _, s := range samples {
		if s.RackPosition == "" || !validPos[s.RackPosition] {
			return nil, domain.NewError(domain.CodeSampleGap,
				"sample references unknown rack position", s.RackPosition)
		}
		if _, ok := HeatSegmentOrder(s.Segment); !ok {
			return nil, domain.NewError(domain.CodeSampleGap,
				"unknown heat segment", s.Segment)
		}
		counts[cellKey{s.RackPosition, s.Segment}]++
	}
	segments := []string{"ramp_up", "hold", "ramp_down"}
	matrix := &CoverageMatrix{FurnaceRun: rack.FurnaceRun}
	for _, p := range rack.Positions {
		for _, seg := range segments {
			n := counts[cellKey{p.ID, seg}]
			matrix.Cells = append(matrix.Cells, CoverageCell{
				RackPosition: p.ID,
				Segment:      seg,
				Covered:      n > 0,
				SampleCount:  n,
			})
		}
	}
	sort.Slice(matrix.Cells, func(i, j int) bool {
		if matrix.Cells[i].RackPosition != matrix.Cells[j].RackPosition {
			return matrix.Cells[i].RackPosition < matrix.Cells[j].RackPosition
		}
		oi, _ := HeatSegmentOrder(matrix.Cells[i].Segment)
		oj, _ := HeatSegmentOrder(matrix.Cells[j].Segment)
		return oi < oj
	})
	return matrix, nil
}

// FullyCovered reports whether every cell in the matrix is covered, i.e. every
// rack position has samples in all three heat segments.
func (m *CoverageMatrix) FullyCovered() bool {
	for _, c := range m.Cells {
		if !c.Covered {
			return false
		}
	}
	return len(m.Cells) > 0
}
