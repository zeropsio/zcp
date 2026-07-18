package telemetry

// Chaos test table (plan: telemetry-production-readiness-2026-07-02.md S1,
// R1 "telemetry must never take a project down"). Every scenario below
// forces the client into an adverse host condition (unwritable/corrupt/
// hostile filesystem, malformed/unreachable/hostile network endpoint) and
// pins the SAME four invariants the plan lists for every case:
//   - no panic (a fault production code doesn't recover from crashes the
//     whole test process, not just a subtest — so surviving the run at all
//     is itself part of the proof)
//   - no error surfaces to the caller (Emit is void; Shutdown must return
//     nil, never propagate the underlying fault)
//   - the tool path is unaffected (Emit must return promptly regardless of
//     the fault — chaosEmitBudget)
//   - the process exits promptly (Shutdown must return within its budget,
//     never hang on a wedged fault — chaosShutdownCeiling)
//
// Two more scenarios (Emit-after-Shutdown, concurrent Emit storm during
// Shutdown) and the closed-channel Emit guard pin don't fit the "build a
// faulty environment" shape — they're about call ORDERING, not host state —
// so they're separate Test functions below the table, same invariants.

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/telemetry/wire"
)

const (
	// chaosEmitBudget bounds how long Emit may take under ANY fault
	// condition — the "tool path unaffected" invariant made concrete
	// (spec §5.1 B3: never blocks, never slows measurably).
	chaosEmitBudget = 50 * time.Millisecond
	// chaosShutdownBudget mirrors the real shutdown context spec §5.5
	// hands the client in production (fresh 750 ms context).
	chaosShutdownBudget = 750 * time.Millisecond
	// chaosShutdownCeiling is a generous, CI-safe ceiling proving Shutdown
	// actually honors chaosShutdownBudget instead of hanging on a wedged
	// fault — the "process exits promptly" invariant made concrete.
	chaosShutdownCeiling = 2 * time.Second
)

// TestClient_Chaos_NeverHarmsHost runs every environmental-fault scenario
// through the same lifecycle (build faulty client → Emit → Shutdown) and
// asserts the shared invariants described in the file doc comment.
func TestClient_Chaos_NeverHarmsHost(t *testing.T) {
	cases := []struct {
		name  string
		build func(t *testing.T) *Client
	}{
		{"unwritable HOME", buildUnwritableHOME},
		{"read-only FS mid-run", buildReadOnlyFSMidRun},
		{"disk-full spool write", buildDiskFullSpoolWrite},
		{"corrupt install.json + spool", buildCorruptInstallAndSpool},
		{"invalid ZCP_TELEMETRY_ENDPOINT URL", buildInvalidEndpointURL},
		{"blackholed endpoint (connect timeout)", buildBlackholedEndpoint},
		{"slow-loris server (header stall > 2s)", buildSlowLorisServer},
		{"TLS failure", buildTLSFailure},
		{"HOME with spaces/unicode", buildUnicodeHOME},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			c := tt.build(t)

			emitStart := time.Now()
			c.Emit(wire.Event{EventType: wire.EventToolCall, Tool: "zerops_deploy", Action: "deploy", Success: true})
			c.Emit(wire.Event{EventType: wire.EventCLICommand, Command: "status"})
			if elapsed := time.Since(emitStart); elapsed > chaosEmitBudget {
				t.Fatalf("Emit took %v under fault %q — tool path must never measurably block", elapsed, tt.name)
			}

			ctx, cancel := context.WithTimeout(context.Background(), chaosShutdownBudget)
			defer cancel()
			shutdownStart := time.Now()
			err := c.Shutdown(ctx)
			elapsed := time.Since(shutdownStart)

			if err != nil {
				t.Fatalf("Shutdown returned %v under fault %q — must exit cleanly, never surface the fault to the caller", err, tt.name)
			}
			if elapsed > chaosShutdownCeiling {
				t.Fatalf("Shutdown took %v under fault %q, want <= %v — process must exit promptly", elapsed, tt.name, chaosShutdownCeiling)
			}
		})
	}
}

