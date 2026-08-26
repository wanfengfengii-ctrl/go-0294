package domain

// Integer-micron machining geometry. Every coordinate is a signed micron.
// The outline must be non-degenerate and free of illegal self-intersections;
// every vertex, hole and notch must stay inside the allowed boundary margin.
// Any negative value, out-of-boundary vertex or multiply-add overflow rejects
// the whole request without creating a task or lineage node.

// Point is a signed integer-micron coordinate.
type Point struct {
	X int64 `json:"x"`
	Y int64 `json:"y"`
}

// Ring is a closed polyline; the first vertex is implicitly repeated at the end.
type Ring []Point

// Bounds is the allowed machining envelope. Margin is the minimum legal
// clearance from the glass edge for holes, notches and the outline itself.
type Bounds struct {
	Width  int64 `json:"width_um"`
	Height int64 `json:"height_um"`
	Margin int64 `json:"margin_um"`
}

// Polygon is a machining outline plus any holes and notches expressed as
// counter-rotating rings.
type Polygon struct {
	Outline Ring   `json:"outline"`
	Holes   []Ring `json:"holes"`
}

// Validate checks the outline and every hole against the allowed boundary. It
// returns a stable CodeGeometryInvalid error on the first violation.
func (p Polygon) Validate(b Bounds) error {
	if b.Width <= 0 || b.Height <= 0 {
		return NewError(CodeGeometryInvalid, "boundary is not positive")
	}
	if b.Margin < 0 || b.Margin*2 > b.Width || b.Margin*2 > b.Height {
		return NewError(CodeGeometryInvalid, "margin exceeds boundary")
	}
	if err := validateRing(p.Outline, b, true); err != nil {
		return err
	}
	for i, h := range p.Holes {
		if err := validateRing(h, b, false); err != nil {
			return err
		}
		if !ringInsideRing(h, p.Outline) {
			return NewError(CodeGeometryInvalid, "hole escapes the outline", "hole")
		}
		// Holes must not overlap each other.
		for j := 0; j < i; j++ {
			if ringsIntersect(h, p.Holes[j]) {
				return NewError(CodeGeometryInvalid, "overlapping holes")
			}
		}
	}
	return nil
}

func validateRing(r Ring, b Bounds, requirePositiveArea bool) error {
	if len(r) < 3 {
		return NewError(CodeGeometryInvalid, "ring has fewer than three vertices")
	}
	for _, pt := range r {
		if pt.X < b.Margin || pt.Y < b.Margin ||
			pt.X > b.Width-b.Margin || pt.Y > b.Height-b.Margin {
			return NewError(CodeGeometryInvalid, "vertex outside allowed boundary")
		}
	}
	if selfIntersects(r) {
		return NewError(CodeGeometryInvalid, "ring self-intersects")
	}
	if requirePositiveArea && area2(r) == 0 {
		return NewError(CodeGeometryInvalid, "degenerate outline")
	}
	return nil
}

// area2 returns twice the signed shoelace area (always integer, no division).
func area2(r Ring) int64 {
	var sum int64
	n := len(r)
	for i := 0; i < n; i++ {
		j := (i + 1) % n
		sum += r[i].X*r[j].Y - r[j].X*r[i].Y
	}
	return sum
}

// selfIntersects reports whether any non-adjacent segment pair crosses.
func selfIntersects(r Ring) bool {
	n := len(r)
	for i := 0; i < n; i++ {
		a1 := r[i]
		a2 := r[(i+1)%n]
		for j := i + 1; j < n; j++ {
			// Skip edges that share an endpoint with edge i.
			if (i+1)%n == j || (j+1)%n == i {
				continue
			}
			b1 := r[j]
			b2 := r[(j+1)%n]
			if properIntersect(a1, a2, b1, b2) {
				return true
			}
		}
	}
	return false
}

func orient(a, b, c Point) int64 {
	return (b.X-a.X)*(c.Y-a.Y) - (b.Y-a.Y)*(c.X-a.X)
}

func onSegment(a, b, p Point) bool {
	return min64(a.X, b.X) <= p.X && p.X <= max64(a.X, b.X) &&
		min64(a.Y, b.Y) <= p.Y && p.Y <= max64(a.Y, b.Y)
}

// properIntersect reports a strict crossing: endpoints of one segment lie on
// opposite sides of the other segment and vice versa, plus the collinear
// overlapping case.
func properIntersect(a, b, c, d Point) bool {
	o1 := orient(a, b, c)
	o2 := orient(a, b, d)
	o3 := orient(c, d, a)
	o4 := orient(c, d, b)

	if ((o1 > 0 && o2 < 0) || (o1 < 0 && o2 > 0)) &&
		((o3 > 0 && o4 < 0) || (o3 < 0 && o4 > 0)) {
		return true
	}
	// Collinear overlaps count as illegal for a machining outline.
	if o1 == 0 && onSegment(a, b, c) {
		return true
	}
	if o2 == 0 && onSegment(a, b, d) {
		return true
	}
	if o3 == 0 && onSegment(c, d, a) {
		return true
	}
	if o4 == 0 && onSegment(c, d, b) {
		return true
	}
	return false
}

// ringInsideRing reports whether every vertex of inner lies within outer.
func ringInsideRing(inner, outer Ring) bool {
	for _, p := range inner {
		if !pointInRing(p, outer) {
			return false
		}
	}
	return true
}

// ringsIntersect reports whether two rings' edges cross.
func ringsIntersect(a, b Ring) bool {
	for i := 0; i < len(a); i++ {
		for j := 0; j < len(b); j++ {
			if properIntersect(a[i], a[(i+1)%len(a)], b[j], b[(j+1)%len(b)]) {
				return true
			}
		}
	}
	return false
}

// pointInRing uses ray casting to test containment (boundary counts as inside).
func pointInRing(p Point, ring Ring) bool {
	inside := false
	n := len(ring)
	for i, j := 0, n-1; i < n; j, i = i, i+1 {
		pi, pj := ring[i], ring[j]
		if onSegment(pi, pj, p) {
			return true
		}
		if (pi.Y > p.Y) != (pj.Y > p.Y) {
			xint := (pj.X-pi.X)*(p.Y-pi.Y)/(pj.Y-pi.Y) + pi.X
			if p.X < xint {
				inside = !inside
			}
		}
	}
	return inside
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
