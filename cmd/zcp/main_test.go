package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/telemetry/wire"
)

// nopCloser wraps a Writer with a no-op Close for testing logShutdown.
type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }

// recordingEmitter is an in-memory telemetry.Emitter for asserting the
// session_end event logShutdown emits (spec §5.5).
type recordingEmitter struct {
	events []wire.Event
}

func (r *recordingEmitter) Emit(e wire.Event) { r.events = append(r.events, e) }

func TestLogShutdown_Categories(t *testing.T) {
	t.Parallel()
	startedAt := time.Now().Add(-5 * time.Minute)

	tests := []struct {
		name    string
		err     error
		wantSub string
	}{
		{"nil is client disconnected", nil, "client disconnected"},
		{"context.Canceled is signal", context.Canceled, "signal"},
		{"io.EOF is stdin closed", io.EOF, "stdin closed"},
		{"EPIPE is broken pipe", syscall.EPIPE, "broken pipe"},
		{"wrapped EOF propagates", errors.Join(io.EOF), "stdin closed"},
		{"unknown error shown verbatim", errors.New("kaboom"), "kaboom"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			logShutdown(nopCloser{&buf}, tt.err, startedAt, nil, nil, 0)
			got := buf.String()
			if !strings.Contains(got, tt.wantSub) {
				t.Errorf("logShutdown(%v) = %q, want substring %q", tt.err, got, tt.wantSub)
			}
			if !strings.Contains(got, "shutdown:") {
				t.Errorf("logShutdown should always contain 'shutdown:', got %q", got)
			}
		})
	}
}

func TestLogShutdown_IncludesUptime(t *testing.T) {
	t.Parallel()
	startedAt := time.Now().Add(-5 * time.Minute)
	var buf bytes.Buffer
	logShutdown(nopCloser{&buf}, nil, startedAt, nil, nil, 0)
	got := buf.String()

	if !strings.Contains(got, "uptime=5m") {
		t.Errorf("should include uptime, got: %q", got)
	}
	if !strings.Contains(got, "calls=0") {
		t.Errorf("nil server should show calls=0, got: %q", got)
	}
}

func TestLogShutdown_NilWriter(t *testing.T) {
	t.Parallel()
	// Must not panic with nil writer.
	logShutdown(nil, nil, time.Now(), nil, nil, 0)
}

func TestLogShutdown_NilEmitter_NoPanic(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	// Must not panic with a nil Emitter (spec: nil Emitter is always a
	// legal, cheap no-op).
	logShutdown(nopCloser{&buf}, nil, time.Now(), nil, nil, 0)
}

func TestLogShutdown_EmitsSessionEndWithShutdownReasonAndDroppedCount(t *testing.T) {
	t.Parallel()
	startedAt := time.Now().Add(-90 * time.Second)

	tests := []struct {
		name         string
		err          error
		wantShutdown string
	}{
		{"nil is client disconnect", nil, wire.ShutdownClientDisconnect},
		{"context.Canceled is signal", context.Canceled, wire.ShutdownSignal},
		{"io.EOF is stdin closed", io.EOF, wire.ShutdownStdinClosed},
		{"EPIPE is broken pipe", syscall.EPIPE, wire.ShutdownBrokenPipe},
		{"unknown error", errors.New("kaboom"), wire.ShutdownError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := &recordingEmitter{}
			var buf bytes.Buffer
			logShutdown(nopCloser{&buf}, tt.err, startedAt, nil, rec, 42)

			if len(rec.events) != 1 {
				t.Fatalf("got %d events, want exactly 1", len(rec.events))
			}
			e := rec.events[0]
			if e.EventType != wire.EventSessionEnd {
				t.Errorf("EventType = %q, want %q", e.EventType, wire.EventSessionEnd)
			}
			if e.Dims[wire.DimShutdownReason] != tt.wantShutdown {
				t.Errorf("dims[shutdown_reason] = %q, want %q", e.Dims[wire.DimShutdownReason], tt.wantShutdown)
			}
			if e.DroppedCount != 42 {
				t.Errorf("DroppedCount = %d, want 42", e.DroppedCount)
			}
			if e.DurationMs < 89_000 {
				t.Errorf("DurationMs = %d, want >= ~90000 (uptime since startedAt)", e.DurationMs)
			}
		})
	}
}
