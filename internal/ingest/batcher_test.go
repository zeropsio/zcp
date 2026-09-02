package ingest

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// fakeInserter is a chInserter test double that records every Insert call.
type fakeInserter struct {
	mu      sync.Mutex
	batches [][]Row
	err     error
	calls   int
}

func (f *fakeInserter) Insert(_ context.Context, rows []Row) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return f.err
	}
	cp := make([]Row, len(rows))
	copy(cp, rows)
	f.batches = append(f.batches, cp)
	return nil
}

func (f *fakeInserter) snapshot() ([][]Row, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]Row, len(f.batches))
	copy(out, f.batches)
	return out, f.calls
}

// setErr changes the error Insert returns for subsequent calls — used to
// simulate ClickHouse recovering mid-test (spec S2 G1 retry tests).
func (f *fakeInserter) setErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestBatcher_FlushesOnSizeThreshold(t *testing.T) {
	t.Parallel()

	fi := &fakeInserter{}
	b := newBatcher(fi, testLogger(), 3, time.Hour) // interval huge: only size triggers
	go b.run()
	defer func() { _ = b.stop(context.Background()) }()

	b.add(Row{}, Row{}, Row{})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, calls := fi.snapshot(); calls >= 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("expected a flush triggered by the size threshold")
}

func TestBatcher_FlushesOnInterval(t *testing.T) {
	t.Parallel()

	fi := &fakeInserter{}
	b := newBatcher(fi, testLogger(), 10_000, 20*time.Millisecond)
	go b.run()
	defer func() { _ = b.stop(context.Background()) }()

	b.add(Row{})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if batches, _ := fi.snapshot(); len(batches) == 1 && len(batches[0]) == 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("expected a flush triggered by the interval ticker")
}

func TestBatcher_StopFlushesRemainder(t *testing.T) {
	t.Parallel()

	fi := &fakeInserter{}
	b := newBatcher(fi, testLogger(), 10_000, time.Hour)
	go b.run()

	b.add(Row{}, Row{})

	if err := b.stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}

	batches, _ := fi.snapshot()
	total := 0
	for _, batch := range batches {
		total += len(batch)
	}
	if total != 2 {
		t.Errorf("total rows flushed on stop = %d, want 2", total)
	}
}

func TestBatcher_StopIsIdempotent(t *testing.T) {
	t.Parallel()

	fi := &fakeInserter{}
	b := newBatcher(fi, testLogger(), 10_000, time.Hour)
	go b.run()

	if err := b.stop(context.Background()); err != nil {
		t.Fatalf("first stop: %v", err)
	}
	if err := b.stop(context.Background()); err != nil {
		t.Fatalf("second stop: %v", err)
	}
}

func TestBatcher_InsertErrorDoesNotPanic(t *testing.T) {
	t.Parallel()

	fi := &fakeInserter{err: errors.New("connection refused")}
	b := newBatcher(fi, testLogger(), 1, time.Hour)
	go b.run()

	b.add(Row{})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, calls := fi.snapshot(); calls >= 1 {
			_ = b.stop(context.Background())
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("expected the insert attempt despite the eventual error")
}

func TestBatcher_EmptyStopDoesNotCallInsert(t *testing.T) {
	t.Parallel()

	fi := &fakeInserter{}
	b := newBatcher(fi, testLogger(), 10_000, time.Hour)
	go b.run()

	if err := b.stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if _, calls := fi.snapshot(); calls != 0 {
		t.Errorf("Insert called %d times on an empty batcher, want 0", calls)
	}
}

// --- S2 G1: bounded failed-batch retry -------------------------------------

func TestBatcher_FailedInsertRetriedOnNextFlush(t *testing.T) {
	t.Parallel()

	fi := &fakeInserter{err: errors.New("connection refused")}
	b := newBatcher(fi, testLogger(), 10_000, time.Hour) // interval huge: only flushNow triggers
	go b.run()
	defer func() { _ = b.stop(context.Background()) }()

	b.add(Row{})
	b.flushNow <- struct{}{}

	// Wait on the counter this asserts, NOT on the fake's call count: flush()
	// increments insertFailuresTotal only AFTER Insert returns and after a
	// logger.Error call, so `calls >= 1` goes true inside a window where the
	// counter is still 0. On a loaded runner the flush goroutine gets
	// preempted in that window and the assert reads 0 (v9.161.0 release run).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b.Stats().InsertFailuresTotal >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if stats := b.Stats(); stats.InsertFailuresTotal != 1 {
		t.Fatalf("InsertFailuresTotal = %d, want 1 after the first failed attempt", stats.InsertFailuresTotal)
	}

	fi.setErr(nil) // ClickHouse recovers
	b.flushNow <- struct{}{}

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if batches, calls := fi.snapshot(); calls >= 2 && len(batches) == 1 && len(batches[0]) == 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("expected the previously failed row to be retried and inserted on the next successful flush")
}

func TestBatcher_RetryBufferBoundedDropsOldestWithCounter(t *testing.T) {
	t.Parallel()

	fi := &fakeInserter{err: errors.New("connection refused")}
	b := newBatcher(fi, testLogger(), 10_000, time.Hour)
	go b.run()
	defer func() { _ = b.stop(context.Background()) }()

	const overflow = 500
	rows := make([]Row, retryBufferCap+overflow)
	b.add(rows...)
	b.flushNow <- struct{}{}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b.Stats().RowsDroppedTotal == overflow {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := b.Stats().RowsDroppedTotal; got != overflow {
		t.Fatalf("RowsDroppedTotal = %d, want %d", got, overflow)
	}

	fi.setErr(nil)
	b.flushNow <- struct{}{}

	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if batches, _ := fi.snapshot(); len(batches) == 1 && len(batches[0]) == retryBufferCap {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("expected exactly %d retried rows after the bound dropped the oldest %d", retryBufferCap, overflow)
}

func TestBatcher_StatsDefaultZero(t *testing.T) {
	t.Parallel()

	fi := &fakeInserter{}
	b := newBatcher(fi, testLogger(), 10_000, time.Hour)
	go b.run()
	defer func() { _ = b.stop(context.Background()) }()

	stats := b.Stats()
	if stats.RowsDroppedTotal != 0 || stats.InsertFailuresTotal != 0 {
		t.Errorf("Stats() = %+v, want all-zero for a fresh batcher", stats)
	}
}
