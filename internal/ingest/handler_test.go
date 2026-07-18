package ingest

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/telemetry/wire"
)

// --- test fixtures -----------------------------------------------------

// testSessionID is the fixed session_id used by every fixture event in
// this file — tests that need a distinct session/install identity build
// their own wire.Event literal instead of overloading validEvent.
const testSessionID = "9d3e8f1a-4b2c-4a5d-9e6f-1234567890ac"

func validEvent(seq uint64) wire.Event {
	return wire.Event{
		SchemaVersion: wire.SchemaVersion,
		EventID:       fmt.Sprintf("%s:%d", testSessionID, seq),
		InstallID:     "9d3e8f1a-4b2c-4a5d-9e6f-1234567890ab",
		SessionID:     testSessionID,
		Seq:           seq,
		EventType:     wire.EventToolCall,
		ClientTime:    time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00"),
		ZcpVersion:    "v6.31.0",
		OS:            "darwin",
		Arch:          "arm64",
		RuntimeEnv:    wire.RuntimeLocal,
		UsageChannel:  wire.ChannelExternal,
		Tool:          "zerops_deploy",
		Action:        "deploy",
		DurationMs:    100,
		Success:       true,
	}
}

func validBatch(events ...wire.Event) wire.Batch {
	return wire.Batch{
		ProtocolVersion: wire.ProtocolVersion,
		BatchID:         "9d3e8f1a-4b2c-4a5d-9e6f-1234567890ad",
		SentAt:          time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00"),
		Events:          events,
	}
}

func newTestHandler(t *testing.T) (*Handler, *fakeInserter) {
	t.Helper()
	fi := &fakeInserter{}
	b := newBatcher(fi, testLogger(), 5000, time.Hour) // never auto-flushes on size/interval during tests
	h := NewHandler(newLimiter(), newDedup(1000, 24*time.Hour), b, nil, newBlocklist(nil, nil), testLogger())
	go b.run()
	t.Cleanup(func() { _ = b.stop(context.Background()) })
	return h, fi
}

func postJSON(t *testing.T, h *Handler, body []byte, gzipEncode bool, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if gzipEncode {
		gw := gzip.NewWriter(&buf)
		if _, err := gw.Write(body); err != nil {
			t.Fatalf("gzip write: %v", err)
		}
		if err := gw.Close(); err != nil {
			t.Fatalf("gzip close: %v", err)
		}
	} else {
		buf.Write(body)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/events", &buf)
	if gzipEncode {
		req.Header.Set("Content-Encoding", "gzip")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	h.Routes().ServeHTTP(rr, req)
	return rr
}

func decodeResponse(t *testing.T, rr *httptest.ResponseRecorder) IngestResponse {
	t.Helper()
	var resp IngestResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response %q: %v", rr.Body.String(), err)
	}
	return resp
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %T: %v", v, err)
	}
	return b
}

// --- happy path ----------------------------------------------------------

func TestHandler_ValidBatch_Accepted(t *testing.T) {
	t.Parallel()

	h, fi := newTestHandler(t)
	batch := validBatch(validEvent(1))
	body := mustMarshal(t, batch)

	rr := postJSON(t, h, body, true, map[string]string{"Content-Type": "application/json"})
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202, body=%s", rr.Code, rr.Body.String())
	}
	resp := decodeResponse(t, rr)
	if resp.Accepted != 1 || resp.Rejected != 0 || resp.Duplicate != 0 {
		t.Errorf("resp = %+v, want accepted=1 rejected=0 duplicate=0", resp)
	}

	_ = h.batch.stop(context.Background())
	batches, _ := fi.snapshot()
	total := 0
	for _, b := range batches {
		total += len(b)
	}
	if total != 1 {
		t.Errorf("rows delivered to inserter = %d, want 1", total)
	}
}

