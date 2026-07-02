package ingest

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Batcher tunables (spec §6 item 5): flush at 5k rows or 2 s, whichever
// comes first.
const (
	batchFlushRows     = 5000
	batchFlushInterval = 2 * time.Second
)

// retryBufferCap bounds the batcher's failed-insert retry buffer (spec S2
// G1): a downed ClickHouse must not grow ingest's memory unboundedly. Once
// full, the OLDEST rows are dropped to make room for the newest, counted in
// rowsDroppedTotal so the loss is visible instead of silent.
const retryBufferCap = 50_000

// chInserter is the seam the batcher flushes through — the real
// implementation (clickhouse.go) uses clickhouse-go v2's native protocol;
// tests use a fake so no live ClickHouse is required (spec §6 item 5).
type chInserter interface {
	Insert(ctx context.Context, rows []Row) error
}

// batcher buffers accepted rows in memory and flushes them to a chInserter
// on a size or time threshold, or on stop (graceful shutdown, spec §6 item
// 5). One batcher per ingest process; add() is safe for concurrent callers
// (HTTP handler goroutines), run() owns the single flush goroutine.
type batcher struct {
	ch     chInserter
	logger *slog.Logger

	flushRows     int
	flushInterval time.Duration

	mu   sync.Mutex
	rows []Row

	// retry holds rows from the previous flush's failed insert, retried on
	// the NEXT flush ahead of freshly buffered rows (spec S2 G1). Touched
	// only inside run()'s single goroutine — no lock needed, same
	// single-writer/single-reader discipline as the rest of run's state.
	retry []Row

	rowsDroppedTotal    atomic.Int64
	insertFailuresTotal atomic.Int64

	flushNow chan struct{}
	stopCh   chan struct{}
	doneCh   chan struct{}
	stopOnce sync.Once
}

func newBatcher(ch chInserter, logger *slog.Logger, flushRows int, flushInterval time.Duration) *batcher {
	return &batcher{
		ch:            ch,
		logger:        logger,
		flushRows:     flushRows,
		flushInterval: flushInterval,
		flushNow:      make(chan struct{}, 1),
		stopCh:        make(chan struct{}),
		doneCh:        make(chan struct{}),
	}
}

// add appends rows to the in-memory buffer. When the buffer reaches
// flushRows it nudges run's loop to flush immediately rather than waiting
// for the next tick.
func (b *batcher) add(rows ...Row) {
	if len(rows) == 0 {
		return
	}
	b.mu.Lock()
	b.rows = append(b.rows, rows...)
	full := len(b.rows) >= b.flushRows
	b.mu.Unlock()
	if full {
		select {
		case b.flushNow <- struct{}{}:
		default:
		}
	}
}

// run is the single owner of flush timing. Call in its own goroutine; it
// returns once stop() has flushed the final remainder. run owns no
// lifecycle context of its own — stop() is the sole termination
// mechanism — so every flush uses a fresh background context (ClickHouse
// I/O is decoupled from any caller's request/server lifecycle, spec §6
// item 5).
func (b *batcher) run() {
	defer close(b.doneCh)

	ticker := time.NewTicker(b.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-b.stopCh:
			b.flush()
			return
		case <-b.flushNow:
			b.flush()
		case <-ticker.C:
			b.flush()
		}
	}
}

// flush drains the current buffer, prepends any rows left over from a
// previous failed insert (spec S2 G1's bounded retry), and inserts on a
// fresh background context (see run's doc comment). Insert errors are
// logged, counted, and the whole attempted batch is queued for the NEXT
// flush's retry — a downed ClickHouse must never crash the ingest process
// (mirrors the client-side "telemetry never blocks/breaks" discipline,
// applied here to the server side: ingest keeps accepting HTTP even if the
// sink is unreachable) NOR silently drop rows it could have retried.
func (b *batcher) flush() {
	b.mu.Lock()
	rows := b.rows
	b.rows = nil
	b.mu.Unlock()

	if len(b.retry) > 0 {
		rows = append(b.retry, rows...)
		b.retry = nil
	}
	if len(rows) == 0 {
		return
	}

	if err := b.ch.Insert(context.Background(), rows); err != nil {
		b.logger.Error("ingest: clickhouse insert failed", "rows", len(rows), "error", err)
		b.insertFailuresTotal.Add(1)
		b.requeueForRetry(rows)
	}
}

// requeueForRetry keeps failed rows for the next flush attempt (spec S2
// G1), bounded to retryBufferCap: when the combined size would exceed it,
// the OLDEST rows are dropped first, logged and counted in
// rowsDroppedTotal — a downed ClickHouse must lose visibility of the drop,
// never the drop itself silently.
func (b *batcher) requeueForRetry(rows []Row) {
	if len(rows) > retryBufferCap {
		dropped := len(rows) - retryBufferCap
		b.rowsDroppedTotal.Add(int64(dropped))
		b.logger.Error("ingest: retry buffer full, dropping oldest rows", "dropped", dropped, "cap", retryBufferCap)
		rows = rows[dropped:]
	}
	b.retry = rows
}

// BatcherStats is the batcher's running ops counters (spec S2 G1), surfaced
// read-only via GET /statsz — counts only, never row content.
type BatcherStats struct {
	RowsDroppedTotal    int64
	InsertFailuresTotal int64
}

// Stats reads the batcher's counters. Safe for concurrent use from any
// goroutine (the statsz HTTP handler) — backed by atomics, never touches
// the rows/retry state run() owns.
func (b *batcher) Stats() BatcherStats {
	return BatcherStats{
		RowsDroppedTotal:    b.rowsDroppedTotal.Load(),
		InsertFailuresTotal: b.insertFailuresTotal.Load(),
	}
}

// stop signals run to flush the remainder and exit, then waits (bounded by
// ctx) for it to finish. Safe to call more than once.
func (b *batcher) stop(ctx context.Context) error {
	b.stopOnce.Do(func() {
		close(b.stopCh)
	})
	select {
	case <-b.doneCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