// chaosOKServer is a local httptest server that always accepts (202) — used
// by scenarios whose fault is elsewhere (filesystem, endpoint) so the send
// path itself stays uninteresting.
func chaosOKServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// chaosFailServer is a local httptest server that always 500s — used to
// force the client down its retryable → spool path so filesystem faults on
// the spool actually get exercised.
func chaosFailServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// buildUnwritableHOME simulates a HOME whose ~/.zcp tree cannot be created
// at all (e.g. a read-only home partition). newSpool's MkdirAll fails,
// c.spoolMgr stays nil, and the client must still construct, Emit, and
// Shutdown cleanly — spool persistence silently no-ops rather than crashing
// (spec §5.1/§5.4).
func buildUnwritableHOME(t *testing.T) *Client {
	t.Helper()
	home := t.TempDir()
	if err := os.Chmod(home, 0o500); err != nil {
		t.Fatalf("chmod home read-only: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(home, 0o700); err != nil {
			t.Errorf("restore home perms for cleanup: %v", err)
		}
	})
	t.Setenv("HOME", home)

	var stderr bytes.Buffer
	// spoolDir == "" forces the real defaultSpoolDir() → os.UserHomeDir()
	// resolution path, the one production code actually takes.
	return newClient(testConfig(chaosFailServer(t).URL), &stderr, "")
}

// buildReadOnlyFSMidRun simulates a spool directory that already existed
// (so construction succeeds) but whose filesystem has since gone
// read-only — every write() call fails with EACCES.
func buildReadOnlyFSMidRun(t *testing.T) *Client {
	t.Helper()
	spoolDir := t.TempDir()
	if err := os.Chmod(spoolDir, 0o500); err != nil {
		t.Fatalf("chmod spool dir read-only: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(spoolDir, 0o700); err != nil {
			t.Errorf("restore spool dir perms for cleanup: %v", err)
		}
	})

	var stderr bytes.Buffer
	return newClient(testConfig(chaosFailServer(t).URL), &stderr, spoolDir)
}