func TestHandler_NonGzipBody_StillAccepted(t *testing.T) {
	t.Parallel()

	h, _ := newTestHandler(t)
	batch := validBatch(validEvent(1))
	body := mustMarshal(t, batch)

	rr := postJSON(t, h, body, false, nil)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202, body=%s", rr.Code, rr.Body.String())
	}
}

func TestHandler_Healthz_Returns200(t *testing.T) {
	t.Parallel()

	h, _ := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	h.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

// --- T8: ingest rejects invalid events ------------------------------------

func TestHandler_RejectsUnknownEventType(t *testing.T) {
	t.Parallel()
	h, _ := newTestHandler(t)
	e := validEvent(1)
	e.EventType = "bogus_type"
	body := mustMarshal(t, validBatch(e))

	rr := postJSON(t, h, body, true, nil)
	resp := decodeResponse(t, rr)
	if resp.Rejected != 1 || resp.Accepted != 0 {
		t.Errorf("resp = %+v, want rejected=1 accepted=0", resp)
	}
}

// --- S2 G2: tolerant validation --------------------------------------------

// TestHandler_UnknownDimKey_AcceptedDimDropped supersedes the old strict
// "reject the whole event" behavior (spec §4.3 tolerant mode, S2 G2): a
// NEWER client's dim key an OLDER ingest doesn't recognize must not drop the
// whole event.
func TestHandler_UnknownDimKey_AcceptedDimDropped(t *testing.T) {
	t.Parallel()
	h, _ := newTestHandler(t)
	e := validEvent(1)
	e.Dims = map[string]string{"unknown_key": "x"}
	body := mustMarshal(t, validBatch(e))

	rr := postJSON(t, h, body, true, nil)
	resp := decodeResponse(t, rr)
	if resp.Accepted != 1 || resp.Rejected != 0 {
		t.Errorf("resp = %+v, want accepted=1 rejected=0", resp)
	}

	statszRR := httptest.NewRecorder()
	h.Routes().ServeHTTP(statszRR, httptest.NewRequest(http.MethodGet, "/statsz", nil))
	var stats StatszResponse
	if err := json.Unmarshal(statszRR.Body.Bytes(), &stats); err != nil {
		t.Fatalf("decode /statsz: %v", err)
	}
	if stats.DimsDroppedTotal != 1 {
		t.Errorf("statsz.DimsDroppedTotal = %d, want 1", stats.DimsDroppedTotal)
	}
}

func TestHandler_UnknownUsageChannel_AcceptedCoerced(t *testing.T) {
	t.Parallel()
	h, _ := newTestHandler(t)
	e := validEvent(1)
	e.UsageChannel = "internal_qa"
	body := mustMarshal(t, validBatch(e))

	rr := postJSON(t, h, body, true, nil)
	resp := decodeResponse(t, rr)
	if resp.Accepted != 1 || resp.Rejected != 0 {
		t.Errorf("resp = %+v, want accepted=1 rejected=0", resp)
	}
}

func TestHandler_UnknownRuntimeEnv_AcceptedCoerced(t *testing.T) {
	t.Parallel()
	h, _ := newTestHandler(t)
	e := validEvent(1)
	e.RuntimeEnv = "serverless"
	body := mustMarshal(t, validBatch(e))

	rr := postJSON(t, h, body, true, nil)
	resp := decodeResponse(t, rr)
	if resp.Accepted != 1 || resp.Rejected != 0 {
		t.Errorf("resp = %+v, want accepted=1 rejected=0", resp)
	}
}

// TestHandler_NewerSchemaVersion_AcceptedForwardCompat supersedes the old
// TestHandler_RejectsBadSchemaVersion for the ">supported" case (spec §4.3/
// §8, S2 G2): a genuinely NEWER client schema_version is accepted once its
// version-1 core validates.
func TestHandler_NewerSchemaVersion_AcceptedForwardCompat(t *testing.T) {
	t.Parallel()
	h, _ := newTestHandler(t)
	e := validEvent(1)
	e.SchemaVersion = 99
	body := mustMarshal(t, validBatch(e))

	rr := postJSON(t, h, body, true, nil)
	resp := decodeResponse(t, rr)
	if resp.Accepted != 1 || resp.Rejected != 0 {
		t.Errorf("resp = %+v, want accepted=1 rejected=0", resp)
	}

	statszRR := httptest.NewRecorder()
	h.Routes().ServeHTTP(statszRR, httptest.NewRequest(http.MethodGet, "/statsz", nil))
	var stats StatszResponse
	if err := json.Unmarshal(statszRR.Body.Bytes(), &stats); err != nil {
		t.Fatalf("decode /statsz: %v", err)
	}
	if stats.ForwardCompatAcceptedTotal != 1 {
		t.Errorf("statsz.ForwardCompatAcceptedTotal = %d, want 1", stats.ForwardCompatAcceptedTotal)
	}
}

// TestHandler_OlderThanSupportedSchemaVersion_Rejected is the structural-
// garbage boundary: no schema_version older than 1 has ever shipped, so it
// stays a hard reject (spec §4.3).
func TestHandler_OlderThanSupportedSchemaVersion_Rejected(t *testing.T) {
	t.Parallel()
	h, _ := newTestHandler(t)
	e := validEvent(1)
	e.SchemaVersion = 0
	body := mustMarshal(t, validBatch(e))

	rr := postJSON(t, h, body, true, nil)
	resp := decodeResponse(t, rr)
	if resp.Rejected != 1 || resp.Accepted != 0 {
		t.Errorf("resp = %+v, want rejected=1 accepted=0", resp)
	}
}

func TestHandler_ResponseIncludesMaxSchemaVersion(t *testing.T) {
	t.Parallel()
	h, _ := newTestHandler(t)
	body := mustMarshal(t, validBatch(validEvent(1)))

	rr := postJSON(t, h, body, true, nil)
	resp := decodeResponse(t, rr)
	if resp.MaxSchemaVersion != wire.SchemaVersion {
		t.Errorf("resp.MaxSchemaVersion = %d, want %d", resp.MaxSchemaVersion, wire.SchemaVersion)
	}
}

func TestHandler_RejectsOverLengthDimValue(t *testing.T) {
	t.Parallel()
	h, _ := newTestHandler(t)
	e := validEvent(1)
	e.EventType = wire.EventSessionEnd
	e.Dims = map[string]string{wire.DimShutdownReason: strings.Repeat("a", 65)}
	body := mustMarshal(t, validBatch(e))

	rr := postJSON(t, h, body, true, nil)
	resp := decodeResponse(t, rr)
	if resp.Rejected != 1 {
		t.Errorf("resp = %+v, want rejected=1", resp)
	}
}

func TestHandler_RejectsNonUUIDInstallID(t *testing.T) {
	t.Parallel()
	h, _ := newTestHandler(t)
	e := validEvent(1)
	e.InstallID = "not-a-uuid"
	body := mustMarshal(t, validBatch(e))

	rr := postJSON(t, h, body, true, nil)
	resp := decodeResponse(t, rr)
	if resp.Rejected != 1 {
		t.Errorf("resp = %+v, want rejected=1", resp)
	}
}

// The oversize-single-event rejection class (>4 KiB encoded, spec §4.3
// Limits) is pinned at the wire level by internal/telemetry/wire's own
// validate_test.go (S1) — wire.ValidateStrict is a hard dependency of
// every ingest rejection test above, so ingest's PROPAGATION of that
// rejection into "rejected" is already covered by
// TestHandler_RejectsUnknownEventType et al., which exercise the same
// `if err := wire.ValidateStrict(&e); err != nil { resp.Rejected++ }` path.

func TestHandler_DuplicateEventID_CountedNotReinserted(t *testing.T) {
	t.Parallel()
	h, fi := newTestHandler(t)
	e := validEvent(1)
	body := mustMarshal(t, validBatch(e))

	rr1 := postJSON(t, h, body, true, nil)
	resp1 := decodeResponse(t, rr1)
	if resp1.Accepted != 1 {
		t.Fatalf("first send resp = %+v, want accepted=1", resp1)
	}

	rr2 := postJSON(t, h, body, true, nil)
	resp2 := decodeResponse(t, rr2)
	if resp2.Duplicate != 1 || resp2.Accepted != 0 {
		t.Errorf("second send resp = %+v, want duplicate=1 accepted=0", resp2)
	}

	_ = h.batch.stop(context.Background())
	batches, _ := fi.snapshot()
	total := 0
	for _, b := range batches {
		total += len(b)
	}
	if total != 1 {
		t.Errorf("rows delivered to inserter = %d, want 1 (duplicate must not be re-inserted)", total)
	}
}

// --- transport-level caps -------------------------------------------------

func TestHandler_TooManyEventsInBatch_Rejected400(t *testing.T) {
	t.Parallel()
	h, _ := newTestHandler(t)
	events := make([]wire.Event, wire.MaxBatchEvents+1)
	for i := range events {
		events[i] = validEvent(uint64(i + 1))
	}
	body := mustMarshal(t, validBatch(events...))

	rr := postJSON(t, h, body, true, nil)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for a batch exceeding MaxBatchEvents", rr.Code)
	}
}

