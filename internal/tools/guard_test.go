// Tests for: workflow guards — requireWorkflowContext and requireAdoption.
package tools

import (
	"strings"
	"testing"

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

func TestRequireAdoption_KnownService_Passes(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	meta := &workflow.ServiceMeta{Hostname: "app", BootstrappedAt: "2026-01-01"}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatalf("write meta: %v", err)
	}
	result := requireAdoption(stateDir, nil, "app")
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
	result := requireAdoption(stateDir, nil, "app")
	if result == nil {
		t.Fatal("unknown service should be blocked")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "not adopted") {
		t.Errorf("expected 'not adopted', got: %s", text)
	}
}

// fakeRecipeProbe is a minimal RecipeSessionProbe stub for guard tests.
// covered names a closed set of hostnames whose CoversHost answer is
// true; HasAnySession reflects whether any session is registered.
type fakeRecipeProbe struct {
	covered    map[string]bool
	hasSession bool
}

func (f *fakeRecipeProbe) HasAnySession() bool { return f.hasSession }
func (f *fakeRecipeProbe) CurrentSingleSession() (string, string, string, bool) {
	return "", "", "", false
}
func (f *fakeRecipeProbe) CoversHost(host string) bool { return f.covered[host] }

// TestRequireAdoption_NoProbe_NonAdopted_Blocked is the regression guard:
// without a recipeProbe, a non-adopted host with the services dir present
// must still surface SERVICE_NOT_FOUND. Pre-fix-1 behavior must persist
// when no probe is wired.
func TestRequireAdoption_NoProbe_NonAdopted_Blocked(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	meta := &workflow.ServiceMeta{Hostname: "other", BootstrappedAt: "2026-01-01"}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatalf("write meta: %v", err)
	}
	result := requireAdoption(stateDir, nil, "apistage")
	if result == nil {
		t.Fatal("unknown service with nil probe must block")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "SERVICE_NOT_FOUND") {
		t.Errorf("expected SERVICE_NOT_FOUND, got: %s", text)
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
		hasSession: true,
	}
	if result := requireAdoption(stateDir, probe, "apistage"); result != nil {
		t.Errorf("apistage covered by recipe; expected pass, got: %s", getTextContent(t, result))
	}
	if result := requireAdoption(stateDir, probe, "apidev"); result != nil {
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
		covered:    map[string]bool{"db": true},
		hasSession: true,
	}
	if result := requireAdoption(stateDir, probe, "db"); result != nil {
		t.Errorf("db covered by recipe service; expected pass, got: %s", getTextContent(t, result))
	}
}

// TestRequireAdoption_RecipeDoesNotCoverUnrelated_Blocked — Plan covers
// `api` only; an unrelated host must still surface SERVICE_NOT_FOUND.
func TestRequireAdoption_RecipeDoesNotCoverUnrelated_Blocked(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	meta := &workflow.ServiceMeta{Hostname: "other", BootstrappedAt: "2026-01-01"}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatalf("write meta: %v", err)
	}
	probe := &fakeRecipeProbe{
		covered:    map[string]bool{"apistage": true},
		hasSession: true,
	}
	result := requireAdoption(stateDir, probe, "unrelated-host")
	if result == nil {
		t.Fatal("unrelated-host not in recipe; expected SERVICE_NOT_FOUND")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "SERVICE_NOT_FOUND") {
		t.Errorf("expected SERVICE_NOT_FOUND, got: %s", text)
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
		covered:    map[string]bool{"appstage": true},
		hasSession: true,
	}
	if result := requireAdoption(stateDir, probe, "appstage"); result != nil {
		t.Errorf("appstage covered; expected pass, got: %s", getTextContent(t, result))
	}
}

// TestRequireAdoption_EmptyHostList_NoOp — variadic with zero hostnames
// returns nil unconditionally.
func TestRequireAdoption_EmptyHostList_NoOp(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	probe := &fakeRecipeProbe{}
	if result := requireAdoption(stateDir, probe); result != nil {
		t.Errorf("empty host list must no-op, got: %s", getTextContent(t, result))
	}
}
