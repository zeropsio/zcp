package telemetry

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	randv2 "math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zeropsio/zcp/internal/telemetry/wire"
)

// Pipeline tunables (spec §5.1).
const (
	queueCapacity    = 4096
	flushInterval    = 5 * time.Second
	flushMaxEvents   = 100
	flushMaxBytes    = 128 * 1024
	sendTimeout      = 2 * time.Second
	startupJitterMax = 2 * time.Second

	backoffBase = 5 * time.Second
	backoffMax  = 5 * time.Minute

	// shutdownGraceMargin is reserved off the END of a caller-supplied
	// Shutdown ctx for the worker's bounded I/O (shutdownFlush +
	// tryDrainSpool), so the worker reliably finishes and closes doneCh
	// BEFORE the caller's own ctx expires — without this, a fault that
	// legitimately consumes the full budget (e.g. a hung endpoint) makes
	// doneCh close at nearly the same instant ctx.Done() fires, and
	// Shutdown's outer select can pick either at random, non-deterministically
	// surfacing ctx.Err() even though the worker behaved correctly.
	shutdownGraceMargin = 100 * time.Millisecond

	rfc3339Milli = "2006-01-02T15:04:05.000Z07:00"

	// maxResponseBodyBytes bounds how much of the ingest's response body the
	// client reads (spec §4.4 IngestResponse is a handful of ints — 4 KiB is
	// generous headroom, not a real payload budget).
	maxResponseBodyBytes = 4096
)

// Emitter is the seam upper layers (server middleware, CLI) depend on so
// they never need the concrete *Client type. A nil Emitter (typed nil
// *Client or literal nil interface) is always a legal, cheap no-op — spec
// §5.1.
type Emitter interface {
	Emit(wire.Event)
}

// Client is the telemetry emit pipeline: Emit() enqueues (non-blocking),
// ONE worker goroutine owns all I/O — batching, HTTP send, spool, retry
// (spec §5.1). Every exported method tolerates a nil receiver.
type Client struct {
	cfg   Config
	queue chan wire.Event

	seq           atomic.Uint64
	queueDropped  atomic.Int64
	panicDisabled atomic.Bool

	storm    *stormGuard
	spoolMgr *spool

	httpClient *http.Client
	stderr     io.Writer

	// backoffAttempts/backoffUntil are owned exclusively by the worker
	// goroutine — no lock needed (single-writer, single-reader).
	backoffAttempts int
	backoffUntil    time.Time

	closeCh   chan context.Context
	doneCh    chan struct{}
	closeOnce sync.Once

	// schemaWarnOnce ensures the release-ordering guard's warning (spec §8
	// "backend deploys before client releases" — S2's visibility for
	// getting that order wrong) fires at most once per process even if
	// many batches see a server behind this client's schema_version.
	schemaWarnOnce sync.Once
}

var _ Emitter = (*Client)(nil)

// New constructs a Client from a resolved Config. Always returns non-nil.
// When cfg.Enabled is false, the returned Client is a cheap shell: no
// worker goroutine, no channel, no spool — every method short-circuits on
// cfg.Enabled. Separately, every method also tolerates a nil *Client
// receiver (spec §5.1 "nil-safe: a nil *Client is also a no-op").
func New(cfg Config) *Client {
	return newClient(cfg, os.Stderr, "")
}

// newClient is New's fully-injectable constructor (stderr sink + spool
// directory), used directly by tests in this package. spoolDir == ""
// resolves the real ~/.zcp/telemetry/spool-v1/ default.
func newClient(cfg Config, stderr io.Writer, spoolDir string) *Client {
	c := &Client{cfg: cfg, stderr: stderr}
	if !cfg.Enabled {
		return c
	}
	c.queue = make(chan wire.Event, queueCapacity)
	c.storm = newStormGuard(time.Now())
	c.httpClient = &http.Client{Timeout: sendTimeout}
	c.closeCh = make(chan context.Context, 1)
	c.doneCh = make(chan struct{})

	dir := spoolDir
	if dir == "" {
		resolved, err := defaultSpoolDir()
		if err != nil {
			fmt.Fprintf(c.stderr, "telemetry: resolve spool dir: %v\n", err)
		} else {
			dir = resolved
		}
	}
	if dir != "" {
		sp, err := newSpool(dir)
		if err != nil {
			fmt.Fprintf(c.stderr, "telemetry: spool init: %v\n", err)
		} else {
			c.spoolMgr = sp
		}
	}

	go c.runWorker()
	return c
}

// defaultSpoolDir returns ~/.zcp/telemetry/spool-v1/ (spec §5.4).
func defaultSpoolDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".zcp", telemetryDirName, "spool-v1"), nil
}