func TestHandler_OversizeCompressedBody_Rejected413(t *testing.T) {
	t.Parallel()
	h, _ := newTestHandler(t)

	events := make([]wire.Event, 0, 100)
	for i := range 100 {
		e := validEvent(uint64(i + 1))
		// Pad with high-entropy-ish (but still shape-valid) filler so the
		// gzip-compressed body exceeds 64 KiB — repeated close_mode/route
		// text compresses too well, so vary content per event.
		e.CloseMode = fmt.Sprintf("m%040d", i)
		events = append(events, e)
	}
	body := mustMarshal(t, validBatch(events...))
	// Force a body large enough pre-compression that even gzip can't beat
	// the 64 KiB compressed cap: append a large low-entropy JSON string
	// nowhere near representable as an event (invalid JSON overall is
	// fine — the cap must trip before JSON parsing happens).
	var buf bytes.Buffer
	gw, _ := gzip.NewWriterLevel(&buf, gzip.NoCompression)
	if _, err := gw.Write(append(body, bytes.Repeat([]byte("x"), 200_000)...)); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/events", &buf)
	req.Header.Set("Content-Encoding", "gzip")
	rr := httptest.NewRecorder()
	h.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413 for an oversize compressed body", rr.Code)
	}
}

func TestHandler_OversizeDecodedBody_Rejected413(t *testing.T) {
	t.Parallel()
	h, _ := newTestHandler(t)

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	// Highly compressible payload: small compressed size, large decoded
	// size — proves the POST-decompress cap (256 KiB) is enforced
	// independently of the pre-decompress cap (64 KiB).
	if _, err := gw.Write(bytes.Repeat([]byte("a"), 400_000)); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	if buf.Len() >= maxCompressedBytes {
		t.Fatalf("test fixture invalid: compressed size %d already exceeds the compressed cap", buf.Len())
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/events", &buf)
	req.Header.Set("Content-Encoding", "gzip")
	rr := httptest.NewRecorder()
	h.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413 for an oversize decoded body", rr.Code)
	}
}

