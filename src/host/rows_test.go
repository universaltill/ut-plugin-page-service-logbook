package host

import (
	"reflect"
	"testing"
)

func TestDecodeRows(t *testing.T) {
	raw := []byte(`[{"id":"a","n":42,"f":1.5,"z":null,"big":9007199254740993,"b":{"b64":"AAEC/w=="}},{"id":"b","n":-7,"f":2,"z":null,"big":1,"b":{"b64":""}}]`)
	rows, err := decodeRows(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d", len(rows))
	}
	r := rows[0]
	if r["n"] != int64(42) || r["f"] != float64(1.5) || r["z"] != nil || r["id"] != "a" {
		t.Errorf("row0 = %#v", r)
	}
	if r["big"] != int64(9007199254740993) {
		t.Errorf("integers beyond 2^53 lost precision: %#v", r["big"])
	}
	if !reflect.DeepEqual(r["b"], []byte{0, 1, 2, 255}) {
		t.Errorf("blob = %#v", r["b"])
	}
	if rows[1]["n"] != int64(-7) {
		t.Errorf("row1 = %#v", rows[1])
	}
}

func TestDecodeRowsEmptyAndBad(t *testing.T) {
	rows, err := decodeRows([]byte(`[]`))
	if err != nil || len(rows) != 0 {
		t.Fatalf("empty: %v %v", rows, err)
	}
	if _, err := decodeRows([]byte(`{"x":1}`)); err == nil {
		t.Error("object instead of array must fail")
	}
	if _, err := decodeRows([]byte(`[{"n":`)); err == nil {
		t.Error("truncated must fail")
	}
}
