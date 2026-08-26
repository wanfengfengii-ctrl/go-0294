// Package film implements the integer square-millimetre conservation ledger for
// interlayer film batches. Every movement (issue, cut, recycle, sample, loss)
// is a double-entry post, and the opening balance must always equal the sum of
// the available, in-progress, finished, recycled, sample and loss buckets.
package film

import (
	"curtainwall.example/assembly-gate/internal/domain"
)

// EntryKind classifies a film-ledger double-entry post.
type EntryKind int

const (
	EntryIssue EntryKind = iota
	EntryCut
	EntryRecycle
	EntrySample
	EntryLoss
)

// String returns a stable wire name for the entry kind.
func (k EntryKind) String() string {
	switch k {
	case EntryIssue:
		return "issue"
	case EntryCut:
		return "cut"
	case EntryRecycle:
		return "recycle"
	case EntrySample:
		return "sample"
	case EntryLoss:
		return "loss"
	default:
		return "unknown"
	}
}

// Account is the conserved state of a single film batch, expressed in integer
// square millimetres.
type Account struct {
	Available  int64 `json:"available"`
	InProgress int64 `json:"in_progress"`
	Finished   int64 `json:"finished"`
	Recycled   int64 `json:"recycled"`
	Sample     int64 `json:"sample"`
	Loss       int64 `json:"loss"`
}

// Opening is the conserved invariant: available + in-progress + finished +
// recycled + sample + loss. It never changes across a valid sequence of posts.
func (a Account) Opening() int64 {
	return a.Available + a.InProgress + a.Finished + a.Recycled + a.Sample + a.Loss
}

// Entry is one atomic double-entry post. Amount is always non-negative; the
// sign of the movement is determined by the kind.
type Entry struct {
	Kind   EntryKind `json:"kind"`
	Amount int64     `json:"amount_um2"`
}

// Validate returns a stable error for a negative or non-positive amount.
func (e Entry) Validate() error {
	if e.Amount <= 0 {
		return domain.NewError(domain.CodeInsufficientArea, "entry amount must be positive", e.Kind.String())
	}
	switch e.Kind {
	case EntryIssue, EntryCut, EntryRecycle, EntrySample, EntryLoss:
		return nil
	default:
		return domain.NewError(domain.CodeInsufficientArea, "unknown entry kind")
	}
}

// Apply posts a single entry, mutating the account in place. It returns
// domain.ErrInsufficientArea when a post would drive the available balance
// negative, and leaves the account unchanged in that case.
func (a *Account) Apply(e Entry) error {
	if err := e.Validate(); err != nil {
		return err
	}
	switch e.Kind {
	case EntryIssue:
		// Issue moves area from available into in-progress (cut for a job).
		if a.Available < e.Amount {
			return domain.NewError(domain.CodeInsufficientArea, "insufficient available area")
		}
		a.Available -= e.Amount
		a.InProgress += e.Amount
	case EntryCut:
		// Cut moves in-progress area into finished product.
		if a.InProgress < e.Amount {
			return domain.NewError(domain.CodeInsufficientArea, "insufficient in-progress area")
		}
		a.InProgress -= e.Amount
		a.Finished += e.Amount
	case EntryRecycle:
		// Recycle returns in-progress area to available.
		if a.InProgress < e.Amount {
			return domain.NewError(domain.CodeInsufficientArea, "insufficient in-progress area")
		}
		a.InProgress -= e.Amount
		a.Available += e.Amount
	case EntrySample:
		// Sample draws from available into the sample bucket.
		if a.Available < e.Amount {
			return domain.NewError(domain.CodeInsufficientArea, "insufficient available area")
		}
		a.Available -= e.Amount
		a.Sample += e.Amount
	case EntryLoss:
		// Loss draws from in-progress into the loss bucket.
		if a.InProgress < e.Amount {
			return domain.NewError(domain.CodeInsufficientArea, "insufficient in-progress area")
		}
		a.InProgress -= e.Amount
		a.Loss += e.Amount
	}
	return nil
}

// Ledger binds a batch identity to a conserved account and its opening balance.
type Ledger struct {
	Batch   string  `json:"batch"`
	Opening int64   `json:"opening"`
	Account Account `json:"account"`
}

// NewLedger opens a batch with an available opening balance and records the
// invariant total.
func NewLedger(batch string, opening int64) (*Ledger, error) {
	if opening < 0 {
		return nil, domain.NewError(domain.CodeInsufficientArea, "negative opening balance")
	}
	return &Ledger{
		Batch:   batch,
		Opening: opening,
		Account: Account{Available: opening},
	}, nil
}

// Balanced reports whether the current buckets still sum to the opening.
func (l *Ledger) Balanced() bool {
	return l.Account.Opening() == l.Opening
}
