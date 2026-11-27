// Package host is the plugin's port layer: every capability the domain code
// needs from the till is an interface here, so src/store, src/views and
// src/app are tested natively (src/hosttest) before the plugin SQLite host
// functions (#3157 slice B) and the records channel SDK (#3697) exist. The
// only file that binds to the real host is wasm.go (wasip1).
package host

import (
	"encoding/json"
	"errors"
	"time"
)

// Row is one SQL result row: int64, string, float64 or nil values.
type Row map[string]any

// DB is the plugin's own SQLite database. One statement per call
// (transaction control is refused by the host, #3157 slice A).
type DB interface {
	Exec(sql string, args ...any) (rowsAffected int64, err error)
	Query(sql string, args ...any) ([]Row, error)
}

// Views reads core read views (ADR-0121 §5). Read-only; the plugin has no
// write path to core tables.
type Views interface {
	Query(name string, args any) (json.RawMessage, error)
}

// RecordStatus is one outbox row of this plugin as core reports it.
type RecordStatus struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Reason   string `json:"reason"`
	Attempts int    `json:"attempts"`
}

// Records is the core plugin-records channel (#3697, plugin-records.md).
type Records interface {
	// Forward queues a record; queued=false (no error) on a main till.
	Forward(collection, id string, payload any) (queued bool, err error)
	// Role is "replica" or "main".
	Role() (string, error)
	Status(collection string) ([]RecordStatus, error)
	// SnapshotSet stores a snapshot (main only; stored=false on a replica).
	SnapshotSet(collection string, v any) (stored bool, err error)
	SnapshotGet(collection string, out any) (found bool, err error)
}

// Clock and ID sources, so tests are deterministic.
type Clock interface{ Now() time.Time }

// IDs returns a new random UUID string.
type IDs interface{ NewID() string }

// Host is everything the domain code may touch.
type Host interface {
	DB() DB
	Views() Views
	Records() Records
	Clock() Clock
	IDs() IDs
}

// ErrUnavailable is returned by a capability the running till does not offer
// yet (e.g. DB before #3157 slice B). Callers fail closed.
var ErrUnavailable = errors.New("host capability unavailable")
