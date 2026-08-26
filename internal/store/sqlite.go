package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"curtainwall.example/assembly-gate/internal/arbiter"
	"curtainwall.example/assembly-gate/internal/domain"
	"curtainwall.example/assembly-gate/internal/evidence"
	"curtainwall.example/assembly-gate/internal/film"
	"curtainwall.example/assembly-gate/internal/instrument"
	_ "modernc.org/sqlite"
)

// SQLiteStore is the production store backed by an embedded relational
// database. Every mutation runs in a transaction and leaves no partial side
// effect; unique constraints guard plate identity, raw-glass identity and
// idempotency operation ids; startup recovery reloads committed state and
// reconciles film conservation and lease expiry before serving.
type SQLiteStore struct {
	mu      sync.Mutex
	db      *sql.DB
	state   *state
	adapter instrument.Adapter
	path    string
}

// NewSQLite opens (or creates) the database at path, migrates the schema and
// recovers committed state. An empty path opens a private in-memory database.
func NewSQLite(path string, adapter instrument.Adapter) (*SQLiteStore, error) {
	dsn := path
	if dsn == "" {
		dsn = "file:assembly-gate?mode=memory&cache=shared"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite via modernc uses a single writer; one connection keeps the
	// transaction semantics deterministic.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	if err := ensureVersion(db); err != nil {
		return nil, err
	}
	st, err := loadState(db)
	if err != nil {
		return nil, err
	}
	if err := st.Recover(); err != nil {
		// An irrecoverable invariant break must surface before listening.
		return nil, err
	}
	if adapter == nil {
		adapter = instrument.NewPayloadAdapter()
	}
	return &SQLiteStore{db: db, state: st, adapter: adapter, path: path}, nil
}

// Close releases the database handle.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func ensureVersion(db *sql.DB) error {
	var v string
	err := db.QueryRow(`SELECT value FROM meta WHERE key = 'schema_version'`).Scan(&v)
	if err == sql.ErrNoRows {
		_, err = db.Exec(`INSERT INTO meta (key, value) VALUES ('schema_version', ?)`, schemaVersion)
		return err
	}
	if err != nil {
		return err
	}
	if v != schemaVersion {
		return fmt.Errorf("unsupported schema version %q (want %q)", v, schemaVersion)
	}
	return nil
}

// mutate stages a clone, applies fn, persists and commits only on success.
func (s *SQLiteStore) mutate(fn func(*state) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	clone := s.state.clone()
	if err := fn(clone); err != nil {
		return err
	}
	if err := s.persist(clone); err != nil {
		return err
	}
	s.state = clone
	return nil
}

func (s *SQLiteStore) read(fn func(*state) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn(s.state)
}

// ---- Store interface ----

func (s *SQLiteStore) LockDesign(snap domain.DesignSnapshot) (*Task, error) {
	var out *Task
	err := s.mutate(func(st *state) error {
		t, err := st.LockDesign(snap)
		if err != nil {
			return err
		}
		out = t
		return nil
	})
	return out, err
}

func (s *SQLiteStore) GetTask(id string) (*Task, error) {
	var out *Task
	err := s.read(func(st *state) error {
		t, err := st.getTask(id)
		if err != nil {
			return err
		}
		out = st.publicTask(t)
		return nil
	})
	return out, err
}

func (s *SQLiteStore) ListTasks() []*Task {
	var out []*Task
	_ = s.read(func(st *state) error {
		out = st.ListTasks()
		return nil
	})
	return out
}

func (s *SQLiteStore) Advance(taskID string, req OperationRequest) (*Task, error) {
	var out *Task
	err := s.mutate(func(st *state) error {
		t, err := st.Advance(taskID, req)
		if err != nil {
			return err
		}
		out = t
		return nil
	})
	return out, err
}

func (s *SQLiteStore) SubmitSamples(taskID string, req SampleRequest) (SamplesResult, error) {
	var out SamplesResult
	err := s.mutate(func(st *state) error {
		r, err := st.SubmitSamples(taskID, req)
		if err != nil {
			return err
		}
		out = r
		return nil
	})
	return out, err
}

func (s *SQLiteStore) SubmitInstrumentCall(taskID string, req InstrumentRequest) (InstrumentResult, error) {
	var out InstrumentResult
	err := s.mutate(func(st *state) error {
		r, err := st.SubmitInstrumentCall(taskID, req, s.adapter)
		if err != nil {
			return err
		}
		out = r
		return nil
	})
	return out, err
}

func (s *SQLiteStore) RunRetry(callID string) (InstrumentResult, error) {
	var out InstrumentResult
	err := s.mutate(func(st *state) error {
		r, err := st.RunRetry(callID, s.adapter)
		if err != nil {
			return err
		}
		out = r
		return nil
	})
	return out, err
}

func (s *SQLiteStore) RegisterAnomaly(taskID string, req AnomalyRequest) (*arbiter.RetestSet, error) {
	var out *arbiter.RetestSet
	err := s.mutate(func(st *state) error {
		set, err := st.RegisterAnomaly(taskID, req)
		if err != nil {
			return err
		}
		out = set
		return nil
	})
	return out, err
}

func (s *SQLiteStore) GetRetests(taskID string) (*arbiter.RetestSet, error) {
	var out *arbiter.RetestSet
	err := s.read(func(st *state) error {
		set, err := st.GetRetests(taskID)
		if err != nil {
			return err
		}
		out = set
		return nil
	})
	return out, err
}

