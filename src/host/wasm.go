//go:build wasip1

package host

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/universaltill/universal-till/sdk/plugin"
)

// WASM binds the ports to the real till. The plugin cannot run until #3157
// slice B (db_exec/db_query) and the #3697 records SDK exist: those two
// ports return ErrUnavailable, so every action fails closed instead of
// pretending to store data. Views are bound today (plugin.ViewQuery).
type WASM struct{}

// Default is the host main binds to.
func Default() Host { return WASM{} }

func (WASM) DB() DB           { return unavailableDB{} }
func (WASM) Views() Views     { return wasmViews{} }
func (WASM) Records() Records { return unavailableRecords{} }
func (WASM) Clock() Clock     { return wasmClock{} }
func (WASM) IDs() IDs         { return wasmIDs{} }

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
