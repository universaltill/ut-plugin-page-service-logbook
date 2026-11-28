package host

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
)

// Fail-closed ports: the capability does not exist on this till yet.

type unavailableDB struct{}

func (unavailableDB) Exec(string, ...any) (int64, error)  { return 0, ErrUnavailable }
func (unavailableDB) Query(string, ...any) ([]Row, error) { return nil, ErrUnavailable }

type unavailableRecords struct{}

func (unavailableRecords) Forward(string, string, any) (bool, error) { return false, ErrUnavailable }
func (unavailableRecords) Role() (string, error)                     { return "", ErrUnavailable }
func (unavailableRecords) Status(string) ([]RecordStatus, error)     { return nil, ErrUnavailable }
func (unavailableRecords) SnapshotSet(string, any) (bool, error)     { return false, ErrUnavailable }
func (unavailableRecords) SnapshotGet(string, any) (bool, error)     { return false, ErrUnavailable }

// mixID builds a UUID-shaped id from random bytes, the wall clock (ns) and a
// per-instance sequence. The till's WASI random_get is deterministic (wazero's
// default source: every event instance draws the same bytes), so random bytes
// alone repeat across events and the second staff member or entry would fail
// on the primary key. The clock differs per event; the sequence per call.
func mixID(rnd [16]byte, nanos int64, seq uint64) string {
	h := sha256.New()
	h.Write(rnd[:])
	var n [16]byte
	binary.BigEndian.PutUint64(n[:8], uint64(nanos))
	binary.BigEndian.PutUint64(n[8:], seq)
	h.Write(n[:])
	b := h.Sum(nil)[:16]
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	x := hex.EncodeToString(b)
	return x[0:8] + "-" + x[8:12] + "-" + x[12:16] + "-" + x[16:20] + "-" + x[20:]
}