func TestHandler_MalformedJSON_Rejected400(t *testing.T) {
	t.Parallel()
	h, _ := newTestHandler(t)
	rr := postJSON(t, h, []byte("{not json"), true, nil)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for malformed JSON", rr.Code)
	}
}

func TestHandler_MalformedGzip_Rejected400(t *testing.T) {
	t.Parallel()
	h, _ := newTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/events", bytes.NewReader([]byte("not actually gzip")))
	req.Header.Set("Content-Encoding", "gzip")
	rr := httptest.NewRecorder()
	h.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for malformed gzip", rr.Code)
	}
}

func TestHandler_UnknownProtocolVersion_Rejected400(t *testing.T) {
	t.Parallel()
	h, _ := newTestHandler(t)
	batch := validBatch(validEvent(1))
	batch.ProtocolVersion = 99
	body := mustMarshal(t, batch)

	rr := postJSON(t, h, body, true, nil)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for unknown protocol_version", rr.Code)
	}
}

// --- S4: /statsz observability counters + ops-route hardening --------------

func TestStatsz_CountsAcceptedAndRejected(t *testing.T) {
	t.Parallel()
	h, _ := newTestHandler(t)
	good := validEvent(1)
	bad := validEvent(2)
	bad.EventID = "" // fails ValidateStrict's UUID check → a schema reject
	body := mustMarshal(t, validBatch(good, bad))
	postJSON(t, h, body, true, map[string]string{"X-Real-IP": "203.0.113.10"})

	statszRR := httptest.NewRecorder()
	h.Routes().ServeHTTP(statszRR, httptest.NewRequest(http.MethodGet, "/statsz", nil))
	var stats StatszResponse
	if err := json.Unmarshal(statszRR.Body.Bytes(), &stats); err != nil {
		t.Fatalf("decode /statsz: %v", err)
	}
	if stats.EventsAcceptedTotal != 1 {
		t.Errorf("EventsAcceptedTotal = %d, want 1", stats.EventsAcceptedTotal)
	}
	if stats.EventsRejectedTotal != 1 {
		t.Errorf("EventsRejectedTotal = %d, want 1", stats.EventsRejectedTotal)
	}
	if stats.ServerErrorsTotal != 0 {
		t.Errorf("ServerErrorsTotal = %d, want 0 (ingest returns no 5xx by design)", stats.ServerErrorsTotal)
	}
}

