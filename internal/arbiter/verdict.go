package arbiter

import (
	"curtainwall.example/assembly-gate/internal/domain"
)

// ClosureRequirements captures everything the final barrier must verify before
// accepting an admit / isolate / cancel verdict.
type ClosureRequirements struct {
	// LineageComplete reports whether the current generation's raw -> tempered
	// -> laminated lineage is fully present.
	LineageComplete bool
	// FilmConserved reports whether every film account still balances.
	FilmConserved bool
	// AllStagesClosed reports whether every manufacturing stage up to retest is
	// complete for the current generation.
	AllStagesClosed bool
	// MetricsPass reports whether stress and appearance thresholds are met.
	MetricsPass bool
	// DestructivePass reports whether the sampled destructive test passed.
	DestructivePass bool
	// RetestComplete reports whether the retest scope has been fully closed.
	RetestComplete bool
	// Reviews is the independent review list for the current generation.
	Reviews []Review
	// Generation is the generation being arbitrated.
	Generation int
}

// VerdictResult is the terminal outcome returned to the caller.
type VerdictResult struct {
	Verdict    Verdict `json:"verdict"`
	Credential string  `json:"credential,omitempty"`
}

// CheckClosure verifies every closure condition and the dual-review gate,
// returning the first stable rejection encountered.
func CheckClosure(req ClosureRequirements) error {
	if !req.LineageComplete {
		return domain.NewError(domain.CodeStageOutOfOrder, "lineage incomplete for current generation")
	}
	if !req.FilmConserved {
		return domain.NewError(domain.CodeInsufficientArea, "film conservation not closed")
	}
	if !req.AllStagesClosed {
		return domain.NewError(domain.CodeStageOutOfOrder, "not all process stages closed")
	}
	if !req.MetricsPass {
		return domain.NewError(domain.CodeStageOutOfOrder, "stress or appearance thresholds not met")
	}
	if !req.DestructivePass {
		return domain.NewError(domain.CodeStageOutOfOrder, "destructive test not passed")
	}
	if !req.RetestComplete {
		return domain.NewError(domain.CodeStageOutOfOrder, "anomaly retest not complete")
	}
	if !Reviews(req.Reviews, req.Generation) {
		return domain.NewError(domain.CodeStageOutOfOrder, "two distinct qualified reviewers required")
	}
	return nil
}
