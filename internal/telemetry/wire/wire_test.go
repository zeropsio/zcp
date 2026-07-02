package wire_test

import (
	"encoding/json"
	"testing"

	"github.com/zeropsio/zcp/internal/telemetry/wire"
)

// TestResponse_JSONTags pins the wire.Response envelope's field names (spec
// §4.4) — internal/ingest's IngestResponse is a type alias of this struct,
// so a drift here would silently change the response both the ingest and
// the client rely on.
func TestResponse_JSONTags(t *testing.T) {
	t.Parallel()

	resp := wire.Response{
		Accepted:         1,
		Rejected:         2,
		Duplicate:        3,
		RetryAfterMs:     4,
		MaxSchemaVersion: 5,
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	want := map[string]float64{
		"accepted":           1,
		"rejected":           2,
		"duplicate":          3,
		"retry_after_ms":     4,
		"max_schema_version": 5,
	}
	for key, wantVal := range want {
		gotVal, ok := got[key]
		if !ok {
			t.Errorf("response JSON missing key %q: %s", key, b)
			continue
		}
		if gotVal != wantVal {
			t.Errorf("response[%q] = %v, want %v", key, gotVal, wantVal)
		}
	}
	if len(got) != len(want) {
		t.Errorf("response JSON has %d keys, want %d: %s", len(got), len(want), b)
	}
}
