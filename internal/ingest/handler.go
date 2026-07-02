package ingest

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/zeropsio/zcp/internal/telemetry/wire"
)

// Body size caps (spec §6 item 2): BEFORE decompress (compressed bytes on
// the wire) and AFTER decompress (decoded JSON bytes).
const (
	maxCompressedBytes = 64 * 1024
	maxDecodedBytes    = 256 * 1024
)

// healthzTimeout bounds the best-effort ClickHouse ping in GET /healthz —
// healthz itself always returns 200 (spec §6 item 2); the ping only informs
// the reported status.
const healthzTimeout = 500 * time.Millisecond

// IngestResponse is the per-request accept/reject/duplicate/retry response
// (spec §4.4). wire.Response is the single owner of this shape — both sides
// of the ingest/client boundary share it, never a hand-duplicated copy.
type IngestResponse = wire.Response

// StatszResponse is the tiny ops-counters payload (spec §6 GET /statsz):
// counts only, never row/event content — the pipeline-health Grafana
// panel's data source (spec S2 G1/G2).
type StatszResponse struct {
	RowsDroppedTotal           int64 `json:"rows_dropped_total"`
	InsertFailuresTotal        int64 `json:"insert_failures_total"`
	DimsDroppedTotal           int64 `json:"dims_dropped_total"`
	ForwardCompatAcceptedTotal int64 `json:"forward_compat_accepted_total"`
}

// pinger is the healthz seam — chInsertConn implements it; tests may pass
// nil (healthz then reports "ok" unconditionally, no CH check attempted).
type pinger interface {
	Ping(ctx context.Context) error
}

// Handler is the ingest HTTP surface: POST /v1/events + GET /healthz +
// GET /statsz (spec §6). Every dependency is injected so it never touches a
// live ClickHouse or the wall clock directly in tests.
type Handler struct {
	limiter *limiter
	dedup   *dedup
	batch   *batcher
	ch      pinger
	block   *blocklist
	logger  *slog.Logger
	now     func() time.Time

	dimsDroppedTotal           atomic.Int64
	forwardCompatAcceptedTotal atomic.Int64
}

// NewHandler constructs a Handler. ch may be nil (healthz then skips the
// ClickHouse ping and always reports "ok").
func NewHandler(l *limiter, d *dedup, b *batcher, ch pinger, bl *blocklist, logger *slog.Logger) *Handler {
	return &Handler{limiter: l, dedup: d, batch: b, ch: ch, block: bl, logger: logger, now: time.Now}
}

// Routes returns the ingest service's HTTP mux (spec §6).
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/events", h.handleEvents)
	mux.HandleFunc("GET /healthz", h.handleHealthz)
	mux.HandleFunc("GET /statsz", h.handleStatsz)
	return mux
}

