package domain

import "testing"

func rect(x0, y0, x1, y1 int64) Polygon {
	return Polygon{Outline: Ring{
		{X: x0, Y: y0}, {X: x1, Y: y0}, {X: x1, Y: y1}, {X: x0, Y: y1},
	}}
}

var bounds = Bounds{Width: 100000, Height: 200000, Margin: 5}

func TestValidRectanglePasses(t *testing.T) {
	if err := rect(5, 5, 99995, 199995).Validate(bounds); err != nil {
		t.Fatalf("valid rectangle rejected: %v", err)
	}
}

func TestBoundaryViolationByOneMicron(t *testing.T) {
	// One vertex one micron past the right boundary.
	err := rect(5, 5, 99996, 199995).Validate(bounds)
	if err == nil || err.(*Error).Code != CodeGeometryInvalid {
		t.Fatalf("want CodeGeometryInvalid, got %v", err)
	}
}

func TestDegenerateOutlineRejected(t *testing.T) {
	// Three collinear points have zero area.
	p := Polygon{Outline: Ring{{X: 5, Y: 5}, {X: 50000, Y: 5}, {X: 90000, Y: 5}}}
	if err := p.Validate(bounds); err == nil || err.(*Error).Code != CodeGeometryInvalid {
		t.Fatalf("want CodeGeometryInvalid, got %v", err)
	}
}

func TestSelfIntersectingOutlineRejected(t *testing.T) {
	// Bowtie self-intersection.
	p := Polygon{Outline: Ring{{X: 5, Y: 5}, {X: 99995, Y: 199995}, {X: 5, Y: 199995}, {X: 99995, Y: 5}}}
	if err := p.Validate(bounds); err == nil || err.(*Error).Code != CodeGeometryInvalid {
		t.Fatalf("want CodeGeometryInvalid, got %v", err)
	}
}

func TestHoleEscapingOutlineRejected(t *testing.T) {
	p := rect(5, 5, 99995, 199995)
	p.Holes = []Ring{{{X: 200000, Y: 200000}, {X: 210000, Y: 200000}, {X: 205000, Y: 210000}}}
	if err := p.Validate(bounds); err == nil || err.(*Error).Code != CodeGeometryInvalid {
		t.Fatalf("want CodeGeometryInvalid, got %v", err)
	}
}

func TestValidHoleInsideOutlinePasses(t *testing.T) {
	p := rect(5, 5, 99995, 199995)
	p.Holes = []Ring{{{X: 10000, Y: 10000}, {X: 20000, Y: 10000}, {X: 15000, Y: 20000}}}
	if err := p.Validate(bounds); err != nil {
		t.Fatalf("valid hole rejected: %v", err)
	}
}
