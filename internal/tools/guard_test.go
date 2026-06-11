// Tests for: workflow guards — requireWorkflowContext and requireAdoption.
package tools

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestRequireWorkflowContext_NilEngine_NoMarker_Blocks(t *testing.T) {
	t.Parallel()
	result := requireWorkflowContext(nil, t.TempDir(), nil)
	if result == nil {
		t.Fatal("expected non-nil result when no workflow context")
	}
	if !result.IsError {
		t.Error("expected IsError=true")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "WORKFLOW_REQUIRED") {
		t.Errorf("expected WORKFLOW_REQUIRED, got: %s", text)
	}
}

func TestRequireWorkflowContext_ActiveSession_Passes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvLocal, nil)
	if _, err := engine.Start("proj-1", "bootstrap", "test"); err != nil {
		t.Fatalf("start session: %v", err)
	}
	result := requireWorkflowContext(engine, dir, nil)
	if result != nil {
		t.Errorf("active session should pass, got error")
	}
}

func TestRequireWorkflowContext_WorkSession_Passes(t *testing.T) {
	stateDir := t.TempDir()
	ws := workflow.NewWorkSession("proj-1", "container", "test", []string{"appdev"})
	if err := workflow.SaveWorkSession(stateDir, ws); err != nil {
		t.Fatalf("save work session: %v", err)
	}
	result := requireWorkflowContext(nil, stateDir, nil)
	if result != nil {
		t.Errorf("open work session should pass, got error")
	}
}

func TestRequireWorkflowContext_ClosedWorkSession_Blocks(t *testing.T) {
	stateDir := t.TempDir()
	ws := workflow.NewWorkSession("proj-1", "container", "test", []string{"appdev"})
	ws.ClosedAt = "2026-04-17T00:00:00Z"
	ws.CloseReason = workflow.CloseReasonExplicit
	if err := workflow.SaveWorkSession(stateDir, ws); err != nil {
		t.Fatalf("save work session: %v", err)
	}
	result := requireWorkflowContext(nil, stateDir, nil)
	if result == nil {
		t.Fatal("closed work session should block, got nil")
	}
}

func TestRequireWorkflowContext_EmptyStateDir_Blocks(t *testing.T) {
	t.Parallel()
	result := requireWorkflowContext(nil, "", nil)
	if result == nil {
		t.Fatal("expected non-nil result for empty stateDir with nil engine")
	}
}

// TestRequireAdoption_LocalEnv_RecoveryPointsAtAdoptLocal — local env
// recovery hint must name `adopt-local`, not `bootstrap`. Pre-Phase-12
// the message hard-coded bootstrap, sending agents on a wrong-recovery
// loop on local-only projects (the right action is adopt-local +
// targetService).
func TestRequireAdoption_LocalEnv_RecoveryPointsAtAdoptLocal(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	meta := &workflow.ServiceMeta{Hostname: "other", BootstrappedAt: "2026-01-01"}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatalf("write meta: %v", err)
	}
	result := requireAdoption(stateDir, runtime.Info{}, nil, "app")
	if result == nil {
		t.Fatal("expected block")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "adopt-local") {
		t.Errorf("local-env recovery should mention adopt-local; got: %s", text)
	}
	if strings.Contains(text, "bootstrap") {
		t.Errorf("local-env recovery should NOT mention bootstrap; got: %s", text)
	}
	// Structured Recovery hint pinned: tool=zerops_workflow,
	// action=adopt-local, args carry the unadopted hostname so the
	// agent can re-run the call directly.
	var wire ErrorWire
	if err := json.Unmarshal([]byte(text), &wire); err != nil {
		t.Fatalf("parse wire: %v", err)
	}
	if wire.Recovery == nil {
		t.Fatalf("Recovery hint missing")
	}
	if wire.Recovery.Tool != "zerops_workflow" {
		t.Errorf("Recovery.Tool = %q, want zerops_workflow", wire.Recovery.Tool)
	}
	if wire.Recovery.Action != "adopt-local" {
		t.Errorf("Recovery.Action = %q, want adopt-local", wire.Recovery.Action)
	}
	if got := wire.Recovery.Args["targetService"]; got != "app" {
		t.Errorf("Recovery.Args[targetService] = %q, want app", got)
	}
}