type countingPinger struct{ pings int }

func (p *countingPinger) Ping(context.Context) error { p.pings++; return nil }

func TestHealthz_PingCachedWithinTTL(t *testing.T) {
	cp := &countingPinger{}
	b := newBatcher(&fakeInserter{}, testLogger(), 5000, time.Hour)
	h := NewHandler(newLimiter(), newDedup(1000, 24*time.Hour), b, cp, newBlocklist(nil, nil), testLogger())
	go b.run()
	t.Cleanup(func() { _ = b.stop(context.Background()) })

	for range 5 {
		if got := h.healthzStatus(context.Background()); got != "ok" {
			t.Fatalf("healthzStatus = %q, want ok", got)
		}
	}
	if cp.pings != 1 {
		t.Errorf("ClickHouse pinged %d times across 5 rapid /healthz calls, want 1 (cached within TTL)", cp.pings)
	}
}

func TestStatsz_RateLimited(t *testing.T) {
	t.Parallel()
	h, _ := newTestHandler(t)
	var last *httptest.ResponseRecorder
	for range 601 {
		last = httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/statsz", nil)
		req.Header.Set("X-Real-IP", "203.0.113.42")
		h.Routes().ServeHTTP(last, req)
	}
	if last.Code != http.StatusTooManyRequests {
		t.Errorf("statsz status = %d after flooding one IP, want 429 (ops route is bounded)", last.Code)
	}
}

// --- S2: clientIP trusts balancer X-Real-IP, ignores client XFF ------------

func TestClientIP_SpoofedXFF_Ignored(t *testing.T) {
	t.Parallel()
	// A client-supplied X-Forwarded-For must NOT become the identity key —
	// behind the Zerops L7 balancer it is appended left-most and therefore
	// spoofable (verified live 2026-07). With no X-Real-IP, two requests with
	// different spoofed XFF collapse onto the same socket-peer bucket, so a
	// caller cannot rotate XFF to escape the per-IP limit or the blocklist.
	r1 := httptest.NewRequest(http.MethodPost, "/v1/events", nil)
	r1.RemoteAddr = "10.0.0.1:5000"
	r1.Header.Set("X-Forwarded-For", "203.0.113.7")
	r2 := httptest.NewRequest(http.MethodPost, "/v1/events", nil)
	r2.RemoteAddr = "10.0.0.1:6000"
	r2.Header.Set("X-Forwarded-For", "203.0.113.8")
	if got1, got2 := clientIP(r1), clientIP(r2); got1 != got2 {
		t.Fatalf("spoofed XFF leaked into the key: %q vs %q (want both = socket peer)", got1, got2)
	}
	if got := clientIP(r1); got != "10.0.0.1" {
		t.Errorf("clientIP with only XFF = %q, want socket peer 10.0.0.1 (XFF ignored)", got)
	}
}

