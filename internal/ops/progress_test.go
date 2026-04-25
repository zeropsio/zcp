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
	// Callback is emitted only for in-progress states — terminal states (FINISHED,
	// FAILED, CANCELED) are returned directly without a progress notification to
	// avoid a client-side race in Claude Code's MCP JS SDK: its _onresponse
	// synchronously deletes the progress handler while _onnotification dispatches
	// via microtask, so a same-chunk progress+response sequence fires
	// "Received a progress notification for an unknown token" and tears down the
	// stdio transport. Sequence PENDING, RUNNING, FINISHED → 2 callbacks.
	if len(calls) != 2 {
		t.Errorf("callback called %d times, want 2", len(calls))
	}
	for _, msg := range calls {
		if msg == "Process proc-1: "+statusFinished {
			t.Errorf("terminal-state callback emitted (%q) — would race with tool response", msg)
		}
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

func TestPollBuild_UploadingThroughActive(t *testing.T) {
	t.Parallel()

	// UPLOADING is an in-progress state — the poller must wait through it.
	// A stuck UPLOADING from a killed push gets reused by the next attempt,
	// so the poller sees UPLOADING → BUILDING → ACTIVE.
	seq := newBuildSequencer("UPLOADING", "UPLOADING", statusBuilding, statusActive)
	ctx := context.Background()

	event, err := pollBuild(ctx, seq, "proj-1", "svc-1", nil, testConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Status != statusActive {
		t.Errorf("status = %s, want ACTIVE", event.Status)
	}
}

func TestPollBuild_FullLifecycleThroughActive(t *testing.T) {
	t.Parallel()

	// Full AppVersion lifecycle: UPLOADING → WAITING_TO_BUILD → BUILDING →
	// PREPARING_RUNTIME → WAITING_TO_DEPLOY → DEPLOYING → ACTIVE.
	// All intermediate states must be treated as in-progress.
	seq := newBuildSequencer(
		"UPLOADING", "WAITING_TO_BUILD", "WAITING_TO_BUILD",
		statusBuilding, "PREPARING_RUNTIME", "WAITING_TO_DEPLOY",
		"DEPLOYING", statusActive,
	)
	ctx := context.Background()

	event, err := pollBuild(ctx, seq, "proj-1", "svc-1", nil, testConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Status != statusActive {
		t.Errorf("status = %s, want ACTIVE", event.Status)
	}
}

func TestPollBuild_WaitingToBuild_IsInProgress(t *testing.T) {
	t.Parallel()

	// WAITING_TO_BUILD occurs when a deploy is queued behind another build.
	// The poller must wait, not treat it as terminal (which caused double deploys).
	seq := newBuildSequencer("WAITING_TO_BUILD", "WAITING_TO_BUILD", statusBuilding, statusActive)
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
	// Callback is emitted only for in-progress build states — terminal states
	// (ACTIVE, failed pipeline) are returned directly. See pollProcess callback
	// test for the full rationale (Claude Code MCP JS race on same-chunk
	// progress+response). Sequence BUILDING, ACTIVE → 1 callback.
	if len(calls) != 1 {
		t.Errorf("callback called %d times, want 1", len(calls))
	}
	for _, msg := range calls {
		if msg == "Build svc-1: "+statusActive {
			t.Errorf("terminal-state callback emitted (%q) — would race with tool response", msg)
		}
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

func TestDefaultBuildPollConfig_Intervals(t *testing.T) {
	t.Parallel()

	if defaultBuildPollConfig.initialInterval != 1*time.Second {
		t.Errorf("initialInterval = %v, want 1s", defaultBuildPollConfig.initialInterval)
	}
	if defaultBuildPollConfig.stepUpInterval != 5*time.Second {
		t.Errorf("stepUpInterval = %v, want 5s", defaultBuildPollConfig.stepUpInterval)
	}
	if defaultBuildPollConfig.stepUpAfter != 30*time.Second {
		t.Errorf("stepUpAfter = %v, want 30s", defaultBuildPollConfig.stepUpAfter)
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

// --- Defensive polling tests ---

func TestPollBuild_UnknownStatus_TerminatesImmediately(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status string
	}{
		{"PREPARING_RUNTIME_FAILED", "PREPARING_RUNTIME_FAILED"},
		{"WEIRD_STATUS", "WEIRD_STATUS"},
		{"CANCELED", "CANCELED"},
		{"UNKNOWN_FUTURE_STATUS", "UNKNOWN_FUTURE_STATUS"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			seq := newBuildSequencer(tt.status)
			ctx := context.Background()

			event, err := pollBuild(ctx, seq, "proj-1", "svc-1", nil, testConfig())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if event.Status != tt.status {
				t.Errorf("status = %s, want %s", event.Status, tt.status)
			}
		})
	}
}

func TestPollProcess_UnknownStatus_TerminatesImmediately(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status string
	}{
		{"SOME_NEW_STATUS", "SOME_NEW_STATUS"},
		{"WEIRD_STATUS", "WEIRD_STATUS"},
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

func TestPollBuild_PipelineFailedTimestamp_Terminates(t *testing.T) {
	t.Parallel()

	// Status is still BUILDING but PipelineFailed is set — should terminate immediately.
	failed := "2025-01-01T00:01:00Z"
	seq := &appVersionSequencer{
		Mock: platform.NewMock(),
		sequence: [][]platform.AppVersionEvent{
			{
				{
					ID:             "av-1",
					ProjectID:      "proj-1",
					ServiceStackID: "svc-1",
					Status:         statusBuilding,
					Sequence:       1,
					Created:        "2025-01-01T00:00:00Z",
					LastUpdate:     "2025-01-01T00:01:00Z",
					Build: &platform.BuildInfo{
						PipelineFailed: &failed,
					},
				},
			},
		},
	}
	ctx := context.Background()

	event, err := pollBuild(ctx, seq, "proj-1", "svc-1", nil, testConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Status != statusBuilding {
		t.Errorf("status = %s, want BUILDING (PipelineFailed overrides)", event.Status)
	}
	if event.Build == nil || event.Build.PipelineFailed == nil {
		t.Error("expected PipelineFailed to be set")
	}
}

func TestPollBuild_ImmediatePreparingRuntimeFailed(t *testing.T) {
	t.Parallel()

	// The original bug: PREPARING_RUNTIME_FAILED should terminate immediately.
	seq := newBuildSequencer("PREPARING_RUNTIME_FAILED")
	ctx := context.Background()

	event, err := pollBuild(ctx, seq, "proj-1", "svc-1", nil, testConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Status != "PREPARING_RUNTIME_FAILED" {
		t.Errorf("status = %s, want PREPARING_RUNTIME_FAILED", event.Status)
	}
}

// TestPollBuild_SkipsStartWithoutCodeEvent verifies that a pre-existing ACTIVE event
// from startWithoutCode (Source="NONE", Build=nil) is ignored. The poll waits for
// a real build event instead of returning the pre-existing one immediately.
func TestPollBuild_SkipsStartWithoutCodeEvent(t *testing.T) {
	t.Parallel()

	// Simulate: first poll returns only the startWithoutCode ACTIVE (Source=NONE, no Build),
	// second poll adds a new BUILDING event (real deploy),
	// third poll shows the real deploy completed.
	seq := &appVersionSequencer{
		Mock: platform.NewMock(),
		sequence: [][]platform.AppVersionEvent{
			// Poll 1: only the pre-existing startWithoutCode event.
			{{ID: "av-swc", ProjectID: "proj-1", ServiceStackID: "svc-1", Source: "NONE", Status: statusActive, Sequence: 1}},
			// Poll 2: pre-existing + new build in progress.
			{
				{ID: "av-swc", ProjectID: "proj-1", ServiceStackID: "svc-1", Source: "NONE", Status: statusActive, Sequence: 1},
				{ID: "av-build", ProjectID: "proj-1", ServiceStackID: "svc-1", Source: "CLI", Status: "BUILDING", Sequence: 2, Build: &platform.BuildInfo{}},
			},
			// Poll 3: pre-existing + new build completed.
			{
				{ID: "av-swc", ProjectID: "proj-1", ServiceStackID: "svc-1", Source: "NONE", Status: statusActive, Sequence: 1},
				{ID: "av-build", ProjectID: "proj-1", ServiceStackID: "svc-1", Source: "CLI", Status: statusActive, Sequence: 2, Build: &platform.BuildInfo{}},
			},
		},
	}
	ctx := context.Background()

	event, err := pollBuild(ctx, seq, "proj-1", "svc-1", nil, testConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.ID != "av-build" {
		t.Errorf("event ID = %s, want av-build (should skip startWithoutCode av-swc)", event.ID)
	}
	if event.Sequence != 2 {
		t.Errorf("sequence = %d, want 2", event.Sequence)
	}
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

// TestPollProcess_TimeoutSkipsProgressEmit pins the same-chunk progress+response
// race fix: the iteration that detects timeout MUST NOT call onProgress before
// returning the timeout error, otherwise the progress notification and the
// error response can be coalesced into a single stdin chunk on the Claude Code
// MCP TS client and trigger "Received a progress notification for an unknown
// token" → transport teardown (empirically reproduced 7/7 with a stripped-down
// mcptest server emitting progress immediately before the response).
func TestPollProcess_TimeoutSkipsProgressEmit(t *testing.T) {
	t.Parallel()

	seq := newSequencer("PENDING") // never terminates → forces timeout path
	ctx := context.Background()

	cfg := pollConfig{
		initialInterval: 5 * time.Millisecond,
		stepUpInterval:  5 * time.Millisecond,
		stepUpAfter:     20 * time.Millisecond,
		timeout:         30 * time.Millisecond,
	}

	var mu sync.Mutex
	var lastEmit time.Time
	cb := func(_ string, _, _ float64) {
		mu.Lock()
		defer mu.Unlock()
		lastEmit = time.Now()
	}

	start := time.Now()
	_, err := pollProcess(ctx, seq, "proc-1", cb, cfg)
	returned := time.Now()

	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}

	mu.Lock()
	lastEmitCopy := lastEmit
	mu.Unlock()
	if lastEmitCopy.IsZero() {
		t.Fatal("expected at least one progress emit during the loop")
	}

	gap := returned.Sub(lastEmitCopy)
	// The fix moves timeout check before onProgress, so the iteration that
	// detects timeout returns without emitting. Last emit was therefore on a
	// prior in-progress iteration, ≥ initialInterval before timeout fired.
	// Allow a 1 ms slack for scheduler jitter.
	minGap := cfg.initialInterval - 1*time.Millisecond
	if gap < minGap {
		t.Errorf("race window: last progress emitted only %v before return (want ≥ %v); the timeout iteration is emitting progress immediately before responding, which is the bug — see internal/ops/progress.go header comment. start=%v lastEmit=%v returned=%v",
			gap, minGap, start, lastEmitCopy, returned)
	}
}

// TestPollBuild_TimeoutSkipsProgressEmit — same race fix invariant for builds.
func TestPollBuild_TimeoutSkipsProgressEmit(t *testing.T) {
	t.Parallel()

	// Always return one BUILDING event to keep poll going.
	mock := platform.NewMock().WithAppVersionEvents([]platform.AppVersionEvent{{
		ID:             "ev-1",
		ServiceStackID: "ss-1",
		Status:         "BUILDING",
		Sequence:       1,
	}})

	cfg := pollConfig{
		initialInterval: 5 * time.Millisecond,
		stepUpInterval:  5 * time.Millisecond,
		stepUpAfter:     20 * time.Millisecond,
		timeout:         30 * time.Millisecond,
	}

	var mu sync.Mutex
	var lastEmit time.Time
	cb := func(_ string, _, _ float64) {
		mu.Lock()
		defer mu.Unlock()
		lastEmit = time.Now()
	}

	ctx := context.Background()
	_, err := pollBuild(ctx, mock, "proj-1", "ss-1", cb, cfg)
	returned := time.Now()

	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}

	mu.Lock()
	lastEmitCopy := lastEmit
	mu.Unlock()
	if lastEmitCopy.IsZero() {
		t.Fatal("expected at least one progress emit during the loop")
	}

	gap := returned.Sub(lastEmitCopy)
	minGap := cfg.initialInterval - 1*time.Millisecond
	if gap < minGap {
		t.Errorf("race window in pollBuild: last progress emitted only %v before return (want ≥ %v)", gap, minGap)
	}
}
