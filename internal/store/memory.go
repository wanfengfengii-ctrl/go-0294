package store

import (
	"sync"

	"curtainwall.example/assembly-gate/internal/arbiter"
	"curtainwall.example/assembly-gate/internal/domain"
	"curtainwall.example/assembly-gate/internal/evidence"
	"curtainwall.example/assembly-gate/internal/film"
	"curtainwall.example/assembly-gate/internal/instrument"
)

// Memory is a deterministic in-memory Store sharing the same business logic as
// the SQLite store. It is used by tests and the HTTP API test double.
type Memory struct {
	mu      sync.Mutex
	state   *state
	adapter instrument.Adapter
}

// NewMemory returns an empty in-memory store with the payload-driven adapter.
func NewMemory() *Memory {
	return &Memory{state: newState(), adapter: instrument.NewPayloadAdapter()}
}

// NewMemoryWithAdapter returns an in-memory store using the given scripted
// adapter, for deterministic instrument-failure tests.
func NewMemoryWithAdapter(adapter instrument.Adapter) *Memory {
	return &Memory{state: newState(), adapter: adapter}
}

func (m *Memory) mutate(fn func(*state) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return fn(m.state)
}

func (m *Memory) read(fn func(*state) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return fn(m.state)
}

func (m *Memory) LockDesign(snap domain.DesignSnapshot) (*Task, error) {
	var out *Task
	err := m.mutate(func(st *state) error {
		t, err := st.LockDesign(snap)
		if err != nil {
			return err
		}
		out = t
		return nil
	})
	return out, err
}

func (m *Memory) GetTask(id string) (*Task, error) {
	var out *Task
	err := m.read(func(st *state) error {
		t, err := st.getTask(id)
		if err != nil {
			return err
		}
		out = st.publicTask(t)
		return nil
	})
	return out, err
}

func (m *Memory) ListTasks() []*Task {
	var out []*Task
	_ = m.read(func(st *state) error {
		out = st.ListTasks()
		return nil
	})
	return out
}

func (m *Memory) Advance(taskID string, req OperationRequest) (*Task, error) {
	var out *Task
	err := m.mutate(func(st *state) error {
		t, err := st.Advance(taskID, req)
		if err != nil {
			return err
		}
		out = t
		return nil
	})
	return out, err
}

func (m *Memory) SubmitSamples(taskID string, req SampleRequest) (SamplesResult, error) {
	var out SamplesResult
	err := m.mutate(func(st *state) error {
		r, err := st.SubmitSamples(taskID, req)
		if err != nil {
			return err
		}
		out = r
		return nil
	})
	return out, err
}

func (m *Memory) SubmitInstrumentCall(taskID string, req InstrumentRequest) (InstrumentResult, error) {
	var out InstrumentResult
	err := m.mutate(func(st *state) error {
		r, err := st.SubmitInstrumentCall(taskID, req, m.adapter)
		if err != nil {
			return err
		}
		out = r
		return nil
	})
	return out, err
}

func (m *Memory) RunRetry(callID string) (InstrumentResult, error) {
	var out InstrumentResult
	err := m.mutate(func(st *state) error {
		r, err := st.RunRetry(callID, m.adapter)
		if err != nil {
			return err
		}
		out = r
		return nil
	})
	return out, err
}

func (m *Memory) RegisterAnomaly(taskID string, req AnomalyRequest) (*arbiter.RetestSet, error) {
	var out *arbiter.RetestSet
	err := m.mutate(func(st *state) error {
		set, err := st.RegisterAnomaly(taskID, req)
		if err != nil {
			return err
		}
		out = set
		return nil
	})
	return out, err
}

func (m *Memory) GetRetests(taskID string) (*arbiter.RetestSet, error) {
	var out *arbiter.RetestSet
	err := m.read(func(st *state) error {
		set, err := st.GetRetests(taskID)
		if err != nil {
			return err
		}
		out = set
		return nil
	})
	return out, err
}

func (m *Memory) AddReview(taskID string, req ReviewRequest) error {
	return m.mutate(func(st *state) error {
		return st.AddReview(taskID, req)
	})
}

func (m *Memory) SubmitVerdict(taskID string, req VerdictRequest) (*arbiter.VerdictResult, error) {
	var out *arbiter.VerdictResult
	err := m.mutate(func(st *state) error {
		r, err := st.SubmitVerdict(taskID, req)
		if err != nil {
			return err
		}
		out = r
		return nil
	})
	return out, err
}

func (m *Memory) GetCredential(id string) (*Credential, error) {
	var out *Credential
	err := m.read(func(st *state) error {
		c, err := st.GetCredential(id)
		if err != nil {
			return err
		}
		out = c
		return nil
	})
	return out, err
}

func (m *Memory) GetLineage(taskID string) (*domain.Lineage, error) {
	var out *domain.Lineage
	err := m.read(func(st *state) error {
		l, err := st.GetLineage(taskID)
		if err != nil {
			return err
		}
		out = l
		return nil
	})
	return out, err
}

func (m *Memory) GetCoverage(taskID string) (*evidence.CoverageMatrix, error) {
	var out *evidence.CoverageMatrix
	err := m.read(func(st *state) error {
		mm, err := st.GetCoverage(taskID)
		if err != nil {
			return err
		}
		out = mm
		return nil
	})
	return out, err
}

func (m *Memory) GetFilmLedger(batch string) (*film.Ledger, error) {
	var out *film.Ledger
	err := m.read(func(st *state) error {
		l, err := st.GetFilmLedger(batch)
		if err != nil {
			return err
		}
		out = l
		return nil
	})
	return out, err
}

func (m *Memory) PendingRetries() []*instrument.Call {
	var out []*instrument.Call
	_ = m.read(func(st *state) error {
		out = st.PendingRetries()
		return nil
	})
	return out
}

func (m *Memory) Close() error { return nil }