func TestClientIP_XRealIP_Authoritative(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodPost, "/v1/events", nil)
	r.RemoteAddr = "10.0.0.1:5000"
	r.Header.Set("X-Real-IP", "198.51.100.4")
	r.Header.Set("X-Forwarded-For", "203.0.113.7") // spoof — must be ignored
	if got := clientIP(r); got != "198.51.100.4" {
		t.Errorf("clientIP = %q, want the balancer-set X-Real-IP 198.51.100.4", got)
	}
}

func TestClientIP_MalformedXRealIP_FallsBackRemoteAddr(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodPost, "/v1/events", nil)
	r.RemoteAddr = "10.0.0.9:5000"
	r.Header.Set("X-Real-IP", "not-an-ip, 1.2.3.4") // malformed/multi-value
	if got := clientIP(r); got != "10.0.0.9" {
		t.Errorf("clientIP with malformed X-Real-IP = %q, want RemoteAddr fallback 10.0.0.9", got)
	}
}

func TestClientIP_IPv6Canonicalized(t *testing.T) {
	t.Parallel()
	// Equivalent IPv6 spellings must collapse to one key so a caller can't
	// dodge the per-IP limit / blocklist by re-spelling their address.
	r1 := httptest.NewRequest(http.MethodPost, "/v1/events", nil)
	r1.Header.Set("X-Real-IP", "2001:db8::1")
	r2 := httptest.NewRequest(http.MethodPost, "/v1/events", nil)
	r2.Header.Set("X-Real-IP", "2001:0db8:0000:0000:0000:0000:0000:0001")
	if clientIP(r1) != clientIP(r2) {
		t.Errorf("equivalent IPv6 spellings gave different keys: %q vs %q", clientIP(r1), clientIP(r2))
	}
}

// --- rate limiting ---------------------------------------------------------

func TestHandler_IPRateLimit_Returns429WithRetryAfter(t *testing.T) {
	t.Parallel()
	h, _ := newTestHandler(t)
	batch := validBatch(validEvent(1))
	body := mustMarshal(t, batch)

	var last *httptest.ResponseRecorder
	for range 601 {
		last = postJSON(t, h, body, true, map[string]string{"X-Real-IP": "203.0.113.9"})
	}
	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 after exceeding the IP burst", last.Code)
	}
	resp := decodeResponse(t, last)
	if resp.RetryAfterMs <= 0 {
		t.Errorf("retry_after_ms = %d, want > 0", resp.RetryAfterMs)
	}
}

// --- T9: no IP value reaches any durable output ----------------------------

func TestHandler_NoIPInResponseBody(t *testing.T) {
	t.Parallel()
	h, _ := newTestHandler(t)
	const fakeIP = "198.51.100.77"
	batch := validBatch(validEvent(1))
	body := mustMarshal(t, batch)

	rr := postJSON(t, h, body, true, map[string]string{"X-Real-IP": fakeIP})
	if strings.Contains(rr.Body.String(), fakeIP) {
		t.Errorf("response body leaked the client IP: %s", rr.Body.String())
	}
}