// buildDiskFullSpoolWrite is a portable substitute for real ENOSPC: no unit
// test can reliably fill an actual disk, so this forces the exact same
// write-failure code path (spool.write's os.CreateTemp failing) via a
// spool "directory" that becomes a regular file mid-session — a distinct
// OS error class (ENOTDIR) from the read-only case (EACCES) above, but the
// same "swallow the error, never crash" contract applies.
func buildDiskFullSpoolWrite(t *testing.T) *Client {
	t.Helper()
	spoolDir := filepath.Join(t.TempDir(), "spool")
	if err := os.MkdirAll(spoolDir, 0o700); err != nil {
		t.Fatalf("mkdir spool dir: %v", err)
	}

	var stderr bytes.Buffer
	c := newClient(testConfig(chaosFailServer(t).URL), &stderr, spoolDir)

	// Swap the directory for a plain file AFTER the spool already
	// initialized successfully — simulating the volume filling up
	// mid-session, after startup succeeded.
	if err := os.RemoveAll(spoolDir); err != nil {
		t.Fatalf("remove spool dir: %v", err)
	}
	if err := os.WriteFile(spoolDir, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("seed non-directory spool path: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(spoolDir) })

	return c
}

// buildCorruptInstallAndSpool covers both halves of the scenario name: a
// corrupt install.json (Resolve must disable, not crash — already pinned at
// the config level by TestResolve_InstallFileCorrupt_ResultsDisabled, reproved
// here for the combined chaos narrative) AND a separately enabled client
// whose spool directory already contains a corrupt (non-gzip) segment from a
// prior crash — Shutdown's drain-one-segment attempt (G7) must quarantine it
// to *.bad, never panic or wedge (spec §5.4/§5.5).
func buildCorruptInstallAndSpool(t *testing.T) *Client {
	t.Helper()
	home := t.TempDir()
	if err := writeCorruptFile(installFilePath(home, false)); err != nil {
		t.Fatalf("seed corrupt install file: %v", err)
	}
	var resolveBuf bytes.Buffer
	cfg := Resolve(envMap(nil), home, "v1.0.0", wire.RuntimeLocal, &resolveBuf)
	if cfg.Enabled {
		t.Fatal("Resolve must disable telemetry on a corrupt install file, not panic or silently enable")
	}

	spoolDir := t.TempDir()
	corruptSegment := filepath.Join(spoolDir, "00000000000000000001-000001"+spoolFileExt)
	if err := os.WriteFile(corruptSegment, []byte("not valid gzip"), 0o600); err != nil {
		t.Fatalf("seed corrupt spool segment: %v", err)
	}

	var stderr bytes.Buffer
	return newClient(testConfig(chaosOKServer(t).URL), &stderr, spoolDir)
}

// buildInvalidEndpointURL points ZCP_TELEMETRY_ENDPOINT at a string that
// fails URL parsing outright (a raw control character) — sendBatch's
// http.NewRequestWithContext must classify this as retryable rather than
// letting the parse error escape.
func buildInvalidEndpointURL(t *testing.T) *Client {
	t.Helper()
	var stderr bytes.Buffer
	return newClient(testConfig("http://example.com/\n"), &stderr, t.TempDir())
}

// buildBlackholedEndpoint targets TEST-NET-1 (RFC 5737): reserved for
// documentation, never routed on the real internet — connection attempts
// here are dropped/rejected rather than reaching a live host, simulating a
// blackholed endpoint without depending on external network state.
func buildBlackholedEndpoint(t *testing.T) *Client {
	t.Helper()
	var stderr bytes.Buffer
	return newClient(testConfig("http://192.0.2.1:8081/v1/events"), &stderr, t.TempDir())
}

// buildSlowLorisServer accepts the TCP connection (so dial/connect
// succeeds) but never writes a single response byte — the client must be
// bounded by its own timeout (spec §5.1 sendTimeout / the caller's
// Shutdown context), not by however long the server chooses to stall.
func buildSlowLorisServer(t *testing.T) *Client {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				// Discard whatever the client sends, then just hold the
				// connection open — the client's own timeout is what must
				// end this, not a response we choose to send.
				_, _ = io.Copy(io.Discard, c)
			}(conn)
		}
	}()

	var stderr bytes.Buffer
	return newClient(testConfig("http://"+ln.Addr().String()+"/v1/events"), &stderr, t.TempDir())
}

// buildTLSFailure points an https:// endpoint at a plain-HTTP httptest
// server — the TLS handshake must fail cleanly (classified retryable) with
// no hang and no panic, rather than the batch silently vanishing.
func buildTLSFailure(t *testing.T) *Client {
	t.Helper()
	srv := chaosOKServer(t)
	tlsURL := "https://" + strings.TrimPrefix(srv.URL, "http://") + "/v1/events"

	var stderr bytes.Buffer
	return newClient(testConfig(tlsURL), &stderr, t.TempDir())
}

// buildUnicodeHOME is a portability probe rather than a failure fault: HOME
// contains spaces and non-ASCII/emoji runes, none of which pre-exist on
// disk — every path.Join/MkdirAll/CreateTemp call in the client and spool
// packages must handle it exactly like any other path.
func buildUnicodeHOME(t *testing.T) *Client {
	t.Helper()
	home := filepath.Join(t.TempDir(), "my home 世界 🎉") //nolint:gosmopolitan // deliberate non-ASCII test data — this scenario IS the unicode-path probe
	t.Setenv("HOME", home)

	var stderr bytes.Buffer
	return newClient(testConfig(chaosOKServer(t).URL), &stderr, "")
}

