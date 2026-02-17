// Tests for: ops/progress.go — PollProcess with step-up intervals.
package ops

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// processSequencer wraps platform.Mock and overrides GetProcess
// to return a sequence of process states for PollProcess tests.
type processSequencer struct {
	*platform.Mock
	mu       sync.Mutex
	sequence []*platform.Process
	idx      int
}

func (s *processSequencer) GetProcess(_ context.Context, _ string) (*platform.Process, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idx >= len(s.sequence) {
		return s.sequence[len(s.sequence)-1], nil
	}
	p := s.sequence[s.idx]
	s.idx++
	return p, nil
}

func newSequencer(statuses ...string) *processSequencer {
	seq := make([]*platform.Process, len(statuses))
	for i, s := range statuses {
		seq[i] = &platform.Process{
			ID:         "proc-1",
			ActionName: "test",
			Status:     s,
			Created:    "2025-01-01T00:00:00Z",
		}
	}
	return &processSequencer{
		Mock:     platform.NewMock(),
		sequence: seq,
	}
}

func testConfig() pollConfig {
	return pollConfig{
		initialInterval: 1 * time.Millisecond,
		stepUpInterval:  2 * time.Millisecond,
		stepUpAfter:     5 * time.Millisecond,
		timeout:         50 * time.Millisecond,
	}
}

func TestPollProcess_ImmediateFinish(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status string
	}{
		{name: statusFinished, status: statusFinished},
		{name: statusFailed, status: statusFailed},
		{name: "CANCELED", status: statusCanceled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			seq := newSequencer(tt.status)
			ctx := context.Background()

			proc, err := pollProcess(ctx, seq, "proc-1", nil, testConfig())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if proc.Status != tt.status {
				t.Errorf("status = %s, want %s", proc.Status, tt.status)
			}
		})
	}
}

