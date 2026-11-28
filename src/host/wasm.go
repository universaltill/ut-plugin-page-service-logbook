//go:build wasip1

package host

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/universaltill/universal-till/sdk/plugin"
)

// WASM binds the ports to the real till through the official guest SDK
// (ABI 2): the plugin's own SQLite (db_exec / db_query, permission db:own,
// #3157 slice B), core read views and the plugin-records channel
// (records_forward and snapshots, permission records:sync, #3697). A host
// that lacks a capability answers a denied/invalid code; the domain code
// fails closed on any error.
type WASM struct{}

// Default is the host main binds to.
func Default() Host { return WASM{} }

func (WASM) DB() DB           { return wasmDB{} }
func (WASM) Views() Views     { return wasmViews{} }
func (WASM) Records() Records { return wasmRecords{} }
func (WASM) Clock() Clock     { return wasmClock{} }
func (WASM) IDs() IDs         { return wasmIDs{} }

type wasmDB struct{}

func (wasmDB) Exec(sql string, args ...any) (int64, error) { return plugin.DBExec(sql, args...) }

func (wasmDB) Query(sql string, args ...any) ([]Row, error) {
	raw, err := plugin.DBQuery(sql, args...)
	if err != nil {
		return nil, err
	}
	return decodeRows(raw)
}

type wasmRecords struct{}

func (wasmRecords) Forward(collection, id string, payload any) (bool, error) {
	return plugin.RecordsForward(collection, id, payload)
}

func (wasmRecords) Role() (string, error) { return plugin.RecordsRole() }

func (wasmRecords) Status(collection string) ([]RecordStatus, error) {
	rows, err := plugin.RecordsStatus(collection)
	if err != nil {
		return nil, err
	}
	out := make([]RecordStatus, len(rows))
	for i, r := range rows {
		out[i] = RecordStatus{ID: r.ID, Status: r.Status, Reason: r.Reason, Attempts: r.Attempts}
	}
	return out, nil
}

func (wasmRecords) SnapshotSet(collection string, v any) (bool, error) {
	return plugin.SnapshotSet(collection, v)
}

func (wasmRecords) SnapshotGet(collection string, out any) (bool, error) {
	return plugin.SnapshotGet(collection, out)
}

type wasmViews struct{}

func (wasmViews) Query(name string, args any) (json.RawMessage, error) {
	return plugin.ViewQuery(name, args)
}

type wasmClock struct{}

func (wasmClock) Now() time.Time { return time.Now().UTC() }

type wasmIDs struct{}

func (wasmIDs) NewID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}