func TestHandler_NoIPInInsertedRows(t *testing.T) {
	t.Parallel()
	h, fi := newTestHandler(t)
	const fakeIP = "198.51.100.77"
	batch := validBatch(validEvent(1))
	body := mustMarshal(t, batch)

	postJSON(t, h, body, true, map[string]string{"X-Real-IP": fakeIP})
	_ = h.batch.stop(context.Background())

	batches, _ := fi.snapshot()
	for _, rowBatch := range batches {
		for _, row := range rowBatch {
			encoded := mustMarshal(t, row)
			if strings.Contains(string(encoded), fakeIP) {
				t.Errorf("inserted row leaked the client IP: %s", encoded)
			}
		}
	}
}

func TestHandler_NoIPInLoggerOutput(t *testing.T) {
	t.Parallel()
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	fi := &fakeInserter{err: fmt.Errorf("simulated clickhouse down")}
	b := newBatcher(fi, logger, 1, time.Hour) // flushRows=1: forces an immediate flush+log on add
	h := NewHandler(newLimiter(), newDedup(1000, 24*time.Hour), b, nil, newBlocklist(nil, nil), logger)
	go b.run()

	const fakeIP = "198.51.100.201"
	batch := validBatch(validEvent(1))
	body := mustMarshal(t, batch)
	postJSON(t, h, body, true, map[string]string{"X-Real-IP": fakeIP})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, calls := fi.snapshot(); calls >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	_ = b.stop(context.Background())

	if strings.Contains(logBuf.String(), fakeIP) {
		t.Errorf("logger output leaked the client IP: %s", logBuf.String())
	}
}

// --- R5: env-configurable blocklists ---------------------------------------

func TestHandler_BlockedIP_Returns403(t *testing.T) {
	t.Parallel()
	fi := &fakeInserter{}
	b := newBatcher(fi, testLogger(), 5000, time.Hour)
	const blockedIP = "203.0.113.55"
	h := NewHandler(newLimiter(), newDedup(1000, 24*time.Hour), b, nil, newBlocklist([]string{blockedIP}, nil), testLogger())
	go b.run()
	t.Cleanup(func() { _ = b.stop(context.Background()) })

	body := mustMarshal(t, validBatch(validEvent(1)))
	rr := postJSON(t, h, body, true, map[string]string{"X-Real-IP": blockedIP})
	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for a blocked IP", rr.Code)
	}

	_ = b.stop(context.Background())
	batches, _ := fi.snapshot()
	if len(batches) != 0 {
		t.Errorf("a blocked IP's event reached the inserter: %v", batches)
	}
}

func TestHandler_UnblockedIP_StillAccepted(t *testing.T) {
	t.Parallel()
	fi := &fakeInserter{}
	b := newBatcher(fi, testLogger(), 5000, time.Hour)
	h := NewHandler(newLimiter(), newDedup(1000, 24*time.Hour), b, nil, newBlocklist([]string{"203.0.113.55"}, nil), testLogger())
	go b.run()
	t.Cleanup(func() { _ = b.stop(context.Background()) })

	body := mustMarshal(t, validBatch(validEvent(1)))
	rr := postJSON(t, h, body, true, map[string]string{"X-Real-IP": "203.0.113.99"})
	if rr.Code != http.StatusAccepted {
		t.Errorf("status = %d, want 202 for an IP not on the blocklist", rr.Code)
	}
}

func TestHandler_BlockedInstall_Returns403(t *testing.T) {
	t.Parallel()
	fi := &fakeInserter{}
	b := newBatcher(fi, testLogger(), 5000, time.Hour)
	e := validEvent(1)
	h := NewHandler(newLimiter(), newDedup(1000, 24*time.Hour), b, nil, newBlocklist(nil, []string{e.InstallID}), testLogger())
	go b.run()
	t.Cleanup(func() { _ = b.stop(context.Background()) })

	body := mustMarshal(t, validBatch(e))
	rr := postJSON(t, h, body, true, nil)
	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for a blocked install_id", rr.Code)
	}

	_ = b.stop(context.Background())
	batches, _ := fi.snapshot()
	if len(batches) != 0 {
		t.Errorf("a blocked install's event reached the inserter: %v", batches)
	}
}
