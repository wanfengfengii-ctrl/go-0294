// Package domain holds the stable domain types, error taxonomy, integer
// fixed-point arithmetic, micron geometry, process-stage ordering and the
// append-only material lineage model that underpin the curtain-wall
// laminated-glass assembly gate.
package domain

import (
	"fmt"
	"strings"
)

// ErrorCode is a stable, machine-readable classification for every business
// rejection the service can emit. It must never be changed once released.
type ErrorCode string

const (
	// CodeStructureMismatch marks a locked construction (thickness, edge
	// scheme, lamination stack) that does not match the request payload.
	CodeStructureMismatch ErrorCode = "STRUCTURE_MISMATCH"
	// CodeStaleDigest marks an execution request carrying a rule digest that
	// no longer matches the locked snapshot.
	CodeStaleDigest ErrorCode = "STALE_DIGEST"
	// CodeIdentityDuplicate marks a duplicate plate number or raw-glass
	// identity within the same facade zone or globally.
	CodeIdentityDuplicate ErrorCode = "IDENTITY_DUPLICATE"
	// CodeGeometryInvalid marks a non-degenerate, self-intersecting, negative
	// or out-of-boundary integer-micron outline, hole or notch.
	CodeGeometryInvalid ErrorCode = "GEOMETRY_INVALID"
	// CodeInsufficientArea marks a film-account balance too small for a
	// requested issue or cut in integer square millimetres.
	CodeInsufficientArea ErrorCode = "INSUFFICIENT_AREA"
	// CodeLeaseConflict marks a resource lease overlapping an existing lease.
	CodeLeaseConflict ErrorCode = "LEASE_CONFLICT"
	// CodeLeaseExpired marks an attempt to write evidence under an expired lease.
	CodeLeaseExpired ErrorCode = "LEASE_EXPIRED"
	// CodeStageOutOfOrder marks a process step advanced before its mandatory
	// predecessors are closed.
	CodeStageOutOfOrder ErrorCode = "STAGE_OUT_OF_ORDER"
	// CodeSampleGap marks a sample sequence that is out of order or missing a
	// contiguous prefix segment.
	CodeSampleGap ErrorCode = "SAMPLE_GAP"
	// CodeFixedOverflow marks an integer fixed-point computation whose
	// multiply-add overflows, divides by zero or regresses in time.
	CodeFixedOverflow ErrorCode = "FIXED_OVERFLOW"
	// CodeDeviceFailure marks a scripted instrument call that was rejected,
	// disconnected, timed out or malformed.
	CodeDeviceFailure ErrorCode = "DEVICE_FAILURE"
	// CodeRetestGenerationConflict marks a late receipt against a superseded
	// generation that can no longer affect the current conclusion.
	CodeRetestGenerationConflict ErrorCode = "RETEST_GENERATION_CONFLICT"
	// CodeIdempotencyConflict marks the same operation id carrying different
	// request content than its committed result.
	CodeIdempotencyConflict ErrorCode = "IDEMPOTENCY_CONFLICT"
	// CodeFinalExists marks an attempt to write a second final verdict after
	// the single-write barrier has already committed.
	CodeFinalExists ErrorCode = "FINAL_EXISTS"
)

// Error is the unified rejection structure shared by every API response. The
// Reasons slice is always deterministically sorted by the composite key
// (facade zone, plate, raw glass, furnace run, rack position, inspection
// grid, generation) before it is rendered.
type Error struct {
	Code        ErrorCode `json:"code"`
	Message     string    `json:"message"`
	Reasons     []string  `json:"reasons"`
	OperationID string    `json:"operation_id"`
}

// Error implements the builtin error interface.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// NewError builds a rejection with a normalized, deduplicated and sorted
// reason list. Reasons are sorted by the composite key (facade zone, plate,
// raw glass, furnace run, rack position, inspection grid, generation).
func NewError(code ErrorCode, message string, reasons ...string) *Error {
	dedup := make([]string, 0, len(reasons))
	for _, r := range reasons {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		dedup = append(dedup, r)
	}
	return &Error{Code: code, Message: message, Reasons: SortReasons(dedup)}
}

// WithOperation binds an idempotency operation id to the rejection without
// mutating the receiver.
func (e *Error) WithOperation(id string) *Error {
	if e == nil {
		return nil
	}
	out := *e
	out.OperationID = id
	return &out
}
