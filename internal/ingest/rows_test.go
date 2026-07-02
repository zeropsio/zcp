package ingest

import (
	"testing"
	"time"
)

func TestClampEventTime_WithinWindow_UsesClientTime(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	clientTime := now.Add(-1 * time.Hour)

	eventTime, skewMs := clampEventTime(clientTime, now)

	if !eventTime.Equal(clientTime) {
		t.Errorf("eventTime = %v, want client_time %v", eventTime, clientTime)
	}
	wantSkew := int64(time.Hour / time.Millisecond)
	if skewMs != wantSkew {
		t.Errorf("skewMs = %d, want %d", skewMs, wantSkew)
	}
}

func TestClampEventTime_TooOld_FallsBackToReceivedAt(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	clientTime := now.Add(-31 * 24 * time.Hour)

	eventTime, _ := clampEventTime(clientTime, now)

	if !eventTime.Equal(now) {
		t.Errorf("eventTime = %v, want received_at fallback %v", eventTime, now)
	}
}

func TestClampEventTime_TooFarFuture_FallsBackToReceivedAt(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	clientTime := now.Add(31 * 24 * time.Hour)

	eventTime, _ := clampEventTime(clientTime, now)

	if !eventTime.Equal(now) {
		t.Errorf("eventTime = %v, want received_at fallback %v", eventTime, now)
	}
}

func TestClampEventTime_ExactlyAtBoundary_UsesClientTime(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	clientTime := now.Add(-30 * 24 * time.Hour)

	eventTime, _ := clampEventTime(clientTime, now)

	if !eventTime.Equal(clientTime) {
		t.Errorf("eventTime = %v, want client_time %v at exact boundary", eventTime, clientTime)
	}
}

func TestClampEventTime_FutureClientTime_NegativeSkew(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	clientTime := now.Add(5 * time.Second)

	_, skewMs := clampEventTime(clientTime, now)

	if skewMs != -5000 {
		t.Errorf("skewMs = %d, want -5000", skewMs)
	}
}