// Emit-after-Shutdown: once Shutdown has returned, further Emit calls must
// remain safe, prompt no-ops forever — a caller that races its own
// teardown sequencing, or keeps a stale Emitter reference alive past
// process shutdown, must never crash or block.
func TestClient_EmitAfterShutdown_NeverPanicsOrBlocks(t *testing.T) {
	var stderr bytes.Buffer
	c := newClient(testConfig(chaosOKServer(t).URL), &stderr, t.TempDir())

	ctx, cancel := context.WithTimeout(context.Background(), chaosShutdownBudget)
	defer cancel()
	if err := c.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	done := make(chan struct{})
	go func() {
		for range 100 {
			c.Emit(wire.Event{EventType: wire.EventToolCall, Tool: "zerops_deploy", Action: "deploy"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(chaosShutdownCeiling):
		t.Fatal("Emit after Shutdown blocked — must always be a prompt no-op")
	}
}

// Concurrent Emit storm during Shutdown: goroutines hammering Emit while
// Shutdown runs concurrently on another goroutine must never race, panic,
// or prevent Shutdown from returning within its budget (spec §5.1/§5.5;
// run with -race to prove the "never" part).
func TestClient_ConcurrentEmitStormDuringShutdown_NeverPanics(t *testing.T) {
	var stderr bytes.Buffer
	c := newClient(testConfig(chaosOKServer(t).URL), &stderr, t.TempDir())

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for range 10 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
					c.Emit(wire.Event{EventType: wire.EventCLICommand, Command: "status"})
				}
			}
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), chaosShutdownBudget)
	defer cancel()
	shutdownErr := make(chan error, 1)
	go func() { shutdownErr <- c.Shutdown(ctx) }()

	select {
	case err := <-shutdownErr:
		if err != nil {
			t.Fatalf("Shutdown under concurrent Emit storm: %v", err)
		}
	case <-time.After(chaosShutdownCeiling):
		t.Fatal("Shutdown did not return promptly under a concurrent Emit storm")
	}
	close(stop)
	wg.Wait()
}

// Guard Emit against a closed-channel send panic explicitly. Production
// code never closes c.queue today (Shutdown lets the worker goroutine exit
// without closing it — see the doc comment on runWorker), so this
// condition isn't reachable through the public API; the point of this test
// is to pin Emit's OWN defer-recover as the independent safety net, so a
// future change to Shutdown/runWorker can't silently remove protection
// against this specific panic class without a test failing.
func TestClient_Emit_ClosedQueueChannel_RecoversWithoutPanic(t *testing.T) {
	var stderr bytes.Buffer
	c := newClient(testConfig(chaosOKServer(t).URL), &stderr, t.TempDir())

	ctx, cancel := context.WithTimeout(context.Background(), chaosShutdownBudget)
	defer cancel()
	if err := c.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	// The worker has fully exited (Shutdown only returns after doneCh
	// closes) — safe to close the queue ourselves here to inject the fault
	// Emit must recover from.
	close(c.queue)

	c.Emit(wire.Event{EventType: wire.EventToolCall, Tool: "zerops_deploy", Action: "deploy"})

	if !c.panicDisabled.Load() {
		t.Fatal("Emit on a closed queue channel must recover and set panicDisabled")
	}
	if !strings.Contains(stderr.String(), "emit panic") {
		t.Errorf("stderr = %q, want a recovered emit-panic message", stderr.String())
	}
}

// BenchmarkEmit_Disabled measures Emit's cost when telemetry is disabled —
// the check-and-return fast path every tool call takes for an opted-out
// user (spec §5.1, §9 T1 "no measurable latency delta"). Measured numbers
// are documented in spec §9 T1.
func BenchmarkEmit_Disabled(b *testing.B) {
	c := newClient(Config{Enabled: false}, io.Discard, "")
	e := wire.Event{EventType: wire.EventToolCall, Tool: "zerops_deploy", Action: "deploy"}
	b.ReportAllocs()
	for b.Loop() {
		c.Emit(e)
	}
}

// BenchmarkEmit_Enabled measures Emit's non-blocking enqueue cost when
// telemetry is enabled and the worker is draining normally — the hot path
// every tool call takes (spec §5.1, §9 T1). Uses session_start rather than
// tool_call/cli_command so the storm guard's token bucket (burst 200,
// spec §5.2) doesn't saturate a few hundred iterations into a multi-million
// iteration run and start measuring the (even cheaper) drop path instead.
// Measured numbers are documented in spec §9 T1.
func BenchmarkEmit_Enabled(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	c := newClient(testConfig(srv.URL), io.Discard, b.TempDir())
	defer func() { _ = c.Shutdown(context.Background()) }()

	e := wire.Event{EventType: wire.EventSessionStart}
	b.ReportAllocs()
	for b.Loop() {
		c.Emit(e)
	}
}
