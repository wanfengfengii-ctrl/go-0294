// Package lease implements logical-time resource mutual exclusion. A resource
// (tempering furnace, heat-soak rack position, clean lamination table,
// autoclave window or inspection station) is keyed by a resource key, and two
// leases overlap only if their logical time intervals intersect. Evidence may
// only be written by a valid, unexpired lease holder.
package lease

import "curtainwall.example/assembly-gate/internal/domain"

// Status is the lifecycle state of a lease.
type Status int

const (
	StatusActive Status = iota
	StatusExpired
	StatusClosed
)

// String returns a stable wire name for the lease status.
func (s Status) String() string {
	switch s {
	case StatusActive:
		return "active"
	case StatusExpired:
		return "expired"
	case StatusClosed:
		return "closed"
	default:
		return "unknown"
	}
}

// Lease is a logical-time exclusive hold on a resource.
type Lease struct {
	ResourceKey string `json:"resource_key"`
	Holder      string `json:"holder"`
	Start       int64  `json:"start"`
	End         int64  `json:"end"`
	Status      Status `json:"status"`
}

// Overlaps reports whether two leases on the same resource share any logical
// time, using half-open intervals [Start, End).
func (l Lease) Overlaps(o Lease) bool {
	if l.ResourceKey != o.ResourceKey {
		return false
	}
	return l.Start < o.End && o.Start < l.End
}

// Valid reports whether the lease interval is well-formed and non-degenerate.
func (l Lease) Valid() bool {
	return l.ResourceKey != "" && l.Holder != "" && l.Start >= 0 && l.End > l.Start
}

// ExpiredAt reports whether the lease has run past the given logical time.
func (l Lease) ExpiredAt(logicalTime int64) bool {
	return logicalTime >= l.End
}

// Registry enforces mutual exclusion over a set of active leases.
type Registry struct {
	leases []Lease
}

// NewRegistry returns an empty lease registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Acquire adds a lease, rejecting any overlap with an existing active lease on
// the same resource.
func (r *Registry) Acquire(l Lease) error {
	if !l.Valid() {
		return domain.NewError(domain.CodeLeaseConflict, "invalid lease interval", l.ResourceKey)
	}
	for _, existing := range r.leases {
		if existing.Status != StatusActive {
			continue
		}
		if existing.Overlaps(l) {
			return domain.NewError(domain.CodeLeaseConflict, "resource already leased",
				l.ResourceKey)
		}
	}
	l.Status = StatusActive
	r.leases = append(r.leases, l)
	return nil
}

// Expire marks every lease whose end is at or before logicalTime as expired.
func (r *Registry) Expire(logicalTime int64) {
	for i := range r.leases {
		if r.leases[i].Status == StatusActive && r.leases[i].ExpiredAt(logicalTime) {
			r.leases[i].Status = StatusExpired
		}
	}
}

// CheckWrite verifies that holder holds an unexpired lease on the resource at
// logicalTime, returning CodeLeaseExpired or CodeLeaseConflict otherwise.
func (r *Registry) CheckWrite(resourceKey, holder string, logicalTime int64) error {
	for _, l := range r.leases {
		if l.ResourceKey != resourceKey || l.Holder != holder {
			continue
		}
		if l.Status == StatusActive && logicalTime >= l.Start && logicalTime < l.End {
			return nil
		}
		if l.Status == StatusExpired || l.ExpiredAt(logicalTime) {
			return domain.NewError(domain.CodeLeaseExpired, "lease expired", resourceKey)
		}
	}
	return domain.NewError(domain.CodeLeaseConflict, "no valid lease held", resourceKey)
}

// Clone returns a deep copy of the registry, preserving every lease and its
// status. It is used to stage a transaction so a failed commit can be
// discarded without mutating committed state.
func (r *Registry) Clone() *Registry {
	out := &Registry{leases: make([]Lease, len(r.leases))}
	copy(out.leases, r.leases)
	return out
}

// ActiveLeases returns a copy of every currently active lease.
func (r *Registry) ActiveLeases() []Lease {
	out := make([]Lease, 0, len(r.leases))
	for _, l := range r.leases {
		if l.Status == StatusActive {
			out = append(out, l)
		}
	}
	return out
}

// AllLeases returns a copy of every lease in insertion order.
func (r *Registry) AllLeases() []Lease {
	out := make([]Lease, len(r.leases))
	copy(out, r.leases)
	return out
}

// Restore loads a committed lease directly with its status preserved, used by
// startup recovery so expired leases are not re-opened as active.
func (r *Registry) Restore(l Lease) {
	r.leases = append(r.leases, l)
}