// Emit lite-validates, assigns seq/event_id, and non-blocking sends into
// the bounded queue (spec §5.1). Full queue → drop newest + count. A panic
// anywhere in this path is recovered and permanently disables further
// emits for the process (spec §5.1 B3).
func (c *Client) Emit(e wire.Event) {
	if c == nil || !c.cfg.Enabled || c.panicDisabled.Load() {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(c.stderr, "telemetry: emit panic: %v\n", r)
			c.panicDisabled.Store(true)
		}
	}()

	c.fillEnvelope(&e)

	if isStormGoverned(e.EventType) {
		now := time.Now()
		if !c.storm.allow(now) {
			if delta, due := c.storm.recordDrop(now); due {
				c.enqueueThrottle(delta)
			}
			return
		}
	}

	if err := wire.ValidateLite(&e); err != nil {
		fmt.Fprintf(c.stderr, "telemetry: dropping invalid event: %v\n", err)
		return
	}

	select {
	case c.queue <- e:
	default:
		c.queueDropped.Add(1)
	}
}

// fillEnvelope stamps every context/identity field owned by the client
// (spec §4.2), including the monotonically-assigned seq/event_id.
func (c *Client) fillEnvelope(e *wire.Event) {
	seq := c.seq.Add(1)
	e.Seq = seq
	e.SchemaVersion = wire.SchemaVersion
	e.InstallID = c.cfg.InstallID
	e.SessionID = c.cfg.SessionID
	e.EventID = c.cfg.SessionID + ":" + strconv.FormatUint(seq, 10)
	if e.ClientTime == "" {
		e.ClientTime = time.Now().UTC().Format(rfc3339Milli)
	}
	e.ZcpVersion = c.cfg.ZcpVersion
	e.OS = c.cfg.OS
	e.Arch = c.cfg.Arch
	e.RuntimeEnv = c.cfg.RuntimeEnv
	e.UsageChannel = c.cfg.Channel
}

// enqueueThrottle builds and enqueues a client_throttle event (exempt from
// the storm guard itself — spec §5.2).
func (c *Client) enqueueThrottle(delta int64) {
	e := wire.Event{EventType: wire.EventClientThrottle, DroppedCount: delta}
	c.fillEnvelope(&e)
	if err := wire.ValidateLite(&e); err != nil {
		fmt.Fprintf(c.stderr, "telemetry: dropping invalid throttle event: %v\n", err)
		return
	}
	select {
	case c.queue <- e:
	default:
		c.queueDropped.Add(1)
	}
}

// DroppedCount returns the total events dropped so far this process — queue
// overflow plus storm-guard drops combined (spec §5.1/§5.2 "count"). S4
// stamps this into session_end's dropped_count.
func (c *Client) DroppedCount() int64 {
	if c == nil {
		return 0
	}
	total := c.queueDropped.Load()
	if c.storm != nil {
		total += c.storm.totalDropped()
	}
	return total
}

// Shutdown signals the worker to stop, drains whatever is currently queued,
// attempts exactly ONE send within ctx, and spools any leftovers on failure
// (spec §5.5). Safe to call more than once; safe on a nil/disabled client.
func (c *Client) Shutdown(ctx context.Context) error {
	if c == nil || !c.cfg.Enabled {
		return nil
	}
	c.closeOnce.Do(func() {
		c.closeCh <- ctx
	})
	select {
	case <-c.doneCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// runWorker is the single goroutine that owns all telemetry I/O: batching,
// HTTP sends, spool writes/drains, backoff. A panic here is recovered and
// permanently disables Emit for the rest of the process (spec §5.1).
func (c *Client) runWorker() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(c.stderr, "telemetry: worker panic: %v\n", r)
			c.panicDisabled.Store(true)
		}
		close(c.doneCh)
	}()

	// Startup jitter (spec §5.1): gates real SENDS only, never the ability
	// to keep draining the queue into an in-memory batch, and never the
	// worker's responsiveness to closeCh (Shutdown's one send attempt
	// ignores this gate entirely).
	ready := c.cfg.Debug // debug mode has no network path — nothing to jitter
	var jitterTimer <-chan time.Time
	if !ready {
		jitterTimer = time.After(randv2.N(startupJitterMax)) //nolint:gosec // G404: startup jitter, not security-sensitive
	}

	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	var batch []wire.Event
	var batchBytes int

	for {
		select {
		case e, ok := <-c.queue:
			if !ok {
				return
			}
			if c.cfg.Debug {
				c.writeDebugLine(e)
				continue
			}
			batch = append(batch, e)
			batchBytes += approxEventBytes(e)
			if ready && (len(batch) >= flushMaxEvents || batchBytes >= flushMaxBytes) {
				batch, batchBytes = c.flushBatch(context.Background(), batch), 0
			}
		case <-jitterTimer:
			ready = true
			jitterTimer = nil
		case <-ticker.C:
			if ready {
				batch, batchBytes = c.flushBatch(context.Background(), batch), 0
				c.tryDrainSpool(context.Background())
			}
		case shutdownCtx := <-c.closeCh:
			remaining := drainNonBlocking(c.queue)
			if c.cfg.Debug {
				// Debug-mode events dequeued via the normal case above are
				// already written; anything still sitting in the channel at
				// shutdown time needs the same treatment here — never
				// silently dropped just because shutdown raced ahead of the
				// per-event dequeue loop.
				for _, e := range remaining {
					c.writeDebugLine(e)
				}
				return
			}
			batch = append(batch, remaining...)
			// innerCtx reserves shutdownGraceMargin so the worker finishes
			// (and Shutdown observes doneCh) before shutdownCtx itself
			// expires, even when the send attempt below legitimately uses
			// its entire allotment.
			innerCtx, innerCancel := trimmedDeadline(shutdownCtx, shutdownGraceMargin)
			c.shutdownFlush(innerCtx, batch)
			// G7: a short-lived CLI one-shot process never lives long
			// enough for the periodic 5s ticker (tryDrainSpool's only
			// other call site) to fire even once — without an attempt
			// here too, its spool backlog would never get a chance to
			// drain and would just age out at the 7-day bound instead
			// (spec §5.5). Bounded by the same trimmed budget as the
			// flush above — best-effort, never blocks process exit.
			c.tryDrainSpool(innerCtx)
			innerCancel()
			return
		}
	}
}

