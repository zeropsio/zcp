package ingest

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
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

// healthzCacheTTL bounds how often GET /healthz actually pings ClickHouse:
// at most one ping per TTL regardless of request volume, so a public flood of
// /healthz cannot amplify into a per-request ClickHouse-ping DoS (S4
// public-ingress hardening).
const healthzCacheTTL = 5 * time.Second

// IngestResponse is the per-request accept/reject/duplicate/retry response
// (spec §4.4). wire.Response is the single owner of this shape — both sides
// of the ingest/client boundary share it, never a hand-duplicated copy.
type IngestResponse = wire.Response

// StatszResponse is the tiny ops-counters payload (spec §6 GET /statsz):
// counts only, never row/event content — the pipeline-health Grafana
// panel's data source (spec S2 G1/G2).
type StatszResponse struct {
	EventsAcceptedTotal        int64 `json:"events_accepted_total"`
	EventsRejectedTotal        int64 `json:"events_rejected_total"`
	ServerErrorsTotal          int64 `json:"server_errors_total"`
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
	eventsAcceptedTotal        atomic.Int64
	eventsRejectedTotal        atomic.Int64
	serverErrorsTotal          atomic.Int64

	// healthz ping cache (S4): guards the ClickHouse ping so a /healthz flood
	// pings CH at most once per healthzCacheTTL. Guarded by healthMu; the ping
	// itself runs OUTSIDE the lock (never hold a mutex across I/O).
	healthMu        sync.Mutex
	healthCheckedAt time.Time
	healthStatus    string
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
	// /statsz is rate-limited like /v1/events so the ops/metrics route is not
	// an unbounded public surface (S4). /healthz is deliberately NOT rate
	// limited — platform health checks must always get a fast (cached) 200.
	mux.Handle("GET /statsz", h.rateLimited(http.HandlerFunc(h.handleStatsz)))
	return mux
}

// rateLimited wraps an ops handler with the same per-IP token bucket as
// /v1/events (keyed on the balancer-authoritative clientIP), bounding a public
// flood of a metrics route (S4).
func (h *Handler) rateLimited(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if allowed, retryAfter := h.limiter.allowIP(clientIP(r), h.now()); !allowed {
			h.writeJSON(w, http.StatusTooManyRequests, IngestResponse{RetryAfterMs: retryAfter.Milliseconds()})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) handleHealthz(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, http.StatusOK, map[string]string{"status": h.healthzStatus(r.Context())})
}

// healthzStatus reports "ok"/"degraded", pinging ClickHouse at most once per
// healthzCacheTTL (S4: a /healthz flood must not become a per-request CH
// ping). ch==nil always reports "ok". The ping runs outside healthMu so the
// lock is never held across I/O; a cold-cache burst may briefly race a few
// pings, then the cache is warm.
func (h *Handler) healthzStatus(ctx context.Context) string {
	if h.ch == nil {
		return "ok"
	}
	h.healthMu.Lock()
	if h.healthStatus != "" && h.now().Sub(h.healthCheckedAt) < healthzCacheTTL {
		cached := h.healthStatus
		h.healthMu.Unlock()
		return cached
	}
	h.healthMu.Unlock()

	status := "ok"
	pingCtx, cancel := context.WithTimeout(ctx, healthzTimeout)
	defer cancel()
	if err := h.ch.Ping(pingCtx); err != nil {
		status = "degraded"
		h.logger.Warn("ingest: healthz clickhouse ping failed", "error", err)
	}

	h.healthMu.Lock()
	h.healthStatus = status
	h.healthCheckedAt = h.now()
	h.healthMu.Unlock()
	return status
}

// handleStatsz implements GET /statsz (spec §6 S2 G1/G2): the ops-counters
// payload the pipeline-health Grafana panel reads — counts only, never row
// or event content.
func (h *Handler) handleStatsz(w http.ResponseWriter, _ *http.Request) {
	stats := h.batch.Stats()
	h.writeJSON(w, http.StatusOK, StatszResponse{
		EventsAcceptedTotal:        h.eventsAcceptedTotal.Load(),
		EventsRejectedTotal:        h.eventsRejectedTotal.Load(),
		ServerErrorsTotal:          h.serverErrorsTotal.Load(),
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
			h.countReject(&resp)
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
			h.countReject(&resp)
			continue
		}
		if e.EventType == wire.EventToolCall && !h.limiter.allowSessionToolCall(e.SessionID, now) {
			h.countReject(&resp)
			continue
		}
		row, err := buildRow(e, batch.BatchID, batch.ProtocolVersion, now)
		if err != nil {
			h.countReject(&resp)
			continue
		}
		rows = append(rows, row)
		resp.Accepted++
		h.eventsAcceptedTotal.Add(1)
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

// clientIP extracts the request's IP for rate-limiting and blocklist matching
// ONLY (spec §6, B6/T9: never logged, never inserted, kept solely in the
// in-memory limiter/blocklist maps).
//
// Behind the Zerops L7 balancer the only trustworthy client identity is
// X-Real-IP, which the balancer sets authoritatively — a client-supplied
// X-Real-IP is overwritten, while a client-supplied X-Forwarded-For is
// appended left-most and is therefore spoofable (verified live 2026-07). So
// X-Forwarded-For is ignored entirely, and the trusted value is canonicalised
// through net/netip so a malformed or re-spelled address cannot mint a
// distinct key (equivalent IPv6 spellings collapse to one). When no valid
// X-Real-IP is present — in-project traffic that never crossed the balancer —
// it falls back to the socket peer (RemoteAddr). Trust boundary: the
// per-project VXLAN; a direct in-project caller could still forge X-Real-IP,
// which is acceptable because in-project traffic is already trusted and public
// reach is only via the balancer that overwrites the header.
func clientIP(r *http.Request) string {
	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
		if addr, err := netip.ParseAddr(xri); err == nil {
			return addr.String()
		}
		// malformed X-Real-IP → fall through to the socket peer rather than
		// keying on attacker-shaped bytes.
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return addr.String()
	}
	return host
}

// countReject records one schema/validation rejection on both the per-request
// response and the process-wide /statsz counter (S4 observability — a real
// traffic reject-storm becomes visible in events_rejected_total).
func (h *Handler) countReject(resp *IngestResponse) {
	resp.Rejected++
	h.eventsRejectedTotal.Add(1)
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, v any) {
	if status >= 500 {
		h.serverErrorsTotal.Add(1)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		h.logger.Error("ingest: encode response failed", "error", err)
	}
}
