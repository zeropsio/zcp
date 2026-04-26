// Tests for: workflow.go::handleLifecycleStatus — pins the Phase 4.4
// boundary contract.
//
// pipeline-repair commit `8eeb74b2` ("route StateEnvelope through engine
// API + auth layering doc") added the new handler boundary that
// computes an envelope and routes it through synthesis. Pipeline-repair
// C2 updated the callsite at workflow.go:774 to consume `Synthesize` +
// `BodiesOf`, but neither pinned the tool-side wiring contract — a
// regression that zeros an envelope field, swaps an envelope, or drops
// a Synthesize error would only fail downstream tests by accident or
// not at all.
//
// Scope: handleLifecycleStatus only (the canonical recovery primitive
// per spec P4). Other workflow handlers route to synthesis too but
// don't need per-shape boundary tests until a real per-shape bug
// surfaces (pipeline-repair §1: "Real-bug-driven only").
//
// Assertion shape: rendered markdown smoke. The handler emits
// `textResult(RenderStatus(...))` — a typed-plan return surface would
// give cleaner assertions, but adding one is a production-side
// refactor we don't justify without a real bug. Substring on the
// rendered output is the cheapest functional pin: any envelope
// corruption / routing drop / silent error swallow shows up as wrong
// guidance text.
package tools

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestHandleLifecycleStatus_EmptyProject_RoutesToBootstrap pins the
// idle-empty case. With no services in the project and no workflow
// state on disk, the handler must produce a response that mentions
// `zerops_workflow` and `bootstrap` — proving the envelope reached
// Synthesize, the idle-empty atom fired, and BuildPlan picked the
// bootstrap entry as Primary.
//
// A regression that drops the workflow envelope, corrupts the idle
// scenario, or swallows the Synthesize result would fail this test.
func TestHandleLifecycleStatus_EmptyProject_RoutesToBootstrap(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eng := workflow.NewEngine(dir, workflow.EnvContainer, nil)
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "test"})

	result, _, err := handleLifecycleStatus(
		context.Background(), eng, mock, "proj-1", runtime.Info{InContainer: true},
	)
	if err != nil {
		t.Fatalf("handleLifecycleStatus: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected non-error response for empty project, got: %s",
			getTextContent(t, result))
	}

	text := getTextContent(t, result)
	for _, want := range []string{"zerops_workflow", "bootstrap"} {
		if !strings.Contains(text, want) {
			t.Errorf("empty-project response missing %q\n%s", want, text)
		}
	}
}

// TestHandleLifecycleStatus_BootstrappedProject_RoutesToDevelop pins
// the idle-bootstrapped case. With one service that has a ServiceMeta
// on disk (proving it's been bootstrapped through ZCP), the handler
// must route to develop — the envelope's IdleScenario should be
// IdleBootstrapped, planIdle picks develop as Primary.
//
// This is the symmetry to the empty-project test: same handler, same
// pipeline, different envelope shape; if the routing layer fails for
// EITHER input, the corresponding test fails.
func TestHandleLifecycleStatus_BootstrappedProject_RoutesToDevelop(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eng := workflow.NewEngine(dir, workflow.EnvContainer, nil)

	// Adopt-style ServiceMeta on disk → service appears bootstrapped.
	meta := &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             "dev",
		BootstrapSession: "test-session",
		BootstrappedAt:   "2026-04-26T12:00:00Z",
	}
	if err := workflow.WriteServiceMeta(dir, meta); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "test"}).
		WithServices([]platform.ServiceStack{
			{
				ID:     "svc-app",
				Name:   "appdev",
				Status: "ACTIVE",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName:  "nodejs@22",
					ServiceStackTypeCategoryName: "USER",
				},
			},
		})

	result, _, err := handleLifecycleStatus(
		context.Background(), eng, mock, "proj-1", runtime.Info{InContainer: true},
	)
	if err != nil {
		t.Fatalf("handleLifecycleStatus: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected non-error response for bootstrapped project, got: %s",
			getTextContent(t, result))
	}

	text := getTextContent(t, result)
	for _, want := range []string{"zerops_workflow", "develop"} {
		if !strings.Contains(text, want) {
			t.Errorf("bootstrapped-project response missing %q\n%s", want, text)
		}
	}
}

// TestHandleLifecycleStatus_PlatformError_PropagatesAsMCP proves the
// error path: when ComputeEnvelope's underlying ListServices errors
// out, the handler returns a structured MCP error rather than panicking
// or returning empty content. Pipeline-repair P4 (errors stay terse,
// recovery via status) means the error surface itself must remain
// stable — this test guards that.
func TestHandleLifecycleStatus_PlatformError_PropagatesAsMCP(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eng := workflow.NewEngine(dir, workflow.EnvContainer, nil)
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "test"}).
		WithError("ListServices", &platform.PlatformError{
			Code:    platform.ErrAPIError,
			Message: "synthetic API failure",
		})

	result, _, err := handleLifecycleStatus(
		context.Background(), eng, mock, "proj-1", runtime.Info{InContainer: true},
	)
	if err != nil {
		t.Fatalf("handleLifecycleStatus must not return raw error (use convertError); got: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true on platform failure, got success: %s",
			getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "Compute envelope") {
		t.Errorf("error surface should name the failing stage; got: %s", text)
	}
}

// TestHandleLifecycleStatus_CorruptWorkSession_SurfacesRecovery pins the
// cross-layer contract for the recovery primitive: when the per-PID
// work-session file is corrupt, action="status" must surface the typed
// PlatformError(ErrWorkSessionCorrupt) all the way to the LLM with its
// recovery Suggestion intact (zerops_workflow action="reset"
// workflow="develop"). Pre-fix the handler wrapped every ComputeEnvelope
// failure in ErrNotImplemented, deleting the suggestion the agent
// needed to recover.
func TestHandleLifecycleStatus_CorruptWorkSession_SurfacesRecovery(t *testing.T) {
	// Cannot t.Parallel — work session uses os.Getpid() and the corrupt
	// file lives at a per-PID path that other tests would race on.

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "work"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	pidFile := filepath.Join(dir, "work", strconv.Itoa(os.Getpid())+".json")
	if err := os.WriteFile(pidFile, []byte("{malformed"), 0o644); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(pidFile) })

	eng := workflow.NewEngine(dir, workflow.EnvContainer, nil)
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "test"})

	result, _, err := handleLifecycleStatus(
		context.Background(), eng, mock, "proj-1", runtime.Info{InContainer: true},
	)
	if err != nil {
		t.Fatalf("handleLifecycleStatus must not return raw error; got: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true on corrupt work session, got success: %s",
			getTextContent(t, result))
	}

	text := getTextContent(t, result)
	for _, want := range []string{
		platform.ErrWorkSessionCorrupt, // typed code reaches the wire
		"action=",                      // suggestion names the recovery action
		"reset",
		"workflow=",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("corrupt-session error surface missing %q\nwire text:\n%s", want, text)
		}
	}
}
