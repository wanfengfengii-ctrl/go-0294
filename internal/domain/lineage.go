package domain

// The append-only material lineage. Nodes are raw glass, tempered sheets and
// laminated assemblies. Edges may only advance from raw glass to tempered
// sheet and from tempered sheet to laminated assembly. Nothing is ever
// deleted; obsolete generations are flagged instead. Plate numbers are unique
// within a facade zone and raw-glass identities are globally unique.

// NodeKind classifies a material node.
type NodeKind int

const (
	KindRawGlass NodeKind = iota
	KindTempered
	KindLaminated
)

// String returns a stable wire name for the node kind.
func (k NodeKind) String() string {
	switch k {
	case KindRawGlass:
		return "raw_glass"
	case KindTempered:
		return "tempered"
	case KindLaminated:
		return "laminated"
	default:
		return "unknown"
	}
}

// MaterialNode is an immutable lineage vertex. It is never mutated in place;
// rework adds a new node with a higher generation.
type MaterialNode struct {
	ID         string   `json:"id"`
	Kind       NodeKind `json:"kind"`
	FurnaceLot string   `json:"furnace_lot"`
	Generation int      `json:"generation"`
	Obsolete   bool     `json:"obsolete"`
}

// LineageEdge is an append-only parent -> child relation.
type LineageEdge struct {
	Parent string `json:"parent"`
	Child  string `json:"child"`
}

// validParentKind returns the only kind allowed to precede the child kind, or
// false when the child kind has no legal parent.
func validParentKind(child NodeKind) (NodeKind, bool) {
	switch child {
	case KindTempered:
		return KindRawGlass, true
	case KindLaminated:
		return KindTempered, true
	default:
		return 0, false
	}
}

// ValidateEdge checks that parent and child kinds follow the frozen raw ->
// tempered -> laminated progression.
func ValidateEdge(parent, child MaterialNode) error {
	if parent.ID == child.ID {
		return NewError(CodeGeometryInvalid, "self-referential lineage edge")
	}
	want, ok := validParentKind(child.Kind)
	if !ok {
		return NewError(CodeGeometryInvalid, "child kind cannot have a parent", child.Kind.String())
	}
	if parent.Kind != want {
		return NewError(CodeGeometryInvalid, "illegal lineage progression",
			parent.Kind.String(), child.Kind.String())
	}
	return nil
}

// Lineage is the append-only graph. It enforces edge legality and idempotent
// re-insertion of duplicate edges while rejecting forks.
type Lineage struct {
	Nodes map[string]MaterialNode `json:"nodes"`
	Edges []LineageEdge           `json:"edges"`
}

// NewLineage returns an empty lineage graph.
func NewLineage() *Lineage {
	return &Lineage{
		Nodes: make(map[string]MaterialNode),
		Edges: []LineageEdge{},
	}
}

// AddNode inserts a node, rejecting duplicate identities.
func (l *Lineage) AddNode(n MaterialNode) error {
	if n.ID == "" {
		return NewError(CodeIdentityDuplicate, "empty node identity")
	}
	if _, ok := l.Nodes[n.ID]; ok {
		return NewError(CodeIdentityDuplicate, "duplicate node identity", n.ID)
	}
	l.Nodes[n.ID] = n
	return nil
}

// AddEdge appends a legal edge, treating a duplicate as idempotent.
func (l *Lineage) AddEdge(parentID, childID string) error {
	parent, ok := l.Nodes[parentID]
	if !ok {
		return NewError(CodeGeometryInvalid, "unknown parent node", parentID)
	}
	child, ok := l.Nodes[childID]
	if !ok {
		return NewError(CodeGeometryInvalid, "unknown child node", childID)
	}
	if err := ValidateEdge(parent, child); err != nil {
		return err
	}
	edge := LineageEdge{Parent: parentID, Child: childID}
	for _, existing := range l.Edges {
		if existing == edge {
			return nil // idempotent duplicate
		}
	}
	l.Edges = append(l.Edges, edge)
	return nil
}
