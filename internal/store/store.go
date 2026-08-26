// Package store is the transactional persistence and orchestration boundary
// for the assembly gate. It wires the design catalog, lineage, film ledger,
// lease registry, evidence recorder, instrument adapter and final arbiter into
// a single atomic unit of work: every mutation either commits in full or rolls
// back leaving no partial side effect. The production implementation is backed
// by an embedded relational database with unique constraints; a deterministic
// in-memory implementation shares the same business logic for tests.
package store

import (
	"curtainwall.example/assembly-gate/internal/arbiter"
	"curtainwall.example/assembly-gate/internal/domain"
	"curtainwall.example/assembly-gate/internal/evidence"
	"curtainwall.example/assembly-gate/internal/film"
	"curtainwall.example/assembly-gate/internal/instrument"
)

// Task is the public aggregate for one locked manufacturing task.
type Task struct {
	ID           string                `json:"id"`
	Snapshot     domain.DesignSnapshot `json:"snapshot"`
	Generation   int                   `json:"generation"`
	Completed    []string              `json:"completed"`
	Lineage      *domain.Lineage       `json:"lineage"`
	Measurements []domain.Measurement  `json:"measurements"`
	Reviews      []arbiter.Review      `json:"reviews"`
	Verdict      string                `json:"verdict,omitempty"`
	Credential   string                `json:"credential,omitempty"`
}

// OperationRequest advances one manufacturing stage, optionally carrying a
// film entry and/or a resource lease that must commit atomically with the
// stage evidence.
type OperationRequest struct {
	OperationID string            `json:"operation_id"`
	RuleDigest  string            `json:"rule_digest"`
	Generation  int               `json:"generation"`
	LogicalTime int64             `json:"logical_time"`
	Operator    string            `json:"operator"`
	Stage       string            `json:"stage"`
	ResourceKey string            `json:"resource_key,omitempty"`
	LeaseStart  int64             `json:"lease_start,omitempty"`
	LeaseEnd    int64             `json:"lease_end,omitempty"`
	FilmEntry   *FilmEntryRequest `json:"film_entry,omitempty"`
}

// FilmEntryRequest is a wire-safe film double-entry with a string kind.
type FilmEntryRequest struct {
	Kind      string `json:"kind"`
	AmountUM2 int64  `json:"amount_um2"`
}

// SampleRequest submits a strictly ordered heat-soak or autoclave sample batch.
type SampleRequest struct {
	Stage      string                 `json:"stage"` // heat_soak | autoclave
	RuleDigest string                 `json:"rule_digest"`
	Generation int                    `json:"generation"`
	Samples    []evidence.SamplePoint `json:"samples"`
}

// SamplesResult is the deterministic outcome of a sample submission.
type SamplesResult struct {
	Coverage     *evidence.CoverageMatrix  `json:"coverage,omitempty"`
	Autoclave    *evidence.AutoclaveResult `json:"autoclave,omitempty"`
	FullyCovered bool                      `json:"fully_covered"`
}

// InstrumentRequest triggers a scripted device call for a stress, optical or
// destructive reading.
type InstrumentRequest struct {
	Device      instrument.Device `json:"device"`
	Payload     string            `json:"payload"`
	RuleDigest  string            `json:"rule_digest"`
	Generation  int               `json:"generation"`
	LogicalTime int64             `json:"logical_time"`
	Operator    string            `json:"operator"`
}

// InstrumentResult is the outcome of an instrument call or retry run.
type InstrumentResult struct {
	Call *instrument.Call `json:"call"`
	Task *Task            `json:"task,omitempty"`
}

// AnomalyRequest registers an anomaly that expands into a retest scope.
type AnomalyRequest struct {
	Kind       arbiter.AnomalyKind `json:"kind"`
	RackPos    string              `json:"rack_position,omitempty"`
	Generation int                 `json:"generation"`
	RuleDigest string              `json:"rule_digest"`
}

// ReviewRequest submits one independent reviewer attestation.
type ReviewRequest struct {
	Reviewer   string `json:"reviewer"`
	Qualified  bool   `json:"qualified"`
	Generation int    `json:"generation"`
}

// VerdictRequest proposes a terminal admit / isolate / cancel verdict.
type VerdictRequest struct {
	Verdict    string `json:"verdict"`
	Generation int    `json:"generation"`
}

// Credential is a verifiable admission credential minted only on admission.
type Credential struct {
	ID         string `json:"id"`
	TaskID     string `json:"task_id"`
	Generation int    `json:"generation"`
	Value      string `json:"value"`
}

// Store is the transactional boundary consumed by the HTTP API.
type Store interface {
	LockDesign(snapshot domain.DesignSnapshot) (*Task, error)
	GetTask(id string) (*Task, error)
	ListTasks() []*Task
	Advance(taskID string, req OperationRequest) (*Task, error)
	SubmitSamples(taskID string, req SampleRequest) (SamplesResult, error)
	SubmitInstrumentCall(taskID string, req InstrumentRequest) (InstrumentResult, error)
	RunRetry(callID string) (InstrumentResult, error)
	RegisterAnomaly(taskID string, req AnomalyRequest) (*arbiter.RetestSet, error)
	GetRetests(taskID string) (*arbiter.RetestSet, error)
	AddReview(taskID string, req ReviewRequest) error
	SubmitVerdict(taskID string, req VerdictRequest) (*arbiter.VerdictResult, error)
	GetCredential(id string) (*Credential, error)
	GetLineage(taskID string) (*domain.Lineage, error)
	GetCoverage(taskID string) (*evidence.CoverageMatrix, error)
	GetFilmLedger(batch string) (*film.Ledger, error)
	PendingRetries() []*instrument.Call
	Close() error
}

// ErrNotFound is returned for an unknown task, credential or batch identity.
var ErrNotFound = domain.NewError("NOT_FOUND", "resource not found")
