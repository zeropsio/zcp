package ingest

import (
	"bytes"
	"compress/gzip"
	"context"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServe_HandlesRequestsThenShutsDownGracefully(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	fi := &fakeInserter{}
	b := newBatcher(fi, testLogger(), batchFlushRows, time.Hour)
	h := NewHandler(newLimiter(), newDedup(1000, 24*time.Hour), b, nil, newBlocklist(nil, nil), testLogger())

	serveErrCh := make(chan error, 1)
	go func() { serveErrCh <- serve(ctx, listener, h.Routes(), b, testLogger()) }()

	base := "http://" + listener.Addr().String()

	// Wait for the listener to actually accept before issuing requests.
	waitForServer(t, base+"/healthz")

	batch := validBatch(validEvent(1))
	body := mustMarshal(t, batch)
	var gzBuf bytes.Buffer
	gw := gzip.NewWriter(&gzBuf)
	_, _ = gw.Write(body)
	_ = gw.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/events", &gzBuf)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Encoding", "gzip")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /v1/events: %v", err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status = %d, want 202", resp.StatusCode)
	}
	_ = resp.Body.Close()

	cancel()
	select {
	case err := <-serveErrCh:
		if err != nil {
			t.Errorf("serve returned %v, want nil on graceful shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return within 5s of ctx cancellation")
	}

	batches, _ := fi.snapshot()
	total := 0
	for _, batchRows := range batches {
		total += len(batchRows)
	}
	if total != 1 {
		t.Errorf("rows flushed on shutdown = %d, want 1", total)
	}
}

func waitForServer(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
		if err != nil {
			t.Fatalf("build readiness request: %v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("server did not become ready in time")
}

func TestRun_ServesAndShutsDownGracefully(t *testing.T) {
	t.Parallel()

	cfg := Config{
		ListenAddr: "127.0.0.1:0",
		CHHost:     "127.0.0.1",
		CHPort:     1, // nothing listens here; inserts fail-and-log, never crash Run
		CHDatabase: "telemetry",
		CHUser:     "zerops",
	}
	logger := slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))

	ctx, cancel := context.WithCancel(t.Context())
	runErrCh := make(chan error, 1)
	go func() { runErrCh <- Run(ctx, cfg, logger) }()

	// Run resolves its own ephemeral listener internally; there is no way
	// to probe it externally from cfg alone, so this test only proves
	// Run starts without error and shuts down cleanly on ctx cancellation.
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-runErrCh:
		if err != nil {
			t.Errorf("Run returned %v, want nil on graceful shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within 5s of ctx cancellation")
	}
}

func TestRun_ListenError_ReturnsErrPromptly(t *testing.T) {
	t.Parallel()

	// Occupy a port first so Run's net.Listen fails deterministically.
	var lc net.ListenConfig
	occupied, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = occupied.Close() }()

	cfg := Config{
		ListenAddr: occupied.Addr().String(),
		CHHost:     "127.0.0.1",
		CHPort:     1,
		CHDatabase: "telemetry",
		CHUser:     "zerops",
	}
	logger := slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))

	if err := Run(context.Background(), cfg, logger); err == nil {
		t.Error("expected Run to return an error when the listen address is already in use")
	}
}