// TestRequireAdoption_ContainerEnv_StillPointsAtBootstrap — regression.
// Container env keeps the bootstrap recovery message and no adopt-local
// suggestion (adopt-local is local-only).
func TestRequireAdoption_ContainerEnv_StillPointsAtBootstrap(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	meta := &workflow.ServiceMeta{Hostname: "other", BootstrappedAt: "2026-01-01"}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatalf("write meta: %v", err)
	}
	result := requireAdoption(stateDir, runtime.Info{InContainer: true, ServiceName: "zcp"}, nil, "app")
	if result == nil {
		t.Fatal("expected block")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "bootstrap") {
		t.Errorf("container-env recovery should mention bootstrap; got: %s", text)
	}
	if strings.Contains(text, "adopt-local") {
		t.Errorf("container-env recovery should NOT mention adopt-local; got: %s", text)
	}
}

func TestRequireAdoption_KnownService_Passes(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	meta := &workflow.ServiceMeta{Hostname: "app", BootstrappedAt: "2026-01-01"}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatalf("write meta: %v", err)
	}
	result := requireAdoption(stateDir, runtime.Info{}, nil, "app")
	if result != nil {
		t.Errorf("known service should pass, got error")
	}
}

func TestRequireAdoption_UnknownService_Blocks(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	meta := &workflow.ServiceMeta{Hostname: "other", BootstrappedAt: "2026-01-01"}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatalf("write meta: %v", err)
	}
	result := requireAdoption(stateDir, runtime.Info{}, nil, "app")
	if result == nil {
		t.Fatal("unknown service should be blocked")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "not adopted") {
		t.Errorf("expected 'not adopted', got: %s", text)
	}
}

// fakeRecipeProbe is the single RecipeSessionProbe stub for tools tests —
// tools tests exercise the interface CONTRACT, never the concrete
// authoring store (boundary rule: core packages, tests included, do not
// import internal/authoring; the concrete satisfaction is compile-pinned
// at the server.go composition root). covered names a closed set of
// hostnames whose CoversHost answer is true; sessions drives
// HasAnySession (any) and CurrentSingleSession (exactly one).
type fakeRecipeProbe struct {
	covered  map[string]bool
	sessions []fakeRecipeSession
}

// fakeRecipeSession mirrors the C1 contract semantics of
// Store.CurrentSingleSession: the legacy-facts and manifest paths are
// fixed filenames joined under the session's outputRoot.
type fakeRecipeSession struct {
	slug       string
	outputRoot string
}

func (f *fakeRecipeProbe) HasAnySession() bool { return len(f.sessions) > 0 }
func (f *fakeRecipeProbe) CurrentSingleSession() (string, string, string, bool) {
	if len(f.sessions) != 1 {
		return "", "", "", false
	}
	s := f.sessions[0]
	return s.slug,
		filepath.Join(s.outputRoot, "legacy-facts.jsonl"),
		filepath.Join(s.outputRoot, "workspace-manifest.json"),
		true
}
func (f *fakeRecipeProbe) CoversHost(host string) bool { return f.covered[host] }

// TestRequireAdoption_NoProbe_NonAdopted_Blocked is the regression guard:
// without a recipeProbe, a non-adopted host with the services dir present
// must surface ADOPT_REQUIRED (B17: the hostname exists but is not adopted)
// when no probe is wired.
func TestRequireAdoption_NoProbe_NonAdopted_Blocked(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	meta := &workflow.ServiceMeta{Hostname: "other", BootstrappedAt: "2026-01-01"}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatalf("write meta: %v", err)
	}
	result := requireAdoption(stateDir, runtime.Info{}, nil, "apistage")
	if result == nil {
		t.Fatal("unknown service with nil probe must block")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "ADOPT_REQUIRED") {
		t.Errorf("expected ADOPT_REQUIRED, got: %s", text)
	}
}