func TestPollProcess_PollThenFinish(t *testing.T) {
	t.Parallel()

	seq := newSequencer("PENDING", "RUNNING", statusFinished)
	ctx := context.Background()

	proc, err := pollProcess(ctx, seq, "proc-1", nil, testConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proc.Status != statusFinished {
		t.Errorf("status = %s, want FINISHED", proc.Status)
	}
}

func TestPollProcess_Failed(t *testing.T) {
	t.Parallel()

	reason := "build error"
	s := newSequencer("PENDING", statusFailed)
	s.sequence[1].FailReason = &reason
	ctx := context.Background()

	proc, err := pollProcess(ctx, s, "proc-1", nil, testConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proc.Status != statusFailed {
		t.Errorf("status = %s, want FAILED", proc.Status)
	}
	if proc.FailReason == nil || *proc.FailReason != reason {
		t.Errorf("failReason = %v, want %q", proc.FailReason, reason)
	}
}

func TestPollProcess_ContextCanceled(t *testing.T) {
	t.Parallel()

	// Always returns PENDING — never terminates on its own.
	seq := newSequencer("PENDING")
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay.
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	_, err := pollProcess(ctx, seq, "proc-1", nil, testConfig())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != context.Canceled {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

func TestPollProcess_CallbackCalled(t *testing.T) {
	t.Parallel()

	seq := newSequencer("PENDING", "RUNNING", statusFinished)
	ctx := context.Background()

	var mu sync.Mutex
	var calls []string
	cb := func(message string, progress, total float64) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, message)
		if total != 100 {
			t.Errorf("total = %v, want 100", total)
		}
	}

	proc, err := pollProcess(ctx, seq, "proc-1", cb, testConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proc.Status != statusFinished {
		t.Errorf("status = %s, want FINISHED", proc.Status)
	}

	mu.Lock()
	defer mu.Unlock()
	// One callback per GetProcess call: PENDING, RUNNING, FINISHED = 3.
	if len(calls) != 3 {
		t.Errorf("callback called %d times, want 3", len(calls))
	}
}

func TestPollProcess_NilCallback(t *testing.T) {
	t.Parallel()

	seq := newSequencer(statusFinished)
	ctx := context.Background()

	// Must not panic with nil callback.
	proc, err := pollProcess(ctx, seq, "proc-1", nil, testConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proc.Status != statusFinished {
		t.Errorf("status = %s, want FINISHED", proc.Status)
	}
}

func TestPollProcess_Timeout(t *testing.T) {
	t.Parallel()

	seq := newSequencer("PENDING") // Never terminates.
	ctx := context.Background()

	cfg := testConfig()
	cfg.timeout = 10 * time.Millisecond

	_, err := pollProcess(ctx, seq, "proc-1", nil, cfg)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}

	var pe *platform.PlatformError
	if !errorAs(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrAPITimeout {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrAPITimeout)
	}
}

// TestPollProcess_GetProcessError verifies that API errors are propagated.
func TestPollProcess_GetProcessError(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithError("GetProcess", fmt.Errorf("connection refused"))
	ctx := context.Background()

	_, err := pollProcess(ctx, mock, "proc-1", nil, testConfig())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --- PollBuild tests ---

// appVersionSequencer wraps platform.Mock and overrides SearchAppVersions
// to return a sequence of app version event slices for PollBuild tests.
type appVersionSequencer struct {
	*platform.Mock
	mu       sync.Mutex
	sequence [][]platform.AppVersionEvent
	idx      int
}

func (s *appVersionSequencer) SearchAppVersions(_ context.Context, _ string, _ int) ([]platform.AppVersionEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idx >= len(s.sequence) {
		return s.sequence[len(s.sequence)-1], nil
	}
	events := s.sequence[s.idx]
	s.idx++
	return events, nil
}

func newBuildSequencer(statuses ...string) *appVersionSequencer {
	seq := make([][]platform.AppVersionEvent, len(statuses))
	for i, s := range statuses {
		seq[i] = []platform.AppVersionEvent{
			{
				ID:             fmt.Sprintf("av-%d", i),
				ProjectID:      "proj-1",
				ServiceStackID: "svc-1",
				Status:         s,
				Sequence:       i + 1,
				Created:        "2025-01-01T00:00:00Z",
				LastUpdate:     "2025-01-01T00:00:00Z",
			},
		}
	}
	return &appVersionSequencer{
		Mock:     platform.NewMock(),
		sequence: seq,
	}
}

func TestPollBuild_ImmediateActive(t *testing.T) {
	t.Parallel()

	seq := newBuildSequencer(statusActive)
	ctx := context.Background()

	event, err := pollBuild(ctx, seq, "proj-1", "svc-1", nil, testConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Status != statusActive {
		t.Errorf("status = %s, want ACTIVE", event.Status)
	}
}

func TestPollBuild_ImmediateFailed(t *testing.T) {
	t.Parallel()

	seq := newBuildSequencer(statusBuildFailed)
	ctx := context.Background()

	event, err := pollBuild(ctx, seq, "proj-1", "svc-1", nil, testConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Status != statusBuildFailed {
		t.Errorf("status = %s, want BUILD_FAILED", event.Status)
	}
}

func TestPollBuild_BuildingThenActive(t *testing.T) {
	t.Parallel()

	seq := newBuildSequencer(statusBuilding, statusBuilding, statusActive)
	ctx := context.Background()

	event, err := pollBuild(ctx, seq, "proj-1", "svc-1", nil, testConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Status != statusActive {
		t.Errorf("status = %s, want ACTIVE", event.Status)
	}
}

func TestPollBuild_NoEventThenAppears(t *testing.T) {
	t.Parallel()

	// First call returns no events, second returns ACTIVE.
	seq := &appVersionSequencer{
		Mock: platform.NewMock(),
		sequence: [][]platform.AppVersionEvent{
			{}, // no events yet
			{
				{
					ID:             "av-1",
					ProjectID:      "proj-1",
					ServiceStackID: "svc-1",
					Status:         statusActive,
					Sequence:       1,
					Created:        "2025-01-01T00:00:00Z",
					LastUpdate:     "2025-01-01T00:00:00Z",
				},
			},
		},
	}
	ctx := context.Background()

	event, err := pollBuild(ctx, seq, "proj-1", "svc-1", nil, testConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Status != statusActive {
		t.Errorf("status = %s, want ACTIVE", event.Status)
	}
}

func TestPollBuild_Timeout(t *testing.T) {
	t.Parallel()

	seq := newBuildSequencer(statusBuilding) // Never terminates.
	ctx := context.Background()

	cfg := testConfig()
	cfg.timeout = 10 * time.Millisecond

	_, err := pollBuild(ctx, seq, "proj-1", "svc-1", nil, cfg)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}

	var pe *platform.PlatformError
	if !errorAs(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrAPITimeout {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrAPITimeout)
	}
}

func TestPollBuild_ContextCanceled(t *testing.T) {
	t.Parallel()

	seq := newBuildSequencer(statusBuilding)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	_, err := pollBuild(ctx, seq, "proj-1", "svc-1", nil, testConfig())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != context.Canceled {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

func TestPollBuild_SearchError(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithError("SearchAppVersions", fmt.Errorf("connection refused"))
	ctx := context.Background()

	_, err := pollBuild(ctx, mock, "proj-1", "svc-1", nil, testConfig())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestPollBuild_FiltersByServiceID(t *testing.T) {
	t.Parallel()

	// Events for two services — only svc-2 should match.
	seq := &appVersionSequencer{
		Mock: platform.NewMock(),
		sequence: [][]platform.AppVersionEvent{
			{
				{
					ID:             "av-other",
					ProjectID:      "proj-1",
					ServiceStackID: "svc-1",
					Status:         statusBuilding,
					Sequence:       1,
				},
				{
					ID:             "av-target",
					ProjectID:      "proj-1",
					ServiceStackID: "svc-2",
					Status:         statusActive,
					Sequence:       2,
				},
			},
		},
	}
	ctx := context.Background()

	event, err := pollBuild(ctx, seq, "proj-1", "svc-2", nil, testConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.ID != "av-target" {
		t.Errorf("event ID = %s, want av-target", event.ID)
	}
	if event.Status != statusActive {
		t.Errorf("status = %s, want ACTIVE", event.Status)
	}
}

func TestPollBuild_CallbackCalled(t *testing.T) {
	t.Parallel()

	seq := newBuildSequencer(statusBuilding, statusActive)
	ctx := context.Background()

	var mu sync.Mutex
	var calls []string
	cb := func(message string, progress, total float64) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, message)
		if total != 100 {
			t.Errorf("total = %v, want 100", total)
		}
	}

	event, err := pollBuild(ctx, seq, "proj-1", "svc-1", cb, testConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Status != statusActive {
		t.Errorf("status = %s, want ACTIVE", event.Status)
	}

	mu.Lock()
	defer mu.Unlock()
	// One callback per SearchAppVersions call: BUILDING, ACTIVE = 2.
	if len(calls) != 2 {
		t.Errorf("callback called %d times, want 2", len(calls))
	}
}

func TestPollBuild_PublicFunction(t *testing.T) {
	t.Parallel()

	// Immediate ACTIVE — no actual waiting needed.
	seq := newBuildSequencer(statusActive)
	ctx := context.Background()

	event, err := PollBuild(ctx, seq, "proj-1", "svc-1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Status != statusActive {
		t.Errorf("status = %s, want ACTIVE", event.Status)
	}
}

// errorAs is a test helper that wraps errors.As to avoid importing errors.
func errorAs(err error, target any) bool {
	if t, ok := target.(**platform.PlatformError); ok {
		for err != nil {
			if pe, ok := err.(*platform.PlatformError); ok {
				*t = pe
				return true
			}
			// Check for wrapped errors.
			if uw, ok := err.(interface{ Unwrap() error }); ok {
				err = uw.Unwrap()
			} else {
				return false
			}
		}
	}
	return false
}

// TestPollProcess_PublicFunction verifies the public PollProcess uses defaults.
func TestPollProcess_PublicFunction(t *testing.T) {
	t.Parallel()

	// Immediate finish — no actual waiting needed.
	seq := newSequencer(statusFinished)
	ctx := context.Background()

	proc, err := PollProcess(ctx, seq, "proc-1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proc.Status != statusFinished {
		t.Errorf("status = %s, want FINISHED", proc.Status)
	}
}
