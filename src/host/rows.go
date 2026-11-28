package host

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// decodeRows decodes the host's db_query answer (a JSON array of objects)
// into Rows: integers stay int64 (no float64 round trip, so amounts in minor
// units never lose precision), other numbers are float64, a BLOB
// {"b64":"..."} becomes []byte.
func decodeRows(raw []byte) ([]Row, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var in []map[string]any
	if err := dec.Decode(&in); err != nil {
		return nil, fmt.Errorf("db_query: decode: %w", err)
	}
	out := make([]Row, 0, len(in))
	for _, m := range in {
		r := make(Row, len(m))
		for k, v := range m {
			cv, err := convertValue(v)
			if err != nil {
				return nil, fmt.Errorf("db_query: column %q: %w", k, err)
			}
			r[k] = cv
		}
		out = append(out, r)
	}
	return out, nil
}

func convertValue(v any) (any, error) {
	switch x := v.(type) {
	case json.Number:
		if !strings.ContainsAny(string(x), ".eE") {
			if i, err := x.Int64(); err == nil {
				return i, nil
			}
		}
		return x.Float64()
	case map[string]any:
		if s, ok := x["b64"].(string); ok && len(x) == 1 {
			return base64.StdEncoding.DecodeString(s)
		}
		return nil, fmt.Errorf("unexpected object value")
	default: // nil, string, bool
		return v, nil
	}
}
