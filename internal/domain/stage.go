package domain

// Stage is a fixed, ordered manufacturing step. The order is frozen by the
// domain rules: edge confirmation, tempering, stress inspection, heat-soak,
// lamination, pre-press, autoclave cure, optical scan, destructive test,
// anomaly retest, dual review and final verdict. A step may only advance once
// every predecessor is closed, forming a unique contiguous prefix.
type Stage int

const (
	StageEdgeConfirm Stage = iota
	StageTemper
	StageStress
	StageHeatSoak
	StageLamination
	StagePrepPress
	StageAutoclave
	StageOpticalScan
	StageDestructive
	StageRetest
	StageReview
	StageFinal
)

// String returns a stable wire name for the stage.
func (s Stage) String() string {
	names := [...]string{
		"edge_confirm",
		"temper",
		"stress",
		"heat_soak",
		"lamination",
		"pre_press",
		"autoclave",
		"optical_scan",
		"destructive",
		"retest",
		"review",
		"final",
	}
	if int(s) < 0 || int(s) >= len(names) {
		return "unknown"
	}
	return names[s]
}

// IsValid reports whether s is a known stage.
func (s Stage) IsValid() bool {
	return s >= StageEdgeConfirm && s <= StageFinal
}

// Next returns the following stage, or ok=false at the terminal stage.
func (s Stage) Next() (Stage, bool) {
	if !s.IsValid() || s == StageFinal {
		return 0, false
	}
	return s + 1, true
}

// Prefix tracks which stages are complete. A stage may be completed only when
// the immediately preceding stage is already complete, enforcing a single
// contiguous prefix with no gaps.
type Prefix uint16

// Has reports whether the stage is complete.
func (p Prefix) Has(s Stage) bool {
	return s.IsValid() && p&(1<<uint(s)) != 0
}

// Complete marks a stage complete after validating ordering.
func (p Prefix) Complete(s Stage) (Prefix, error) {
	if !s.IsValid() {
		return p, NewError(CodeStageOutOfOrder, "unknown stage")
	}
	if s == StageEdgeConfirm {
		if p.Has(s) {
			return p, NewError(CodeStageOutOfOrder, "stage already complete", s.String())
		}
		return p | 1<<uint(s), nil
	}
	prev := s - 1
	if !p.Has(prev) {
		return p, NewError(CodeStageOutOfOrder, "predecessor not complete", prev.String(), s.String())
	}
	if p.Has(s) {
		return p, NewError(CodeStageOutOfOrder, "stage already complete", s.String())
	}
	return p | 1<<uint(s), nil
}

// HighestCompleted returns the last completed stage in the prefix.
func (p Prefix) HighestCompleted() (Stage, bool) {
	var last Stage
	found := false
	for s := StageEdgeConfirm; s <= StageFinal; s++ {
		if p.Has(s) {
			last = s
			found = true
		}
	}
	return last, found
}