func (s *SQLiteStore) AddReview(taskID string, req ReviewRequest) error {
	return s.mutate(func(st *state) error {
		return st.AddReview(taskID, req)
	})
}

func (s *SQLiteStore) SubmitVerdict(taskID string, req VerdictRequest) (*arbiter.VerdictResult, error) {
	var out *arbiter.VerdictResult
	err := s.mutate(func(st *state) error {
		r, err := st.SubmitVerdict(taskID, req)
		if err != nil {
			return err
		}
		out = r
		return nil
	})
	return out, err
}

func (s *SQLiteStore) GetCredential(id string) (*Credential, error) {
	var out *Credential
	err := s.read(func(st *state) error {
		c, err := st.GetCredential(id)
		if err != nil {
			return err
		}
		out = c
		return nil
	})
	return out, err
}

func (s *SQLiteStore) GetLineage(taskID string) (*domain.Lineage, error) {
	var out *domain.Lineage
	err := s.read(func(st *state) error {
		l, err := st.GetLineage(taskID)
		if err != nil {
			return err
		}
		out = l
		return nil
	})
	return out, err
}

func (s *SQLiteStore) GetCoverage(taskID string) (*evidence.CoverageMatrix, error) {
	var out *evidence.CoverageMatrix
	err := s.read(func(st *state) error {
		m, err := st.GetCoverage(taskID)
		if err != nil {
			return err
		}
		out = m
		return nil
	})
	return out, err
}

func (s *SQLiteStore) GetFilmLedger(batch string) (*film.Ledger, error) {
	var out *film.Ledger
	err := s.read(func(st *state) error {
		l, err := st.GetFilmLedger(batch)
		if err != nil {
			return err
		}
		out = l
		return nil
	})
	return out, err
}

func (s *SQLiteStore) PendingRetries() []*instrument.Call {
	var out []*instrument.Call
	_ = s.read(func(st *state) error {
		out = st.PendingRetries()
		return nil
	})
	return out
}

// ---- persistence ----

func (s *SQLiteStore) persist(st *state) error {
	p := st.toPersisted()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM tasks`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM raw_glass`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM films`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM leases`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM calls`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM retests`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM idempotency`); err != nil {
		return err
	}

	for _, pt := range p.Tasks {
		raw, _ := json.Marshal(pt)
		if _, err := tx.Exec(`INSERT INTO tasks (id, facade_zone, plate_number, payload) VALUES (?, ?, ?, ?)`,
			pt.Task.ID, pt.Task.Snapshot.FacadeZone, pt.Task.Snapshot.PlateNumber, string(raw)); err != nil {
			return mapUniqueViolation(err)
		}
		if pt.Task.Snapshot.FurnaceLot != "" {
			if _, err := tx.Exec(`INSERT INTO raw_glass (id, task_id) VALUES (?, ?)`,
				pt.Task.Snapshot.FurnaceLot, pt.Task.ID); err != nil {
				return mapUniqueViolation(err)
			}
		}
	}
	for _, pf := range p.Films {
		raw, _ := json.Marshal(pf)
		if _, err := tx.Exec(`INSERT INTO films (batch, payload) VALUES (?, ?)`,
			pf.Ledger.Batch, string(raw)); err != nil {
			return err
		}
	}
	for _, l := range p.Leases {
		raw, _ := json.Marshal(l)
		if _, err := tx.Exec(`INSERT INTO leases (payload) VALUES (?)`, string(raw)); err != nil {
			return err
		}
	}
	for _, c := range p.Calls {
		raw, _ := json.Marshal(c)
		if _, err := tx.Exec(`INSERT INTO calls (id, payload) VALUES (?, ?)`, c.ID, string(raw)); err != nil {
			return err
		}
	}
	for _, r := range p.Retests {
		raw, _ := json.Marshal(r.Set)
		if _, err := tx.Exec(`INSERT INTO retests (task_id, payload) VALUES (?, ?)`, r.TaskID, string(raw)); err != nil {
			return err
		}
	}
	for _, id := range p.Idem {
		raw, _ := json.Marshal(persistedIdem{OperationID: id.OperationID, RequestDigest: id.RequestDigest, Response: id.Response})
		if _, err := tx.Exec(`INSERT INTO idempotency (operation_id, payload) VALUES (?, ?)`, id.OperationID, string(raw)); err != nil {
			return mapUniqueViolation(err)
		}
	}
	if _, err := tx.Exec(`UPDATE meta SET value = ? WHERE key = 'schema_version'`, schemaVersion); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO meta (key, value) VALUES ('seq', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, fmt.Sprintf("%d", p.Seq)); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO meta (key, value) VALUES ('clock', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, fmt.Sprintf("%d", p.Clock)); err != nil {
		return err
	}
	return tx.Commit()
}

func mapUniqueViolation(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "tasks.facade_zone") || strings.Contains(msg, "raw_glass.id"):
		return domain.NewError(domain.CodeIdentityDuplicate, "duplicate identity rejected by database")
	case strings.Contains(msg, "idempotency.operation_id"):
		return domain.NewError(domain.CodeIdempotencyConflict, "duplicate operation id rejected by database")
	default:
		return err
	}
}
