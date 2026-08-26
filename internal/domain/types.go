package domain

// Stable aggregate types shared across the business packages. These mirror the
// documented data model and are the wire/persistence shape; every field is
// primitive or a nested stable type so it can round-trip through JSON and the
// embedded relational store without codegen.

// DesignSnapshot is the immutable locked design and manufacturing rule set.
// Once locked it must never be mutated in place; a new lock produces a new
// snapshot with a new generation and rule digest.
type DesignSnapshot struct {
	Project        string           `json:"project"`
	FacadeZone     string           `json:"facade_zone"`
	PlateNumber    string           `json:"plate_number"`
	Version        int              `json:"version"`
	RuleDigest     string           `json:"rule_digest"`
	ThicknessUM    int64            `json:"thickness_um"`
	WidthUM        int64            `json:"width_um"`
	HeightUM       int64            `json:"height_um"`
	EdgeMarginUM   int64            `json:"edge_margin_um"`
	EdgeScheme     string           `json:"edge_scheme"`
	Geometry       Polygon          `json:"geometry"`
	FurnaceLot     string           `json:"furnace_lot"`
	FilmBatch      string           `json:"film_batch"`
	FilmOpeningUM2 int64            `json:"film_opening_um2"`
	Thresholds     map[string]int64 `json:"thresholds"`
	LockedGen      int              `json:"locked_generation"`
	Rack           RackPlan         `json:"rack"`
	Inspection     InspectionPlan   `json:"inspection"`
	Programs       []string         `json:"programs"`
}

// RackPlan is the locked heat-soak loading adjacency graph: furnace run,
// rack positions with levels, and the physical adjacency edges between them.
// The retest-scope computation walks these edges to expand a single anomaly
// into the full affected set.
type RackPlan struct {
	FurnaceRun string          `json:"furnace_run"`
	Positions  []RackPosition  `json:"positions"`
	Adjacency  []AdjacencyPair `json:"adjacency"`
}

// RackPosition is a single heat-soak rack level inside a furnace run.
type RackPosition struct {
	ID    string `json:"id"`
	Level int    `json:"level"`
}

// AdjacencyPair is an undirected physical-adjacency edge between two rack
// positions; a burst on one propagates to its neighbours.
type AdjacencyPair struct {
	A string `json:"a"`
	B string `json:"b"`
}

// InspectionPlan is the locked inspection grid and sampling mapping used to
// locate anomalies and destructive samples deterministically.
type InspectionPlan struct {
	Grid        []string          `json:"grid"`
	Sampling    map[string]string `json:"sampling"`
	Destructive int               `json:"destructive"`
}

// MeasurementKind names a fixed-point physical quality metric.
type MeasurementKind string

const (
	MeasureSurfaceStress MeasurementKind = "surface_stress"
	MeasureEdgeStress    MeasurementKind = "edge_stress"
	MeasureBow           MeasurementKind = "bow"
	MeasureBubbleRate    MeasurementKind = "bubble_rate"
)

// Measurement is one computed, fixed-point quality reading produced by a
// scripted instrument, with its overflow-safe integer result.
type Measurement struct {
	Kind       MeasurementKind `json:"kind"`
	Value      int64           `json:"value"`
	Overflow   bool            `json:"overflow"`
	WellFormed bool            `json:"well_formed"`
}

// ProcessEvidence is an immutable, append-only record of a completed step.
type ProcessEvidence struct {
	TaskID      string `json:"task_id"`
	Stage       Stage  `json:"stage"`
	Generation  int    `json:"generation"`
	LogicalTime int64  `json:"logical_time"`
	Operator    string `json:"operator"`
	LeaseRef    string `json:"lease_ref"`
	Digest      string `json:"digest"`
}

// Sample is one strictly-ordered heat-soak or autoclave sample point.
type Sample struct {
	LogicalTime int64 `json:"logical_time"`
	Value       int64 `json:"value"` // temperature or pressure in its fixed scale
}

// Segment labels a contiguous heat-soak or autoclave phase.
type Segment int

const (
	SegmentRampUp Segment = iota
	SegmentHold
	SegmentRampDown
)

// String returns a stable wire name for the segment.
func (s Segment) String() string {
	switch s {
	case SegmentRampUp:
		return "ramp_up"
	case SegmentHold:
		return "hold"
	case SegmentRampDown:
		return "ramp_down"
	default:
		return "unknown"
	}
}
