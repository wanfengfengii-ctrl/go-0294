package store

// schemaVersion is bumped whenever the on-disk schema or serialization format
// changes; startup recovery refuses to open an incompatible database.
const schemaVersion = "1"

// schema is the embedded relational schema. Unique constraints on the plate
// identity, raw-glass identity and idempotency operation id provide the
// database-level guard against duplicates and idempotency races, mapped to
// stable business errors on conflict.
const schema = `
CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS tasks (
    id           TEXT PRIMARY KEY,
    facade_zone  TEXT NOT NULL,
    plate_number TEXT NOT NULL,
    payload      TEXT NOT NULL,
    UNIQUE (facade_zone, plate_number)
);
CREATE TABLE IF NOT EXISTS raw_glass (
    id      TEXT PRIMARY KEY,
    task_id TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS films (
    batch   TEXT PRIMARY KEY,
    payload TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS leases (
    seq     INTEGER PRIMARY KEY AUTOINCREMENT,
    payload TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS calls (
    id      TEXT PRIMARY KEY,
    payload TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS retests (
    task_id TEXT PRIMARY KEY,
    payload TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS idempotency (
    operation_id TEXT PRIMARY KEY,
    payload      TEXT NOT NULL
);
`
