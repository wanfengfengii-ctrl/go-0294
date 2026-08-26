package store

import (
	"curtainwall.example/assembly-gate/internal/arbiter"
	"curtainwall.example/assembly-gate/internal/domain"
	"curtainwall.example/assembly-gate/internal/evidence"
	"curtainwall.example/assembly-gate/internal/film"
	"curtainwall.example/assembly-gate/internal/instrument"
	"curtainwall.example/assembly-gate/internal/lease"
)

// persistedTask is the on-disk form of a task plus its non-serialized internal
// fields (process prefix, sample history, final barrier and closure flags).
type persistedTask struct {
	Task            Task                   `json:"task"`
	Prefix          domain.Prefix          `json:"prefix"`
	HeatSamples     []evidence.SamplePoint `json:"heat_samples"`
	AutoSamples     []evidence.SamplePoint `json:"auto_samples"`
	Barrier         arbiter.FinalBarrier   `json:"barrier"`
	RetestClosed    bool                   `json:"retest_closed"`
	DestructivePass bool                   `json:"destructive_pass"`
	RetestGen       int                    `json:"retest_gen"`
	Retest          *arbiter.RetestSet     `json:"retest,omitempty"`
}

// persistedFilm is the on-disk form of a film batch account and its entries.
type persistedFilm struct {
	Ledger  film.Ledger  `json:"ledger"`
	Entries []film.Entry `json:"entries"`
}

// persistedIdem is the on-disk form of an idempotency record.
type persistedIdem struct {
	OperationID   string `json:"operation_id"`
	RequestDigest string `json:"request_digest"`
	Response      string `json:"response"`
}

// persistedState is the fully serializable snapshot of the aggregate graph.
type persistedState struct {
	Tasks   []persistedTask   `json:"tasks"`
	Films   []persistedFilm   `json:"films"`
	Leases  []lease.Lease     `json:"leases"`
	Calls   []instrument.Call `json:"calls"`
	Retests []retestRow       `json:"retests"`
	Idem    []persistedIdem   `json:"idem"`
	Seq     int64             `json:"seq"`
	Clock   int64             `json:"clock"`
}

type retestRow struct {
	TaskID string            `json:"task_id"`
	Set    arbiter.RetestSet `json:"set"`
}

// toPersisted converts the in-memory state to its serializable form.
func (s *state) toPersisted() persistedState {
	p := persistedState{Clock: s.clock}
	for _, t := range s.tasks {
		pt := persistedTask{
			Task:            *s.publicTask(t),
			Prefix:          s.prefixes[t.ID],
			HeatSamples:     s.heatSamples[t.ID],
			AutoSamples:     s.autoSamples[t.ID],
			RetestClosed:    s.retestClosed[t.ID],
			DestructivePass: s.destructivePass[t.ID],
			RetestGen:       s.retestGen[t.ID],
		}
		if b, ok := s.barriers[t.ID]; ok {
			pt.Barrier = *b
		}
		pt.Retest = s.retests[t.ID]
		p.Tasks = append(p.Tasks, pt)
	}
	for _, b := range s.film.Batches() {
		l := s.film.Ledger(b)
		if l == nil {
			continue
		}
		entries := s.film.Entries(b)
		if entries == nil {
			entries = []film.Entry{}
		}
		p.Films = append(p.Films, persistedFilm{Ledger: *l, Entries: entries})
	}
	p.Leases = s.leases.AllLeases()
	for _, c := range s.calls {
		p.Calls = append(p.Calls, *c)
	}
	for taskID, set := range s.retests {
		if set != nil {
			p.Retests = append(p.Retests, retestRow{TaskID: taskID, Set: *set})
		}
	}
	for opID, rec := range s.idem {
		p.Idem = append(p.Idem, persistedIdem{
			OperationID: opID, RequestDigest: rec.RequestDigest, Response: rec.Response,
		})
	}
	return p
}

// fromPersisted rebuilds the in-memory state from its serializable form.
func fromPersisted(p persistedState) (*state, error) {
	s := newState()
	s.seq = p.Seq
	s.clock = p.Clock
	for _, pt := range p.Tasks {
		t := pt.Task
		s.tasks = append(s.tasks, &t)
		s.byID[t.ID] = &t
		s.prefixes[t.ID] = pt.Prefix
		s.heatSamples[t.ID] = pt.HeatSamples
		s.autoSamples[t.ID] = pt.AutoSamples
		s.barriers[t.ID] = &arbiter.FinalBarrier{Decided: pt.Barrier.Decided, Verdict: pt.Barrier.Verdict, Credential: pt.Barrier.Credential}
		s.retestClosed[t.ID] = pt.RetestClosed
		s.destructivePass[t.ID] = pt.DestructivePass
		s.retestGen[t.ID] = pt.RetestGen
		if pt.Retest != nil {
			s.retests[t.ID] = pt.Retest
		}
		// Rebuild the catalog identity and digest state from the snapshot.
		s.catalog.Restore(t.Snapshot)
	}
	for _, pf := range p.Films {
		s.film.Restore(pf.Ledger, pf.Entries)
	}
	for _, l := range p.Leases {
		s.leases.Restore(l)
	}
	for _, c := range p.Calls {
		cp := c
		s.calls[cp.ID] = &cp
	}
	for _, r := range p.Retests {
		cp := r.Set
		s.retests[r.TaskID] = &cp
	}
	for _, id := range p.Idem {
		s.idem[id.OperationID] = idemRecord{RequestDigest: id.RequestDigest, Response: id.Response}
	}
	return s, nil
}