// shutdownFlush is Shutdown's "one send attempt" (spec §5.5) — unlike
// flushBatch, it ignores the startup-jitter/backoff gates entirely: a
// process exiting cannot wait out a multi-minute backoff.
func (c *Client) shutdownFlush(ctx context.Context, batch []wire.Event) {
	if len(batch) == 0 {
		return
	}
	switch c.sendBatch(ctx, batch) {
	case sendOK:
	case sendPermanentReject:
		fmt.Fprintf(c.stderr, "telemetry: shutdown batch rejected (permanent), dropping %d events\n", len(batch))
	case sendRetryable:
		c.spoolBatch(batch)
	}
}

// flushBatch sends batch (respecting the backoff gate — a backing-off
// worker spools instead of attempting a doomed send) and always returns the
// post-flush (empty) batch state.
func (c *Client) flushBatch(ctx context.Context, batch []wire.Event) []wire.Event {
	if len(batch) == 0 {
		return batch
	}
	if time.Now().Before(c.backoffUntil) {
		c.spoolBatch(batch)
		return nil
	}
	sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
	outcome := c.sendBatch(sendCtx, batch)
	cancel()
	switch outcome {
	case sendOK:
		c.backoffAttempts = 0
	case sendPermanentReject:
		fmt.Fprintf(c.stderr, "telemetry: batch rejected (permanent), dropping %d events\n", len(batch))
	case sendRetryable:
		c.spoolBatch(batch)
		c.backoffAttempts++
		c.backoffUntil = time.Now().Add(jitteredBackoff(c.backoffAttempts))
	}
	return nil
}

// tryDrainSpool attempts to send ONE oldest spool segment per call — bounded
// work per flush cycle so spool draining never starves the live queue (spec
// §5.4 "without starving the live queue").
func (c *Client) tryDrainSpool(ctx context.Context) {
	if c.spoolMgr == nil || time.Now().Before(c.backoffUntil) {
		return
	}
	events, segPath, ok, err := c.spoolMgr.oldest()
	if err != nil {
		fmt.Fprintf(c.stderr, "telemetry: spool read: %v\n", err)
		return
	}
	if !ok {
		return
	}
	sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
	outcome := c.sendBatch(sendCtx, events)
	cancel()
	switch outcome {
	case sendOK, sendPermanentReject:
		if err := c.spoolMgr.remove(segPath); err != nil {
			fmt.Fprintf(c.stderr, "telemetry: spool remove: %v\n", err)
		}
	case sendRetryable:
		c.backoffAttempts++
		c.backoffUntil = time.Now().Add(jitteredBackoff(c.backoffAttempts))
	}
}

func (c *Client) spoolBatch(batch []wire.Event) {
	if c.spoolMgr == nil || len(batch) == 0 {
		return
	}
	if err := c.spoolMgr.write(batch); err != nil {
		fmt.Fprintf(c.stderr, "telemetry: spool write: %v\n", err)
	}
}

func (c *Client) writeDebugLine(e wire.Event) {
	b, err := json.Marshal(e)
	if err != nil {
		fmt.Fprintf(c.stderr, "telemetry: debug marshal: %v\n", err)
		return
	}
	// Debug-mode output is best-effort diagnostics; there is no further sink
	// to report a write failure to.
	_, _ = c.stderr.Write(append(b, '\n'))
}

// sendOutcome classifies an HTTP send attempt per spec §4.4 client retry
// semantics.
type sendOutcome int