// TestRequireAdoption_RecipeCoversHost_Passes — open recipe Plan has a
// codebase whose hostname is `api`; `apistage` and `apidev` are covered.
func TestRequireAdoption_RecipeCoversHost_Passes(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	// Force services/ dir to exist (gate active) but with an unrelated meta.
	meta := &workflow.ServiceMeta{Hostname: "other", BootstrappedAt: "2026-01-01"}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatalf("write meta: %v", err)
	}
	probe := &fakeRecipeProbe{
		covered: map[string]bool{
			"apistage": true,
			"apidev":   true,
		},
		sessions: []fakeRecipeSession{{slug: "guard-test"}},
	}
	if result := requireAdoption(stateDir, runtime.Info{}, probe, "apistage"); result != nil {
		t.Errorf("apistage covered by recipe; expected pass, got: %s", getTextContent(t, result))
	}
	if result := requireAdoption(stateDir, runtime.Info{}, probe, "apidev"); result != nil {
		t.Errorf("apidev covered by recipe; expected pass, got: %s", getTextContent(t, result))
	}
}

// TestRequireAdoption_RecipeCoversManagedService_Passes — open recipe
// Plan has a managed Service with hostname `db`; `db` is covered.
func TestRequireAdoption_RecipeCoversManagedService_Passes(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	meta := &workflow.ServiceMeta{Hostname: "other", BootstrappedAt: "2026-01-01"}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatalf("write meta: %v", err)
	}
	probe := &fakeRecipeProbe{
		covered:  map[string]bool{"db": true},
		sessions: []fakeRecipeSession{{slug: "guard-test"}},
	}
	if result := requireAdoption(stateDir, runtime.Info{}, probe, "db"); result != nil {
		t.Errorf("db covered by recipe service; expected pass, got: %s", getTextContent(t, result))
	}
}

// TestRequireAdoption_RecipeDoesNotCoverUnrelated_Blocked — Plan covers
// `api` only; an unrelated host must surface ADOPT_REQUIRED.
func TestRequireAdoption_RecipeDoesNotCoverUnrelated_Blocked(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	meta := &workflow.ServiceMeta{Hostname: "other", BootstrappedAt: "2026-01-01"}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatalf("write meta: %v", err)
	}
	probe := &fakeRecipeProbe{
		covered:  map[string]bool{"apistage": true},
		sessions: []fakeRecipeSession{{slug: "guard-test"}},
	}
	result := requireAdoption(stateDir, runtime.Info{}, probe, "unrelated-host")
	if result == nil {
		t.Fatal("unrelated-host not in recipe; expected ADOPT_REQUIRED")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "ADOPT_REQUIRED") {
		t.Errorf("expected ADOPT_REQUIRED, got: %s", text)
	}
}

// TestRequireAdoption_MultipleSessionsOneCovers_Passes — at least one
// session covers the host, deploy passes. The probe abstraction itself
// hides session count; we just need the probe to answer true.
func TestRequireAdoption_MultipleSessionsOneCovers_Passes(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	meta := &workflow.ServiceMeta{Hostname: "other", BootstrappedAt: "2026-01-01"}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatalf("write meta: %v", err)
	}
	probe := &fakeRecipeProbe{
		covered:  map[string]bool{"appstage": true},
		sessions: []fakeRecipeSession{{slug: "guard-test"}},
	}
	if result := requireAdoption(stateDir, runtime.Info{}, probe, "appstage"); result != nil {
		t.Errorf("appstage covered; expected pass, got: %s", getTextContent(t, result))
	}
}

// TestRequireAdoption_EmptyHostList_NoOp — variadic with zero hostnames
// returns nil unconditionally.
func TestRequireAdoption_EmptyHostList_NoOp(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	probe := &fakeRecipeProbe{}
	if result := requireAdoption(stateDir, runtime.Info{}, probe); result != nil {
		t.Errorf("empty host list must no-op, got: %s", getTextContent(t, result))
	}
}
