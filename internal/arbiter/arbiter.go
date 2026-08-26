// Package arbiter implements the single-write final barrier and dual-review
// gate. Two distinct, qualified reviewers must independently approve the
// current generation before a verdict may be proposed; the barrier accepts
// exactly one of admit / isolate / cancel and rejects every later write with
// a stable CodeFinalExists error. Admission additionally mints a unique,
// verifiable credential.
package arbiter

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"curtainwall.example/assembly-gate/internal/domain"
)

// Verdict is the terminal outcome of the assembly-admission gate.
type Verdict int

const (
	VerdictAdmit Verdict = iota
	VerdictIsolate
	VerdictCancel
)

// String returns a stable wire name for the verdict.
func (v Verdict) String() string {
	switch v {
	case VerdictAdmit:
		return "admit"
	case VerdictIsolate:
		return "isolate"
	case VerdictCancel:
		return "cancel"
	default:
		return "unknown"
	}
}

// Review is an independent reviewer attestation for a generation.
type Review struct {
	Reviewer   string `json:"reviewer"`
	Qualified  bool   `json:"qualified"`
	Generation int    `json:"generation"`
}

// Reviews satisfies the dual-review gate: two distinct qualified reviewers for
// the given generation.
func Reviews(rs []Review, generation int) bool {
	seen := map[string]bool{}
	qualified := 0
	for _, r := range rs {
		if r.Generation != generation || !r.Qualified {
			continue
		}
		if r.Reviewer == "" || seen[r.Reviewer] {
			continue
		}
		seen[r.Reviewer] = true
		qualified++
	}
	return qualified >= 2
}

// FinalBarrier is the single-write terminal gate. Once Decided is true, no
// further decision may be committed.
type FinalBarrier struct {
	Decided    bool    `json:"decided"`
	Verdict    Verdict `json:"verdict"`
	Credential string  `json:"credential,omitempty"`
}

// Decide commits a verdict, returning CodeFinalExists if a verdict already
// exists. Only admission mints a credential.
func (b *FinalBarrier) Decide(v Verdict, taskID string, generation int) error {
	if b.Decided {
		return domain.NewError(domain.CodeFinalExists, "final verdict already committed", b.Verdict.String())
	}
	if v < VerdictAdmit || v > VerdictCancel {
		return domain.NewError(domain.CodeFinalExists, "unknown verdict")
	}
	b.Decided = true
	b.Verdict = v
	if v == VerdictAdmit {
		b.Credential = mintCredential(taskID, generation)
	}
	return nil
}

func mintCredential(taskID string, generation int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", taskID, generation)))
	return "CRED-" + hex.EncodeToString(sum[:12])
}
