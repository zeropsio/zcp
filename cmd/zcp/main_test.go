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
)

// nopCloser wraps a Writer with a no-op Close for testing logShutdown.
type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }

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
			logShutdown(nopCloser{&buf}, tt.err, startedAt, nil)
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
	logShutdown(nopCloser{&buf}, nil, startedAt, nil)
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
	logShutdown(nil, nil, time.Now(), nil)
}
