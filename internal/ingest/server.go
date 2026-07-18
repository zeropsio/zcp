package ingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// shutdownTimeout bounds the HTTP server's graceful drain on ctx
// cancellation (spec §6 item 5 "graceful shutdown ... flushes").
const shutdownTimeout = 10 * time.Second

// Public-ingress timeouts (S4): the ingest is exposed to the open internet,
// so every stage of a request carries a deadline — a slow client cannot pin a
// connection open by trickling headers OR body (Slowloris).
const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 15 * time.Second
	idleTimeout       = 60 * time.Second
)

// limiterGCInterval is how often the in-memory limiter reclaims idle
// per-IP/install/session entries (spec §6 item 4 "periodic GC").
const limiterGCInterval = 10 * time.Minute

// Run wires the ingest service (ClickHouse connection → limiter/dedup/
// batcher → HTTP handler) and serves until ctx is canceled, then drains
// gracefully. cmd/zcp-ingest/main.go is the thin production caller,
// typically with ctx from signal.NotifyContext.
func Run(ctx context.Context, cfg Config, logger *slog.Logger) error {
	if cfg.CHSuperPassword != "" {
		if err := bootstrapSchema(ctx, cfg, logger); err != nil {
			return fmt.Errorf("schema bootstrap: %w", err)
		}
	} else {
		logger.Info("ingest: schema bootstrap skipped (CH_SUPER_PASSWORD unset)")
	}

	ch, err := newCHConn(cfg)
	if err != nil {
		return fmt.Errorf("connect clickhouse: %w", err)
	}
	defer func() {
		if closeErr := ch.Close(); closeErr != nil {
			logger.Error("ingest: clickhouse close", "error", closeErr)
		}
	}()

	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "tcp", cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.ListenAddr, err)
	}

	lim := newLimiter()
	dd := newDedup(dedupMaxEntries, dedupTTL)
	b := newBatcher(ch, logger, batchFlushRows, batchFlushInterval)
	bl := newBlocklist(cfg.BlockedIPs, cfg.BlockedInstalls)
	h := NewHandler(lim, dd, b, ch, bl, logger)

	stopGC := make(chan struct{})
	go runLimiterGC(lim, stopGC)
	defer close(stopGC)

	logger.Info("ingest: listening", "addr", listener.Addr().String())
	return serve(ctx, listener, h.Routes(), b, logger)
}

// serve is Run's testable core: accepts on listener until ctx is canceled,
// then gracefully shuts the HTTP server down and flushes the batcher's
// remaining buffered rows (spec §6 item 5). The batcher owns its own
// lifecycle (started here, stopped here) independent of ctx — see
// batcher.run's doc comment.
func serve(ctx context.Context, listener net.Listener, handler http.Handler, b *batcher, logger *slog.Logger) error {
	go b.run() //nolint:contextcheck // run/flush deliberately use a fresh background context for ClickHouse I/O, decoupled from ctx (see batcher.run's doc comment)

	srv := newServer(handler)
	serveErrCh := make(chan error, 1)
	go func() {
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErrCh <- err
			return
		}
		serveErrCh <- nil
	}()

	var serveErr error
	select {
	case serveErr = <-serveErrCh:
	case <-ctx.Done():
		// ctx is already Done here — deriving the shutdown grace period
		// FROM it would cancel srv.Shutdown immediately instead of giving
		// in-flight requests shutdownTimeout to drain. A fresh context is
		// the correct choice, not an oversight.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil { //nolint:contextcheck // see comment above
			logger.Error("ingest: http shutdown", "error", err)
		}
		serveErr = <-serveErrCh
	}

	// Same reasoning as above: b.stop's ctx bounds the wait for the
	// batcher's final flush, which must run even when the outer ctx is
	// already Done (that's precisely the shutdown path this exists for).
	if err := b.stop(context.Background()); err != nil { //nolint:contextcheck // see comment above
		logger.Error("ingest: batcher stop", "error", err)
		if serveErr == nil {
			serveErr = err
		}
	}
	return serveErr
}

// newServer builds the ingest http.Server with the S4 public-ingress
// deadlines set. Extracted as a seam so a test can assert none is zero (an
// unbounded Read/Write/Idle timeout is the Slowloris hole this closes).
func newServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}

// runLimiterGC periodically reclaims idle limiter state (spec §6 item 4).
func runLimiterGC(l *limiter, stop <-chan struct{}) {
	ticker := time.NewTicker(limiterGCInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			l.gc(time.Now())
		}
	}
}
