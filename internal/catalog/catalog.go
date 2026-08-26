// Package catalog implements the curtain-wall glass design and material rules
// catalog: it validates and locks an immutable design snapshot, mints a
// deterministic rule digest, and rejects stale digests, duplicate identities
// and construction mismatches before any task or lineage node is created.
package catalog

import (
	"curtainwall.example/assembly-gate/internal/domain"
)

// Catalog holds the locked snapshots and enforces identity uniqueness across
// the whole project. It is the authoritative source for rule digests and the
// single place that decides whether an execution request may proceed.
type Catalog struct {
	snapshots map[string]domain.DesignSnapshot
	// rawGlass tracks the globally unique raw-glass identities.
	rawGlass map[string]string
	// plates tracks plate numbers unique within a facade zone.
	plates map[string]string
}

// New returns an empty design catalog.
func New() *Catalog {
	return &Catalog{
		snapshots: make(map[string]domain.DesignSnapshot),
		rawGlass:  make(map[string]string),
		plates:    make(map[string]string),
	}
}

// lockKey composes the task identity: project + facade zone + plate number.
func lockKey(p domain.DesignSnapshot) string {
	return p.Project + "|" + p.FacadeZone + "|" + p.PlateNumber
}

// zoneKey composes the facade-zone plate-uniqueness key.
func zoneKey(p domain.DesignSnapshot) string {
	return p.Project + "|" + p.FacadeZone + "|" + p.PlateNumber
}

// Lock validates the snapshot's geometry and identity, computes its rule
// digest and stores it immutably. It returns the digest and the locked
// generation. Any rejection happens before the snapshot is retained.
func (c *Catalog) Lock(snapshot domain.DesignSnapshot) (string, int, error) {
	if snapshot.Project == "" || snapshot.FacadeZone == "" || snapshot.PlateNumber == "" {
		return "", 0, domain.NewError(domain.CodeStructureMismatch,
			"project, facade zone and plate number are required")
	}
	if snapshot.ThicknessUM <= 0 {
		return "", 0, domain.NewError(domain.CodeStructureMismatch, "thickness must be positive")
	}
	// Geometry validates before any identity or task state is created.
	bounds := domain.Bounds{
		Width:  snapshot.WidthUM,
		Height: snapshot.HeightUM,
		Margin: snapshot.EdgeMarginUM,
	}
	if err := snapshot.Geometry.Validate(bounds); err != nil {
		return "", 0, err
	}
	zk := zoneKey(snapshot)
	if _, ok := c.plates[zk]; ok {
		return "", 0, domain.NewError(domain.CodeIdentityDuplicate,
			"duplicate plate number in facade zone",
			snapshot.FacadeZone, snapshot.PlateNumber)
	}
	if snapshot.FurnaceLot != "" {
		if _, ok := c.rawGlass[snapshot.FurnaceLot]; ok {
			return "", 0, domain.NewError(domain.CodeIdentityDuplicate,
				"duplicate raw-glass identity", snapshot.FurnaceLot)
		}
	}
	// A new lock always advances the generation.
	gen := snapshot.LockedGen + 1
	snapshot.LockedGen = gen
	snapshot.RuleDigest = Digest(snapshot)
	c.snapshots[lockKey(snapshot)] = snapshot
	c.plates[zk] = lockKey(snapshot)
	if snapshot.FurnaceLot != "" {
		c.rawGlass[snapshot.FurnaceLot] = lockKey(snapshot)
	}
	return snapshot.RuleDigest, gen, nil
}

// ValidateDigest reports whether the given digest matches the locked snapshot
// for the task identity. A mismatched or unknown snapshot returns
// CodeStaleDigest so execution requests carry a matching rule summary.
func (c *Catalog) ValidateDigest(project, zone, plate, digest string) error {
	snap, ok := c.snapshots[project+"|"+zone+"|"+plate]
	if !ok {
		return domain.NewError(domain.CodeStaleDigest, "no locked design for identity")
	}
	if snap.RuleDigest != digest {
		return domain.NewError(domain.CodeStaleDigest, "rule digest does not match locked snapshot")
	}
	return nil
}

// Snapshot returns the locked snapshot for a task identity, or false.
func (c *Catalog) Snapshot(project, zone, plate string) (domain.DesignSnapshot, bool) {
	s, ok := c.snapshots[project+"|"+zone+"|"+plate]
	return s, ok
}

// ThicknessAllowed reports whether the requested construction thickness matches
// the locked snapshot, used to reject construction mismatches on execution.
func ThicknessAllowed(snap domain.DesignSnapshot, requested int64) bool {
	return snap.ThicknessUM == requested
}

// Clone returns a deep copy of the catalog so a transaction can be staged and
// discarded without mutating committed state.
func (c *Catalog) Clone() *Catalog {
	out := &Catalog{
		snapshots: make(map[string]domain.DesignSnapshot, len(c.snapshots)),
		rawGlass:  make(map[string]string, len(c.rawGlass)),
		plates:    make(map[string]string, len(c.plates)),
	}
	for k, v := range c.snapshots {
		out.snapshots[k] = v
	}
	for k, v := range c.rawGlass {
		out.rawGlass[k] = v
	}
	for k, v := range c.plates {
		out.plates[k] = v
	}
	return out
}

// Restore inserts an already-locked snapshot without re-validating or
// re-computing its digest. It is used by startup recovery so committed data is
// never re-checked against rules that may have since changed.
func (c *Catalog) Restore(s domain.DesignSnapshot) {
	c.snapshots[lockKey(s)] = s
	c.plates[zoneKey(s)] = lockKey(s)
	if s.FurnaceLot != "" {
		c.rawGlass[s.FurnaceLot] = lockKey(s)
	}
}
