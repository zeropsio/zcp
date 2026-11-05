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

func TestNewServer_HasPublicTimeouts(t *testing.T) {
	srv := newServer(nil)
	if srv.ReadTimeout == 0 || srv.WriteTimeout == 0 || srv.IdleTimeout == 0 || srv.ReadHeaderTimeout == 0 {
		t.Errorf("public-ingress server must set every deadline (Slowloris hole): read=%v write=%v idle=%v header=%v",
			srv.ReadTimeout, srv.WriteTimeout, srv.IdleTimeout, srv.ReadHeaderTimeout)
	}
}

func TestServe_HandlesRequestsThenShutsDownGracefully(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
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
	if err := <-serveErrCh; err != nil {
		t.Errorf("serve returned %v, want nil on graceful shutdown", err)
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

// listeningHandler records the listener address once Run has bound it.
type listeningHandler struct{ listening chan string }

func (*listeningHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *listeningHandler) Handle(_ context.Context, r slog.Record) error {
	if r.Message == "ingest: listening" {
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == "addr" {
				h.listening <- a.Value.String()
				return false
			}
			return true
		})
	}
	return nil
}
func (h *listeningHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *listeningHandler) WithGroup(string) slog.Handler      { return h }

func TestRun_ServesAndShutsDownGracefully(t *testing.T) {
	t.Parallel()

	cfg := Config{
		ListenAddr: "127.0.0.1:0",
		CHHost:     "127.0.0.1",
		CHPort:     1, // nothing listens here; inserts fail-and-log, never crash Run
		CHDatabase: "telemetry",
		CHUser:     "zerops",
	}
	logs := &listeningHandler{listening: make(chan string, 1)}
	logger := slog.New(logs)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	runErrCh := make(chan error, 1)
	go func() { runErrCh <- Run(ctx, cfg, logger) }()

	var addr string
	select {
	case addr = <-logs.listening:
	case err := <-runErrCh:
		t.Fatalf("Run returned before listening: %v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/statsz", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /statsz: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /statsz status = %d, want 200", resp.StatusCode)
	}
	cancel()
	if err := <-runErrCh; err != nil {
		t.Errorf("Run returned %v, want nil on graceful shutdown", err)
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
