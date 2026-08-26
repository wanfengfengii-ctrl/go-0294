package catalog

import (
	"testing"

	"curtainwall.example/assembly-gate/internal/domain"
)

func validSnapshot() domain.DesignSnapshot {
	return domain.DesignSnapshot{
		Project: "Tower-A", FacadeZone: "F1", PlateNumber: "P-001",
		Version: 1, ThicknessUM: 12000, WidthUM: 100010, HeightUM: 200010,
		EdgeMarginUM: 5, EdgeScheme: "flat-polish",
		Geometry: domain.Polygon{Outline: domain.Ring{
			{X: 5, Y: 5}, {X: 100005, Y: 5}, {X: 100005, Y: 200005}, {X: 5, Y: 200005},
		}},
		FurnaceLot: "LOT-7", FilmBatch: "FILM-9", FilmOpeningUM2: 1000000,
		Thresholds: map[string]int64{"surface_stress": 1000},
	}
}

func TestLockComputesStableDigest(t *testing.T) {
	c := New()
	d1, g1, err := c.Lock(validSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if d1 == "" {
		t.Fatal("empty digest")
	}
	if g1 != 1 {
		t.Fatalf("generation = %d, want 1", g1)
	}
	// Re-locking a fresh catalog with the identical snapshot yields the same
	// digest (deterministic canonicalization).
	c2 := New()
	d2, _, err := c2.Lock(validSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Fatalf("digest not deterministic: %s != %s", d1, d2)
	}
}

func TestLockRejectsDuplicatePlate(t *testing.T) {
	c := New()
	if _, _, err := c.Lock(validSnapshot()); err != nil {
		t.Fatal(err)
	}
	dup := validSnapshot()
	dup.FurnaceLot = "LOT-8" // distinct raw glass, same plate in same zone
	_, _, err := c.Lock(dup)
	if err == nil || err.(*domain.Error).Code != domain.CodeIdentityDuplicate {
		t.Fatalf("want CodeIdentityDuplicate, got %v", err)
	}
}

func TestLockRejectsDuplicateRawGlass(t *testing.T) {
	c := New()
	if _, _, err := c.Lock(validSnapshot()); err != nil {
		t.Fatal(err)
	}
	dup := validSnapshot()
	dup.PlateNumber = "P-002" // distinct plate, same raw-glass identity
	_, _, err := c.Lock(dup)
	if err == nil || err.(*domain.Error).Code != domain.CodeIdentityDuplicate {
		t.Fatalf("want CodeIdentityDuplicate, got %v", err)
	}
}

func TestValidateDigestRejectsStale(t *testing.T) {
	c := New()
	digest, _, err := c.Lock(validSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ValidateDigest("Tower-A", "F1", "P-001", digest); err != nil {
		t.Fatalf("matching digest rejected: %v", err)
	}
	if err := c.ValidateDigest("Tower-A", "F1", "P-001", "stale"); err == nil ||
		err.(*domain.Error).Code != domain.CodeStaleDigest {
		t.Fatalf("want CodeStaleDigest, got %v", err)
	}
}

func TestLockRejectsDegenerateGeometry(t *testing.T) {
	c := New()
	snap := validSnapshot()
	snap.Geometry = domain.Polygon{Outline: domain.Ring{
		{X: 5, Y: 5}, {X: 50000, Y: 5}, {X: 90000, Y: 5},
	}}
	_, _, err := c.Lock(snap)
	if err == nil || err.(*domain.Error).Code != domain.CodeGeometryInvalid {
		t.Fatalf("want CodeGeometryInvalid, got %v", err)
	}
}
