package film

import (
	"sort"

	"curtainwall.example/assembly-gate/internal/domain"
)

// Manager holds the conserved accounts for every interlayer film batch and the
// append-only entry history. It provides atomic apply semantics: a rejected
// entry never mutates the account and never produces an entry row.
type Manager struct {
	batches map[string]*Ledger
	entries map[string][]Entry
}

// NewManager returns an empty film manager.
func NewManager() *Manager {
	return &Manager{
		batches: make(map[string]*Ledger),
		entries: make(map[string][]Entry),
	}
}

// EnsureBatch opens a batch with the given opening balance if it does not
// exist, returning the existing ledger otherwise.
func (m *Manager) EnsureBatch(batch string, opening int64) (*Ledger, error) {
	if l, ok := m.batches[batch]; ok {
		return l, nil
	}
	l, err := NewLedger(batch, opening)
	if err != nil {
		return nil, err
	}
	m.batches[batch] = l
	m.entries[batch] = []Entry{}
	return l, nil
}

// Ledger returns the ledger for a batch, or nil.
func (m *Manager) Ledger(batch string) *Ledger {
	return m.batches[batch]
}

// Entries returns the append-only entry history for a batch in insertion order.
func (m *Manager) Entries(batch string) []Entry {
	return m.entries[batch]
}

// Apply posts a single entry atomically against a batch. The account is only
// mutated when the post is valid and the balance is sufficient.
func (m *Manager) Apply(batch string, e Entry) error {
	l, ok := m.batches[batch]
	if !ok {
		return domain.NewError(domain.CodeInsufficientArea, "unknown film batch", batch)
	}
	if err := l.Account.Apply(e); err != nil {
		return err
	}
	m.entries[batch] = append(m.entries[batch], e)
	return nil
}

// Batches returns the batch identities in sorted order for deterministic reads.
func (m *Manager) Batches() []string {
	out := make([]string, 0, len(m.batches))
	for b := range m.batches {
		out = append(out, b)
	}
	sort.Strings(out)
	return out
}

// Reconcile verifies every batch still satisfies the conservation invariant.
// It returns a stable error naming the first violating batch, or nil.
func (m *Manager) Reconcile() error {
	for _, b := range m.Batches() {
		if !m.batches[b].Balanced() {
			return domain.NewError(domain.CodeInsufficientArea, "film conservation violated", b)
		}
	}
	return nil
}

// Clone returns a deep copy of the manager for transaction staging so a failed
// transaction can be discarded without mutating committed state.
func (m *Manager) Clone() *Manager {
	out := NewManager()
	for b, l := range m.batches {
		cp := *l
		out.batches[b] = &cp
		entries := make([]Entry, len(m.entries[b]))
		copy(entries, m.entries[b])
		out.entries[b] = entries
	}
	return out
}

// Restore loads a committed ledger and its entry history directly, used by
// startup recovery so conserved state is not re-applied through business
// rules.
func (m *Manager) Restore(l Ledger, entries []Entry) {
	m.batches[l.Batch] = &Ledger{Batch: l.Batch, Opening: l.Opening, Account: l.Account}
	m.entries[l.Batch] = entries
}
