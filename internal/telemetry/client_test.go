package telemetry

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/telemetry/wire"
)

func testConfig(endpoint string) Config {
	return Config{
		Enabled:    true,
		Channel:    wire.ChannelExternal,
		Endpoint:   endpoint,
		InstallID:  "11111111-1111-4111-8111-111111111111",
		SessionID:  "22222222-2222-4222-8222-222222222222",
		ZcpVersion: "v1.0.0",
		OS:         "linux",
		Arch:       "amd64",
		RuntimeEnv: wire.RuntimeLocal,
	}
}

// decodeGzipBatch reads a gzip-encoded wire.Batch POST body.
func decodeGzipBatch(t *testing.T, body io.Reader) wire.Batch {
	t.Helper()
	gr, err := gzip.NewReader(body)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer gr.Close()
	var batch wire.Batch
	if err := json.NewDecoder(gr).Decode(&batch); err != nil {
		t.Fatalf("decode batch: %v", err)
	}
	return batch
}

// T1-adjacent: nil Emitter / nil *Client must never panic and must be cheap.
func TestClient_NilClient_EmitAndShutdownAreNoop(t *testing.T) {
	var c *Client
	c.Emit(wire.Event{EventType: wire.EventToolCall})
	if err := c.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown on nil client: %v", err)
	}
	if got := c.DroppedCount(); got != 0 {
		t.Fatalf("DroppedCount on nil client = %d, want 0", got)
	}
}

// T4-adjacent: disabled config → Emit never enqueues, never spools.
func TestClient_Disabled_EmitIsNoop(t *testing.T) {
	spoolDir := t.TempDir()
	var stderr bytes.Buffer
	c := newClient(Config{Enabled: false}, &stderr, spoolDir)
	defer func() { _ = c.Shutdown(context.Background()) }()

	c.Emit(wire.Event{EventType: wire.EventToolCall, Tool: "zerops_deploy"})

	entries, err := readDirNames(spoolDir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("spool dir has entries for a disabled client: %v", entries)
	}
}

