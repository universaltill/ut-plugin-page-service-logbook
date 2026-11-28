package host

import "testing"

// The till's WASI random source is deterministic (wazero default), so every
// event instance draws the same bytes. IDs must still differ across events:
// the wall clock and a sequence are mixed in.
func TestMixIDDiffersWhenOnlyTheClockOrSequenceDoes(t *testing.T) {
	var r [16]byte // the same "random" bytes every time
	seen := map[string]bool{}
	for _, c := range []struct {
		nanos int64
		seq   uint64
	}{{1, 1}, {2, 1}, {1, 2}, {1700000000000000001, 1}, {1700000000000000002, 1}} {
		id := mixID(r, c.nanos, c.seq)
		if seen[id] {
			t.Fatalf("duplicate id %s for %+v", id, c)
		}
		seen[id] = true
		if len(id) != 36 || id[14] != '4' {
			t.Fatalf("not a v4-shaped uuid: %s", id)
		}
	}
	if mixID(r, 5, 5) != mixID(r, 5, 5) {
		t.Fatal("mixID must be a pure function of its inputs")
	}
}
