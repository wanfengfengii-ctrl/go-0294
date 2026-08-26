package store

import (
	"database/sql"
	"encoding/json"

	"curtainwall.example/assembly-gate/internal/instrument"
	"curtainwall.example/assembly-gate/internal/lease"
)

// loadState reads the committed aggregate graph from the relational tables and
// rebuilds the in-memory state. It is used once at startup; recovery then
// reconciles film conservation and closes expired leases before the listener
// opens.
func loadState(db *sql.DB) (*state, error) {
	p := persistedState{}

	if err := db.QueryRow(`SELECT COALESCE(value,'0') FROM meta WHERE key='seq'`).Scan(&p.Seq); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if err := db.QueryRow(`SELECT COALESCE(value,'0') FROM meta WHERE key='clock'`).Scan(&p.Clock); err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	rows, err := db.Query(`SELECT payload FROM tasks ORDER BY id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return nil, err
		}
		var pt persistedTask
		if err := json.Unmarshal([]byte(raw), &pt); err != nil {
			rows.Close()
			return nil, err
		}
		p.Tasks = append(p.Tasks, pt)
	}
	rows.Close()

	rows, err = db.Query(`SELECT payload FROM films ORDER BY batch`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return nil, err
		}
		var pf persistedFilm
		if err := json.Unmarshal([]byte(raw), &pf); err != nil {
			rows.Close()
			return nil, err
		}
		p.Films = append(p.Films, pf)
	}
	rows.Close()

	rows, err = db.Query(`SELECT payload FROM leases ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return nil, err
		}
		var l lease.Lease
		if err := json.Unmarshal([]byte(raw), &l); err != nil {
			rows.Close()
			return nil, err
		}
		p.Leases = append(p.Leases, l)
	}
	rows.Close()

	rows, err = db.Query(`SELECT payload FROM calls ORDER BY id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return nil, err
		}
		var c instrument.Call
		if err := json.Unmarshal([]byte(raw), &c); err != nil {
			rows.Close()
			return nil, err
		}
		p.Calls = append(p.Calls, c)
	}
	rows.Close()

	rows, err = db.Query(`SELECT task_id, payload FROM retests ORDER BY task_id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var taskID, raw string
		if err := rows.Scan(&taskID, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		var set retestRow
		if err := json.Unmarshal([]byte(raw), &set.Set); err != nil {
			rows.Close()
			return nil, err
		}
		set.TaskID = taskID
		p.Retests = append(p.Retests, set)
	}
	rows.Close()

	rows, err = db.Query(`SELECT operation_id, payload FROM idempotency ORDER BY operation_id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var op, raw string
		if err := rows.Scan(&op, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		var rec persistedIdem
		if err := json.Unmarshal([]byte(raw), &rec); err != nil {
			rows.Close()
			return nil, err
		}
		p.Idem = append(p.Idem, persistedIdem{OperationID: op, RequestDigest: rec.RequestDigest, Response: rec.Response})
	}
	rows.Close()

	return fromPersisted(p)
}