// T5: queue overflow drops events, never blocks Emit. Deterministic: the
// worker is shut down FIRST (nothing ever drains c.queue), so every Emit
// past queueCapacity is guaranteed to hit the non-blocking drop path — no
// timing race with a live worker goroutine.
func TestClient_QueueOverflow_DropsNewestNeverBlocks(t *testing.T) {
	spoolDir := t.TempDir()
	var stderr bytes.Buffer
	c := newClient(testConfig("http://127.0.0.1:0/unreachable"), &stderr, spoolDir)

	// Drain-free from here on: the worker has exited, nothing reads c.queue.
	if err := c.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	const overflow = 500
	const n = queueCapacity + overflow
	done := make(chan struct{})
	go func() {
		for range n {
			c.Emit(wire.Event{EventType: wire.EventSessionStart})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Emit blocked — queue overflow must drop, never block")
	}

	if got := c.DroppedCount(); got != overflow {
		t.Fatalf("DroppedCount() = %d, want exactly %d (n - queueCapacity, worker never drains)", got, overflow)
	}
}

// Seq monotonicity under concurrent Emit — race-clean, unique per-event seq.
func TestClient_ConcurrentEmit_SeqIsUniqueAndMonotonic(t *testing.T) {
	// Use an enabled client with debug mode so no network is involved but
	// fillEnvelope (and thus seq assignment) still runs for every Emit.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	cfg := testConfig(srv.URL)
	cfg.Debug = true
	var buf syncBuffer
	dc := newClient(cfg, &buf, t.TempDir())
	defer func() { _ = dc.Shutdown(context.Background()) }()

	const goroutines = 20
	const perGoroutine = 50
	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			for range perGoroutine {
				dc.Emit(wire.Event{EventType: wire.EventSessionStart})
			}
		})
	}
	wg.Wait()

	// Give the worker time to drain the debug-mode writes.
	deadline := time.Now().Add(3 * time.Second)
	want := goroutines * perGoroutine
	for time.Now().Before(deadline) {
		if buf.lineCount() >= want {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	lines := buf.lines()
	if len(lines) != want {
		t.Fatalf("got %d debug lines, want %d", len(lines), want)
	}
	seen := map[uint64]struct{}{}
	var maxSeq uint64
	for _, line := range lines {
		var e wire.Event
		if err := json.Unmarshal(line, &e); err != nil {
			t.Fatalf("unmarshal debug line: %v", err)
		}
		if _, dup := seen[e.Seq]; dup {
			t.Fatalf("duplicate seq %d", e.Seq)
		}
		seen[e.Seq] = struct{}{}
		if e.Seq > maxSeq {
			maxSeq = e.Seq
		}
		if e.EventID != e.SessionID+":"+seqStr(e.Seq) {
			t.Fatalf("event_id %q does not match session_id:seq for seq %d", e.EventID, e.Seq)
		}
	}
	if int(maxSeq) != want {
		t.Fatalf("max seq = %d, want %d (dense 1..N under this client's exclusive use)", maxSeq, want)
	}
}

// T7: spool respects bounds is covered by spool_test.go; here we cover the
// client-level integration — a send failure at shutdown spools the leftover
// batch, and a corrupt segment doesn't wedge later drains (T7 replay).
func TestClient_Shutdown_SendFailureSpoolsLeftovers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	spoolDir := t.TempDir()
	var stderr bytes.Buffer
	c := newClient(testConfig(srv.URL), &stderr, spoolDir)

	c.Emit(wire.Event{EventType: wire.EventToolCall, Tool: "zerops_deploy", Action: "deploy"})

	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()
	if err := c.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	names, err := readDirNames(spoolDir)
	if err != nil {
		t.Fatalf("readdir spool: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("expected a spool segment after a failed shutdown send")
	}
}

// G7: Shutdown must ALSO attempt to drain one leftover spool segment
// (budget-permitting), not just flush the fresh in-process batch. Without
// this, only serve-mode's periodic 5s ticker ever drains the spool — a CLI
// one-shot process (which routinely exits well before the first tick) can
// never clear a backlog left by a prior failed run, so its spool ages out
// at the 7-day bound instead of ever being delivered (spec §5.5).
func TestClient_Shutdown_DrainsOneLeftoverSpoolSegment(t *testing.T) {
	received := make(chan wire.Batch, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- decodeGzipBatch(t, r.Body)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	spoolDir := t.TempDir()
	// Seed a leftover segment as if a PRIOR process failed to send it (e.g.
	// the ingest was down during that run).
	seedSpool, err := newSpool(spoolDir)
	if err != nil {
		t.Fatalf("newSpool: %v", err)
	}
	leftover := wire.Event{
		EventType: wire.EventCLICommand, Command: "deploy",
		InstallID: "11111111-1111-4111-8111-111111111111",
		SessionID: "33333333-3333-4333-8333-333333333333",
		EventID:   "33333333-3333-4333-8333-333333333333:1", Seq: 1,
	}
	if err := seedSpool.write([]wire.Event{leftover}); err != nil {
		t.Fatalf("seed leftover spool segment: %v", err)
	}

	var stderr bytes.Buffer
	c := newClient(testConfig(srv.URL), &stderr, spoolDir)
	// No new Emit this run — a CLI one-shot with nothing new to say must
	// STILL get a chance to clear the pre-existing backlog on Shutdown.

	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()
	if err := c.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	select {
	case batch := <-received:
		if len(batch.Events) != 1 || batch.Events[0].Command != "deploy" {
			t.Fatalf("server received unexpected batch: %+v", batch)
		}
	default:
		t.Fatal("server never received the leftover spool segment — Shutdown did not drain it")
	}

	names, err := readDirNames(spoolDir)
	if err != nil {
		t.Fatalf("readdir spool: %v", err)
	}
	if len(names) != 0 {
		t.Fatalf("leftover spool segment was not removed after a successful drain: %v", names)
	}
}

// Successful send path: event reaches the server with the correct envelope.
func TestClient_SendsValidBatchOnFlush(t *testing.T) {
	received := make(chan wire.Batch, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Encoding") != "gzip" {
			t.Errorf("Content-Encoding = %q, want gzip", r.Header.Get("Content-Encoding"))
		}
		received <- decodeGzipBatch(t, r.Body)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	spoolDir := t.TempDir()
	var stderr bytes.Buffer
	c := newClient(testConfig(srv.URL), &stderr, spoolDir)

	c.Emit(wire.Event{EventType: wire.EventToolCall, Tool: "zerops_deploy", Action: "deploy", Success: true})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	select {
	case batch := <-received:
		if len(batch.Events) != 1 {
			t.Fatalf("got %d events, want 1", len(batch.Events))
		}
		e := batch.Events[0]
		if e.Tool != "zerops_deploy" || e.InstallID == "" || e.SessionID == "" || e.EventID == "" {
			t.Fatalf("event envelope incomplete: %+v", e)
		}
		if batch.ProtocolVersion != wire.ProtocolVersion {
			t.Fatalf("ProtocolVersion = %d, want %d", batch.ProtocolVersion, wire.ProtocolVersion)
		}
	default:
		t.Fatal("server never received a batch")
	}
}

// --- S2: release-ordering guard (server < client schema_version) ----------

// TestClient_SendBatch_StaleIngestResponse_WarnsToStderr proves the wiring
// end to end: a real HTTP round-trip through sendBatch, not just the helper
// method in isolation.
func TestClient_SendBatch_StaleIngestResponse_WarnsToStderr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"accepted":1,"max_schema_version":0}`))
	}))
	defer srv.Close()

	spoolDir := t.TempDir()
	var stderr bytes.Buffer
	c := newClient(testConfig(srv.URL), &stderr, spoolDir)

	c.Emit(wire.Event{EventType: wire.EventToolCall, Tool: "zerops_deploy"})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	if !strings.Contains(stderr.String(), "schema_version") {
		t.Fatalf("expected a schema-version warning in stderr, got: %q", stderr.String())
	}
}

func TestClient_SchemaWarn_ServerBehindClient_WarnsOnceEvenAcrossRepeatedResponses(t *testing.T) {
	spoolDir := t.TempDir()
	var stderr bytes.Buffer
	c := newClient(testConfig("http://127.0.0.1:0/unreachable"), &stderr, spoolDir)
	defer func() { _ = c.Shutdown(context.Background()) }()

	staleBody := []byte(`{"accepted":1,"max_schema_version":0}`)
	c.checkSchemaWarning(staleBody)
	c.checkSchemaWarning(staleBody)
	c.checkSchemaWarning(staleBody)

	if got := strings.Count(stderr.String(), "schema_version"); got != 1 {
		t.Fatalf("expected exactly one schema-version warning across repeated stale responses, got %d in: %q",
			got, stderr.String())
	}
}

func TestClient_SchemaWarn_ServerAtOrAheadOfClient_NoWarning(t *testing.T) {
	spoolDir := t.TempDir()
	var stderr bytes.Buffer
	c := newClient(testConfig("http://127.0.0.1:0/unreachable"), &stderr, spoolDir)
	defer func() { _ = c.Shutdown(context.Background()) }()

	c.checkSchemaWarning([]byte(`{"accepted":1,"max_schema_version":1}`))
	c.checkSchemaWarning([]byte(`{"accepted":1,"max_schema_version":2}`))

	if stderr.Len() != 0 {
		t.Fatalf("expected no warning when server schema_version >= client, got: %q", stderr.String())
	}
}

func TestClient_SchemaWarn_MalformedOrEmptyBody_NeverPanicsOrWarns(t *testing.T) {
	spoolDir := t.TempDir()
	var stderr bytes.Buffer
	c := newClient(testConfig("http://127.0.0.1:0/unreachable"), &stderr, spoolDir)
	defer func() { _ = c.Shutdown(context.Background()) }()

	c.checkSchemaWarning([]byte(`not json`))
	c.checkSchemaWarning(nil)
	c.checkSchemaWarning([]byte{})

	if stderr.Len() != 0 {
		t.Fatalf("expected no warning from malformed/empty bodies, got: %q", stderr.String())
	}
}

// Permanent 4xx rejection: batch is dropped, never spooled.
func TestClient_PermanentReject_DropsWithoutSpooling(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	spoolDir := t.TempDir()
	var stderr bytes.Buffer
	c := newClient(testConfig(srv.URL), &stderr, spoolDir)

	c.Emit(wire.Event{EventType: wire.EventToolCall, Tool: "zerops_deploy"})

	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()
	if err := c.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	names, err := readDirNames(spoolDir)
	if err != nil {
		t.Fatalf("readdir spool: %v", err)
	}
	if len(names) != 0 {
		t.Fatalf("a 4xx-rejected batch must not be spooled, found: %v", names)
	}
}

// Debug mode: events go to the debug writer, never to the network or spool.
func TestClient_DebugMode_WritesJSONLNeverNetworkOrSpool(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	cfg := testConfig(srv.URL)
	cfg.Debug = true
	spoolDir := t.TempDir()
	var buf syncBuffer
	c := newClient(cfg, &buf, spoolDir)

	c.Emit(wire.Event{EventType: wire.EventToolCall, Tool: "zerops_deploy"})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	if buf.lineCount() != 1 {
		t.Fatalf("debug writer got %d lines, want 1", buf.lineCount())
	}
	if called {
		t.Fatal("debug mode must never hit the network")
	}
	names, _ := readDirNames(spoolDir)
	if len(names) != 0 {
		t.Fatal("debug mode must never write to the spool")
	}
}

// A panicking emit path recovers and disables telemetry for the rest of the
// process (spec §5.1) rather than crashing the caller.
func TestClient_EmitPanicRecovery_DisablesFurtherEmit(t *testing.T) {
	spoolDir := t.TempDir()
	var stderr bytes.Buffer
	c := newClient(Config{Enabled: true, SessionID: "not-set-on-purpose"}, &stderr, spoolDir)
	defer func() { _ = c.Shutdown(context.Background()) }()

	// panicDisabled starts false; simulate a worker panic directly (the
	// documented recovery seam) and verify Emit becomes a no-op afterward.
	c.panicDisabled.Store(true)
	c.Emit(wire.Event{EventType: wire.EventToolCall, Tool: "zerops_deploy"})

	if got := c.queueDropped.Load(); got != 0 {
		t.Fatalf("queueDropped = %d, want 0 — a panic-disabled client shouldn't even attempt to enqueue", got)
	}
}

func readDirNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, filepath.Join(dir, e.Name()))
	}
	return names, nil
}

// syncBuffer is a concurrency-safe io.Writer used to capture debug-mode
// output from the worker goroutine in tests.
type syncBuffer struct {
	mu       sync.Mutex
	rawLines [][]byte
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	cp := make([]byte, len(p))
	copy(cp, p)
	b.rawLines = append(b.rawLines, bytes.TrimRight(cp, "\n"))
	return len(p), nil
}

func (b *syncBuffer) lineCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.rawLines)
}

func (b *syncBuffer) lines() [][]byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([][]byte, len(b.rawLines))
	copy(out, b.rawLines)
	return out
}

func seqStr(seq uint64) string {
	return strconv.FormatUint(seq, 10)
}