const (
	sendOK sendOutcome = iota
	// sendPermanentReject: 4xx (400/413) — drop the batch, never retry.
	sendPermanentReject
	// sendRetryable: 429/5xx/timeout/network error — spool + back off.
	sendRetryable
)

// sendBatch POSTs events as one gzip-encoded wire.Batch (spec §4.1).
func (c *Client) sendBatch(ctx context.Context, events []wire.Event) sendOutcome {
	batch := wire.Batch{
		ProtocolVersion: wire.ProtocolVersion,
		BatchID:         c.newBatchID(),
		SentAt:          time.Now().UTC().Format(rfc3339Milli),
		Events:          events,
	}
	body, err := json.Marshal(batch)
	if err != nil {
		fmt.Fprintf(c.stderr, "telemetry: marshal batch: %v\n", err)
		return sendPermanentReject
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(body); err != nil {
		fmt.Fprintf(c.stderr, "telemetry: gzip batch: %v\n", err)
		return sendPermanentReject
	}
	if err := gz.Close(); err != nil {
		fmt.Fprintf(c.stderr, "telemetry: gzip close: %v\n", err)
		return sendPermanentReject
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.Endpoint, &buf)
	if err != nil {
		return sendRetryable
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return sendRetryable // network error / timeout
	}
	defer resp.Body.Close()
	// respBody is small (the ingest's IngestResponse envelope, spec §4.4);
	// LimitReader bounds it defensively, and the trailing Copy still drains
	// any remainder so the connection is always reusable regardless.
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyBytes))
	_, _ = io.Copy(io.Discard, resp.Body)

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		c.checkSchemaWarning(respBody)
		return sendOK
	case resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusRequestEntityTooLarge:
		return sendPermanentReject
	default: // 429, 5xx, anything else unexpected
		return sendRetryable
	}
}

// checkSchemaWarning is the release-ordering guard's client side (spec §8
// S2): body is the raw 2xx response. If the server's advertised
// max_schema_version is behind this client's own wire.SchemaVersion, warn
// ONCE per process — visibility for the "deployed the client before the
// ingest" mistake. Best-effort: a malformed/empty body never surfaces an
// error or blocks (spec B3).
func (c *Client) checkSchemaWarning(body []byte) {
	if len(body) == 0 {
		return
	}
	var resp wire.Response
	if err := json.Unmarshal(body, &resp); err != nil {
		return
	}
	if resp.MaxSchemaVersion >= wire.SchemaVersion {
		return
	}
	c.schemaWarnOnce.Do(func() {
		fmt.Fprintf(c.stderr, "telemetry: ingest supports schema_version up to %d, this client sends %d — "+
			"deploy ingest before releasing this client version\n", resp.MaxSchemaVersion, wire.SchemaVersion)
	})
}

func (c *Client) newBatchID() string {
	id, err := newUUIDv4()
	if err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return id
}

// jitteredBackoff implements full-jitter exponential backoff (spec §5.1:
// 5s → 5min): sleep = random_between(0, min(cap, base*2^(attempt-1))).
func jitteredBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := backoffBase
	for i := 1; i < attempt; i++ {
		if d >= backoffMax {
			d = backoffMax
			break
		}
		d *= 2
	}
	if d > backoffMax {
		d = backoffMax
	}
	if d <= 0 {
		return 0
	}
	return randv2.N(d) //nolint:gosec // G404: backoff jitter, not security-sensitive
}

// approxEventBytes measures an event's encoded size for the 128 KiB flush
// trigger (spec §5.1).
func approxEventBytes(e wire.Event) int {
	b, err := json.Marshal(e)
	if err != nil {
		return 0
	}
	return len(b)
}

// drainNonBlocking pulls everything currently buffered in queue without
// blocking — used by the shutdown path to grab whatever's left (spec §5.5).
func drainNonBlocking(queue chan wire.Event) []wire.Event {
	var out []wire.Event
	for {
		select {
		case e, ok := <-queue:
			if !ok {
				return out
			}
			out = append(out, e)
		default:
			return out
		}
	}
}

// trimmedDeadline derives a context whose deadline is margin earlier than
// parent's (when parent has a deadline at all), reserving headroom so
// bounded work done against the returned context finishes — and any
// completion signal derived from it fires — before parent itself expires.
// If margin would consume the entire remaining budget (or parent has no
// deadline), it falls back to parent's own bound unchanged rather than
// manufacturing an already-expired context.
func trimmedDeadline(parent context.Context, margin time.Duration) (context.Context, context.CancelFunc) {
	deadline, ok := parent.Deadline()
	if !ok {
		return context.WithCancel(parent)
	}
	trimmed := deadline.Add(-margin)
	if !trimmed.After(time.Now()) {
		return context.WithCancel(parent)
	}
	return context.WithDeadline(parent, trimmed)
}
