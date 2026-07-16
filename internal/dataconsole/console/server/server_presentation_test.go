package server

import (
	"net/http"
	"testing"
)

// This file pins S14's presentation-contract wiring at the HTTP layer:
// BlobMeta.Vector (finding 5, UI-AUD-03) reaching the client as the
// X-DataConsole-Vector response header, mirroring how X-DataConsole-Truncated
// already carries BlobMeta.Truncated (server_valuefidelity_test.go).

// TestHandleBlob_Read_Vector_SignalsHeader pins the qdrant vector-collapse
// signal: BlobMeta.Vector=true must reach the client as
// X-DataConsole-Vector: true so the SPA (S15) can key on it to collapse a raw
// vector[] array instead of rendering a wall of floats inline.
func TestHandleBlob_Read_Vector_SignalsHeader(t *testing.T) {
	t.Parallel()
	ts, tok, fake := newTestServerWithFake(t, true)
	defer ts.Close()
	fake.blobs["point.json"] = []byte(`{"id":1,"vector":[0.1,0.2,0.3,0.4]}`)
	fake.vectorKeys = map[string]bool{"point.json": true}

	r := do(t, ts, "GET", `/api/blob?service=store&segs=%5B%22point.json%22%5D`, tok, "", false)
	if r.code != http.StatusOK {
		t.Fatalf("GET = %d, want 200", r.code)
	}
	if got := r.header.Get("X-DataConsole-Vector"); got != "true" {
		t.Fatalf("X-DataConsole-Vector = %q, want true", got)
	}
}

// TestHandleBlob_Read_NonVector_HeaderFalse is the companion GREEN case: an
// ordinary blob must carry the header as an explicit "false", not an absent
// header a client would need to default itself.
func TestHandleBlob_Read_NonVector_HeaderFalse(t *testing.T) {
	t.Parallel()
	ts, tok, fake := newTestServerWithFake(t, true)
	defer ts.Close()
	fake.blobs["plain.txt"] = []byte("hello")

	r := do(t, ts, "GET", `/api/blob?service=store&segs=%5B%22plain.txt%22%5D`, tok, "", false)
	if r.code != http.StatusOK {
		t.Fatalf("GET = %d, want 200", r.code)
	}
	if got := r.header.Get("X-DataConsole-Vector"); got != "false" {
		t.Fatalf("X-DataConsole-Vector = %q, want false", got)
	}
}