func (h *Handler) handleHealthz(w http.ResponseWriter, r *http.Request) {
	status := "ok"
	if h.ch != nil {
		ctx, cancel := context.WithTimeout(r.Context(), healthzTimeout)
		defer cancel()
		if err := h.ch.Ping(ctx); err != nil {
			status = "degraded"
			h.logger.Warn("ingest: healthz clickhouse ping failed", "error", err)
		}
	}
	h.writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

// handleStatsz implements GET /statsz (spec §6 S2 G1/G2): the ops-counters
// payload the pipeline-health Grafana panel reads — counts only, never row
// or event content.
func (h *Handler) handleStatsz(w http.ResponseWriter, _ *http.Request) {
	stats := h.batch.Stats()
	h.writeJSON(w, http.StatusOK, StatszResponse{
		RowsDroppedTotal:           stats.RowsDroppedTotal,
		InsertFailuresTotal:        stats.InsertFailuresTotal,
		DimsDroppedTotal:           h.dimsDroppedTotal.Load(),
		ForwardCompatAcceptedTotal: h.forwardCompatAcceptedTotal.Load(),
	})
}

// handleEvents implements POST /v1/events (spec §6): blocklist gate, IP
// rate limit, size caps, gzip decode, tolerant per-event validation, dedup,
// per-install/session/new-install-cardinality limits, then hands accepted
// rows to the batcher.
func (h *Handler) handleEvents(w http.ResponseWriter, r *http.Request) {
	now := h.now()
	ip := clientIP(r)

	if h.block.blockedIP(ip) {
		// R5 ops lever (spec §6): a blocked IP is never logged, and the
		// response carries nothing that would let the caller distinguish
		// "blocked" from any other 403 cause.
		h.writeJSON(w, http.StatusForbidden, IngestResponse{})
		return
	}

	if allowed, retryAfter := h.limiter.allowIP(ip, now); !allowed {
		h.writeJSON(w, http.StatusTooManyRequests, IngestResponse{RetryAfterMs: retryAfter.Milliseconds()})
		return
	}

	data, ok := h.readBody(w, r)
	if !ok {
		return // readBody already wrote the error response
	}

	var batch wire.Batch
	if err := json.Unmarshal(data, &batch); err != nil {
		h.writeJSON(w, http.StatusBadRequest, IngestResponse{})
		return
	}
	if batch.ProtocolVersion != wire.ProtocolVersion {
		h.writeJSON(w, http.StatusBadRequest, IngestResponse{})
		return
	}
	if len(batch.Events) > wire.MaxBatchEvents {
		h.writeJSON(w, http.StatusBadRequest, IngestResponse{})
		return
	}

	var resp IngestResponse
	var rows []Row
	for i := range batch.Events {
		e := batch.Events[i]
		if h.block.blockedInstall(e.InstallID) {
			// Same R5 discipline as the IP block above: preserve whatever
			// this request already legitimately accepted, but never process
			// a blocked install further (spec §6).
			h.batch.add(rows...)
			h.writeJSON(w, http.StatusForbidden, IngestResponse{})
			return
		}
		outcome, err := wire.ValidateStrict(&e)
		if err != nil {
			resp.Rejected++
			continue
		}
		h.dimsDroppedTotal.Add(int64(outcome.DimsDropped))
		if outcome.ForwardCompat {
			h.forwardCompatAcceptedTotal.Add(1)
		}
		if !h.limiter.allowNewInstall(ip, e.InstallID, now) {
			// Abuse circuit breaker (spec §6 item 4): 429 the whole
			// request, but keep whatever this request already accepted —
			// that work was legitimately validated and deduped before the
			// cardinality guard tripped.
			h.batch.add(rows...)
			h.writeJSON(w, http.StatusTooManyRequests, resp)
			return
		}
		if h.dedup.seen(e.EventID, now) {
			resp.Duplicate++
			continue
		}
		if !h.limiter.allowInstallDaily(e.InstallID, now) {
			resp.Rejected++
			continue
		}
		if e.EventType == wire.EventToolCall && !h.limiter.allowSessionToolCall(e.SessionID, now) {
			resp.Rejected++
			continue
		}
		row, err := buildRow(e, batch.BatchID, batch.ProtocolVersion, now)
		if err != nil {
			resp.Rejected++
			continue
		}
		rows = append(rows, row)
		resp.Accepted++
	}
	// Release-ordering guard (spec §4.4/§8 S2): tells the client which
	// schema_version THIS ingest currently supports, so a client newer than
	// the deployed ingest can warn once instead of silently drifting.
	resp.MaxSchemaVersion = wire.SchemaVersion
	h.batch.add(rows...)
	h.writeJSON(w, http.StatusAccepted, resp)
}

// readBody enforces the pre/post-decompress size caps (spec §6 item 2) and
// returns the decoded JSON bytes. On any violation it writes the error
// response itself and returns ok=false.
func (h *Handler) readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	var reader io.Reader
	if r.Header.Get("Content-Encoding") == "gzip" {
		capped := http.MaxBytesReader(w, r.Body, maxCompressedBytes)
		gz, err := gzip.NewReader(capped)
		if err != nil {
			h.writeJSON(w, http.StatusBadRequest, IngestResponse{})
			return nil, false
		}
		defer func() { _ = gz.Close() }()
		reader = io.LimitReader(gz, maxDecodedBytes+1)
	} else {
		// No compression: the decoded cap doubles as the wire cap.
		reader = http.MaxBytesReader(w, r.Body, maxDecodedBytes)
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		h.writeJSON(w, http.StatusRequestEntityTooLarge, IngestResponse{})
		return nil, false
	}
	if len(data) > maxDecodedBytes {
		h.writeJSON(w, http.StatusRequestEntityTooLarge, IngestResponse{})
		return nil, false
	}
	return data, true
}

// clientIP extracts the request's IP for rate-limiting ONLY (spec B6/T9:
// never logged, never inserted, kept solely in the in-memory limiter map).
// X-Forwarded-For's first hop wins; falls back to RemoteAddr.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first, _, _ := strings.Cut(xff, ",")
		return strings.TrimSpace(first)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		h.logger.Error("ingest: encode response failed", "error", err)
	}
}
