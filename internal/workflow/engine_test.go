// Tests for: workflow engine — orchestration, managed service detection.
package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/knowledge"
)

func TestIsManagedService(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		serviceType string
		want        bool
	}{
		{"postgresql", "postgresql@16", true},
		{"valkey", "valkey@7.2", true},
		{"object_storage_hyphen", "object-storage@1", true},
		{"shared_storage_hyphen", "shared-storage@1", true},
		{"object_storage_bare", "object-storage", true},
		{"shared_storage_bare", "shared-storage", true},
		{"nats", "nats@2.10", true},
		{"clickhouse", "clickhouse@24.3", true},
		{"qdrant", "qdrant@1.12", true},
		{"typesense", "typesense@27.1", true},
		{"runtime_bun", "bun@1.2", false},
		{"runtime_nodejs", "nodejs@22", false},
		{"runtime_go", "go@1", false},
		{"runtime_php", "php-nginx@8.4", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsManagedService(tt.serviceType); got != tt.want {
				t.Errorf("IsManagedService(%q) = %v, want %v", tt.serviceType, got, tt.want)
			}
		})
	}
}

func TestEngine_Reset(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	if _, err := eng.Start("proj-1", "develop", "test"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := eng.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if eng.HasActiveSession() {
		t.Error("expected no active session after reset")
	}
}

func TestEngine_Reset_ClearsSessionID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	if _, err := eng.Start("proj-1", "develop", "test"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if eng.SessionID() == "" {
		t.Fatal("expected non-empty session ID after Start")
	}

	if err := eng.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if eng.SessionID() != "" {
		t.Error("expected empty session ID after Reset")
	}
}

func TestEngine_Reset_Unregisters(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	if _, err := eng.Start("proj-1", "develop", "test"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := eng.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	sessions, err := ListSessions(dir)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("want 0 sessions after reset, got %d", len(sessions))
	}
}

func TestEngine_Iterate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	if _, err := eng.Start("proj-1", "develop", "test"); err != nil {
		t.Fatalf("Start: %v", err)
	}

	state, err := eng.Iterate()
	if err != nil {
		t.Fatalf("Iterate: %v", err)
	}
	if state.Iteration != 1 {
		t.Errorf("Iteration: want 1, got %d", state.Iteration)
	}
}

func TestEngine_GetState(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	if _, err := eng.Start("proj-1", "develop", "test"); err != nil {
		t.Fatalf("Start: %v", err)
	}

	state, err := eng.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state.ProjectID != "proj-1" {
		t.Errorf("ProjectID: want proj-1, got %s", state.ProjectID)
	}
}

func TestEngine_Start_StoresSessionID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	state, err := eng.Start("proj-1", "develop", "test")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if eng.SessionID() != state.SessionID {
		t.Errorf("engine SessionID mismatch: want %s, got %s", state.SessionID, eng.SessionID())
	}
}

func TestEngine_Start_RegistersInRegistry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	state, err := eng.Start("proj-1", "develop", "test")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	sessions, err := eng.ListActiveSessions()
	if err != nil {
		t.Fatalf("ListActiveSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("want 1 session, got %d", len(sessions))
	}
	if sessions[0].SessionID != state.SessionID {
		t.Errorf("registry session mismatch")
	}
}

// --- Auto-reset completed session tests ---

func TestEngine_Start_AutoResetDoneSession(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	// Create and manually mark bootstrap as completed (inactive).
	state, err := eng.Start("proj-1", "develop", "first")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Simulate completed workflow: bootstrap present but not active.
	state.Bootstrap = &BootstrapState{Active: false}
	if err := saveSessionState(dir, eng.SessionID(), state); err != nil {
		t.Fatalf("saveSessionState: %v", err)
	}

	// Start again — should auto-reset the completed session.
	state2, err := eng.Start("proj-1", "develop", "second")
	if err != nil {
		t.Fatalf("Start after completed: %v", err)
	}
	if state2.Intent != "second" {
		t.Errorf("Intent: want 'second', got %s", state2.Intent)
	}
	if state2.SessionID == state.SessionID {
		t.Error("expected new session ID after auto-reset")
	}
}

func TestEngine_Start_ActiveSessionBlocks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	if _, err := eng.Start("proj-1", "develop", "first"); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Start with active (non-completed) session should fail.
	_, err := eng.Start("proj-1", "develop", "second")
	if err == nil {
		t.Fatal("expected error for second Start with active session")
	}
}

// --- Bootstrap per-service scoping tests ---

func TestEngine_BootstrapParallelAllowed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng1 := NewEngine(dir, EnvLocal, nil)
	eng2 := NewEngine(dir, EnvLocal, nil)

	if _, err := eng1.BootstrapStart("proj-1", "first bootstrap"); err != nil {
		t.Fatalf("first BootstrapStart: %v", err)
	}

	// Second bootstrap on different engine should succeed (per-service, not global).
	if _, err := eng2.BootstrapStart("proj-1", "second bootstrap"); err != nil {
		t.Fatalf("second BootstrapStart should succeed (no global exclusivity): %v", err)
	}
}

func TestEngine_BootstrapExclusivity_DeadPID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Register a bootstrap session with a dead PID directly.
	entry := SessionEntry{
		SessionID: "dead-bootstrap",
		PID:       9999999,
		Workflow:  "bootstrap",
		ProjectID: "proj-1",
	}
	if err := RegisterSession(dir, entry); err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}

	// New bootstrap should succeed because the dead PID will be pruned.
	eng := NewEngine(dir, EnvLocal, nil)
	_, err := eng.BootstrapStart("proj-1", "test")
	if err != nil {
		t.Fatalf("BootstrapStart should succeed after dead PID pruned: %v", err)
	}
}

func TestEngine_HostnameLock_BlocksSameService(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng1 := NewEngine(dir, EnvContainer, nil)

	// Start first bootstrap and submit plan for "appdev".
	if _, err := eng1.BootstrapStart("proj-1", "first"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}
	_, err := eng1.BootstrapCompletePlan([]BootstrapTarget{{
		Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", ExplicitStage: "appstage"},
	}}, nil, nil)
	if err != nil {
		t.Fatalf("CompletePlan: %v", err)
	}
	// Provision step creates incomplete metas.
	if _, err := eng1.BootstrapComplete(context.Background(), "provision", "provisioned", nil); err != nil {
		t.Fatalf("Complete provision: %v", err)
	}

	// Second engine tries to bootstrap the SAME hostname.
	eng2 := NewEngine(dir, EnvContainer, nil)
	if _, err := eng2.BootstrapStart("proj-1", "second"); err != nil {
		t.Fatalf("second BootstrapStart: %v", err)
	}
	_, err = eng2.BootstrapCompletePlan([]BootstrapTarget{{
		Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", ExplicitStage: "appstage"},
	}}, nil, nil)
	if err == nil {
		t.Fatal("expected error: same hostname locked by first session")
	}
	if !strings.Contains(err.Error(), "being bootstrapped") {
		t.Errorf("error should mention hostname lock, got: %v", err)
	}
}

func TestEngine_HostnameLock_AllowsDifferentService(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng1 := NewEngine(dir, EnvContainer, nil)

	// Start first bootstrap for "appdev".
	if _, err := eng1.BootstrapStart("proj-1", "first"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}
	if _, err := eng1.BootstrapCompletePlan([]BootstrapTarget{{
		Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", ExplicitStage: "appstage"},
	}}, nil, nil); err != nil {
		t.Fatalf("CompletePlan: %v", err)
	}
	if _, err := eng1.BootstrapComplete(context.Background(), "provision", "provisioned", nil); err != nil {
		t.Fatalf("Complete provision: %v", err)
	}

	// Second engine bootstraps DIFFERENT hostname — should succeed.
	eng2 := NewEngine(dir, EnvContainer, nil)
	if _, err := eng2.BootstrapStart("proj-1", "second"); err != nil {
		t.Fatalf("second BootstrapStart: %v", err)
	}
	_, err := eng2.BootstrapCompletePlan([]BootstrapTarget{{
		Runtime: RuntimeTarget{DevHostname: "webdev", Type: "nodejs@22", ExplicitStage: "webstage"},
	}}, nil, nil)
	if err != nil {
		t.Fatalf("different hostname should not be blocked: %v", err)
	}
}

// --- Multiple engines coexist tests ---

func TestEngine_MultipleEngines_Coexist(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng1 := NewEngine(dir, EnvLocal, nil)
	eng2 := NewEngine(dir, EnvLocal, nil)

	// Both can start deploy sessions (different from bootstrap exclusivity).
	if _, err := eng1.Start("proj-1", "develop", "first"); err != nil {
		t.Fatalf("eng1.Start: %v", err)
	}
	if _, err := eng2.Start("proj-1", "develop", "second"); err != nil {
		t.Fatalf("eng2.Start: %v", err)
	}

	sessions, err := ListSessions(dir)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Errorf("want 2 sessions, got %d", len(sessions))
	}
}

func TestEngine_SameEngine_ActiveSessionBlocks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	state, err := eng.Start("proj-1", "develop", "original")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	sessionID := state.SessionID

	// Second start on same engine should fail (active non-completed session).
	_, err = eng.Start("proj-1", "develop", "retry")
	if err == nil {
		t.Fatal("expected error: active session should block")
	}

	// Original session still accessible.
	state, err = eng.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state.SessionID != sessionID {
		t.Errorf("session ID changed unexpectedly")
	}
}

// --- Bootstrap conductor engine tests ---

func TestEngine_BootstrapStart_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	resp, err := eng.BootstrapStart("proj-1", "bun + postgres")
	if err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}
	if resp.SessionID == "" {
		t.Error("expected non-empty SessionID")
	}
	if resp.Intent != "bun + postgres" {
		t.Errorf("Intent mismatch")
	}
	if resp.Progress.Total != 3 {
		t.Errorf("Total: want 3, got %d", resp.Progress.Total)
	}
	if resp.Current == nil {
		t.Fatal("Current should not be nil")
	}
	if resp.Current.Name != "discover" {
		t.Errorf("Current.Name: want discover, got %s", resp.Current.Name)
	}

	// Verify bootstrap state is persisted.
	state, err := eng.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state.Bootstrap == nil {
		t.Fatal("Bootstrap state should be set")
	}
	if !state.Bootstrap.Active {
		t.Error("Bootstrap should be active")
	}
}

func TestEngine_BootstrapStart_RecipeRoute(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	docs := map[string]*knowledge.Document{
		"zerops://recipes/laravel-minimal": {
			URI:        "zerops://recipes/laravel-minimal",
			Title:      "Laravel Minimal",
			Languages:  []string{"php"},
			Frameworks: []string{"laravel"},
			ImportYAML: "project:\n  name: laravel-minimal-agent\n",
		},
	}
	store, err := knowledge.NewStore(docs)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	eng := NewEngine(dir, EnvLocal, store)

	// Under the discovery+commit split, recipe is committed only when the
	// LLM explicitly picks it. BootstrapStartWithRoute resolves the slug
	// against the corpus and pre-populates RecipeMatch on state.
	if _, err := eng.BootstrapStartWithRoute("proj-1", "Build a Laravel weather dashboard", BootstrapRouteRecipe, "laravel-minimal"); err != nil {
		t.Fatalf("BootstrapStartWithRoute: %v", err)
	}
	state, err := eng.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state.Bootstrap == nil {
		t.Fatal("Bootstrap state missing")
	}
	if state.Bootstrap.Route != BootstrapRouteRecipe {
		t.Errorf("route: got %q, want %q", state.Bootstrap.Route, BootstrapRouteRecipe)
	}
	if state.Bootstrap.RecipeMatch == nil || state.Bootstrap.RecipeMatch.Slug != "laravel-minimal" {
		t.Errorf("recipe match: got %+v", state.Bootstrap.RecipeMatch)
	}
}

// TestEngine_BootstrapStartWithRoute_UnknownSlug_NoSessionLeak verifies the
// fail-fast guard: when the LLM picks a recipe slug that the corpus doesn't
// know (typo, stale context, retired recipe), the engine must error BEFORE
// creating a session. Otherwise an orphan session file persists and blocks
// future bootstrap attempts until a manual reset.
func TestEngine_BootstrapStartWithRoute_UnknownSlug_NoSessionLeak(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	docs := map[string]*knowledge.Document{
		"zerops://recipes/laravel-minimal": {
			URI:        "zerops://recipes/laravel-minimal",
			Languages:  []string{"php"},
			Frameworks: []string{"laravel"},
			ImportYAML: "services:\n  - hostname: app\n    type: php-nginx@8.4\n",
		},
	}
	store, _ := knowledge.NewStore(docs)
	eng := NewEngine(dir, EnvLocal, store)

	_, err := eng.BootstrapStartWithRoute("proj-1", "laravel", BootstrapRouteRecipe, "not-a-real-recipe")
	if err == nil {
		t.Fatal("expected error for unknown slug")
	}
	if !strings.Contains(err.Error(), "unknown slug") {
		t.Errorf("error should name 'unknown slug': got %q", err.Error())
	}

	// Critical invariant: no session was created. The registry and on-disk
	// state must be empty so the next bootstrap call succeeds immediately.
	sessions, lsErr := eng.ListActiveSessions()
	if lsErr != nil {
		t.Fatalf("ListActiveSessions: %v", lsErr)
	}
	if len(sessions) != 0 {
		t.Errorf("session leaked after failed recipe lookup: %+v", sessions)
	}
	if eng.HasActiveSession() {
		t.Error("engine reports active session after failed recipe lookup")
	}

	// Follow-up: a subsequent valid start must succeed without needing reset.
	if _, err := eng.BootstrapStartWithRoute("proj-1", "laravel retry", BootstrapRouteRecipe, "laravel-minimal"); err != nil {
		t.Fatalf("follow-up start after failed lookup: %v (session leak blocked retry)", err)
	}
}

func TestEngine_BootstrapStart_ClassicRouteWhenNoRecipeMatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	docs := map[string]*knowledge.Document{
		"zerops://recipes/laravel-minimal": {
			URI:        "zerops://recipes/laravel-minimal",
			Languages:  []string{"php"},
			Frameworks: []string{"laravel"},
			ImportYAML: "project: {}",
		},
	}
	store, _ := knowledge.NewStore(docs)
	eng := NewEngine(dir, EnvLocal, store)

	// BootstrapStart (no route) commits classic by default — no auto-recipe
	// detection anymore. Even when the intent has framework keywords, classic
	// is the default until the LLM picks differently.
	if _, err := eng.BootstrapStart("proj-1", "weather dashboard without a framework keyword"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}
	state, _ := eng.GetState()
	if state.Bootstrap.Route == BootstrapRouteRecipe {
		t.Errorf("route should not be recipe on default BootstrapStart, got %q", state.Bootstrap.Route)
	}
}

func TestEngine_BootstrapStart_ExistingSession(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	if _, err := eng.BootstrapStart("proj-1", "first"); err != nil {
		t.Fatalf("first BootstrapStart: %v", err)
	}

	_, err := eng.BootstrapStart("proj-1", "second")
	if err == nil {
		t.Fatal("expected error for second BootstrapStart")
	}
}

func TestEngine_BootstrapComplete_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	resp, err := eng.BootstrapComplete(context.Background(), "discover", "FRESH project, plan submitted", nil)
	if err != nil {
		t.Fatalf("BootstrapComplete: %v", err)
	}
	if resp.Current == nil {
		t.Fatal("Current should not be nil after first complete")
	}
	if resp.Current.Name != "provision" {
		t.Errorf("Current.Name: want provision, got %s", resp.Current.Name)
	}
	if resp.Progress.Completed != 1 {
		t.Errorf("Completed: want 1, got %d", resp.Progress.Completed)
	}
}

func TestEngine_BootstrapComplete_FullSequence(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	// Complete discover with plan
	resp, err := eng.BootstrapCompletePlan([]BootstrapTarget{{
		Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", ExplicitStage: "appstage"},
	}}, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}

	steps := []string{"provision", "close"}
	for _, step := range steps {
		var err error
		resp, err = eng.BootstrapComplete(context.Background(), step, "Attestation for "+step+" completed ok", nil)
		if err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
	}

	// After all steps: bootstrap done.
	if resp.Current != nil {
		t.Error("Current should be nil after all steps")
	}
	if resp.Progress.Completed != 3 {
		t.Errorf("Completed: want 3, got %d", resp.Progress.Completed)
	}

	// Session should be unregistered (completed sessions are immediately cleaned up).
	sessions, err := ListSessions(dir)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("want 0 sessions after bootstrap done, got %d", len(sessions))
	}
}

func TestEngine_BootstrapComplete_AdoptionFastPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvContainer, nil)

	if _, err := eng.BootstrapStart("proj-1", "adopt existing"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	// Submit plan with all targets isExisting=true, all deps EXISTS.
	_, err := eng.BootstrapCompletePlan([]BootstrapTarget{{
		Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", IsExisting: true, ExplicitStage: "appstage"},
		Dependencies: []Dependency{
			{Hostname: "db", Type: "postgresql@16", Resolution: "EXISTS"},
		},
	}}, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}

	// Complete provision — should auto-complete close (adoption fast-path).
	resp, err := eng.BootstrapComplete(context.Background(), "provision", "All services exist and are running", nil)
	if err != nil {
		t.Fatalf("BootstrapComplete(provision): %v", err)
	}

	// Bootstrap should be fully done after just provision.
	if resp.Current != nil {
		t.Errorf("Current should be nil (bootstrap done), got step %q", resp.Current.Name)
	}
	if resp.Progress.Completed != 3 {
		t.Errorf("Completed: want 3, got %d", resp.Progress.Completed)
	}

	// Session should be cleaned up.
	sessions, err := ListSessions(dir)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("want 0 sessions after adoption done, got %d", len(sessions))
	}

	// ServiceMeta should exist with BootstrappedAt set.
	meta, err := ReadServiceMeta(dir, "appdev")
	if err != nil {
		t.Fatalf("ReadServiceMeta: %v", err)
	}
	if meta == nil {
		t.Fatal("expected appdev meta to exist")
	}
	if meta.BootstrappedAt == "" {
		t.Error("BootstrappedAt should be set")
	}
	if meta.BootstrapSession != "" {
		t.Error("adopted service should have empty BootstrapSession")
	}

	// Verify CLAUDE.md reflog was written.
	claudeMD := filepath.Join(dir, "..", "CLAUDE.md")
	data, readErr := os.ReadFile(claudeMD)
	if readErr == nil && len(data) > 0 {
		if !strings.Contains(string(data), "appdev") {
			t.Error("CLAUDE.md reflog should mention appdev")
		}
	}
}

func TestEngine_BootstrapComplete_AdoptionFastPath_MixedPlan_NoSkip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvContainer, nil)

	if _, err := eng.BootstrapStart("proj-1", "mixed plan"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	// Mixed plan: one existing, one new — should NOT fast-path.
	_, err := eng.BootstrapCompletePlan([]BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", IsExisting: true, ExplicitStage: "appstage"}},
		{Runtime: RuntimeTarget{DevHostname: "apidev", Type: "nodejs@22", IsExisting: false, ExplicitStage: "apistage"}},
	}, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}

	resp, err := eng.BootstrapComplete(context.Background(), "provision", "Mixed plan provisioned", nil)
	if err != nil {
		t.Fatalf("BootstrapComplete(provision): %v", err)
	}

	// Should advance to close, NOT auto-complete (fast-path requires all-existing plan).
	if resp.Current == nil {
		t.Fatal("Current should not be nil — mixed plan must not fast-path")
	}
	if resp.Current.Name != "close" {
		t.Errorf("Current.Name: want close, got %s", resp.Current.Name)
	}
}

func TestEngine_BootstrapSkip_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	// Complete discover with empty plan (managed-only, allows skipping close).
	if _, err := eng.BootstrapCompletePlan([]BootstrapTarget{}, nil, nil); err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}

	// Complete provision.
	if _, err := eng.BootstrapComplete(context.Background(), "provision", "Attestation for provision completed ok", nil); err != nil {
		t.Fatalf("BootstrapComplete(provision): %v", err)
	}

	// Skip close (skippable step when plan has no runtime services).
	resp, err := eng.BootstrapSkip("close", "managed-only bootstrap, no metas to write")
	if err != nil {
		t.Fatalf("BootstrapSkip: %v", err)
	}
	// After skipping the final step, bootstrap is done — Current is nil.
	if resp.Current != nil {
		t.Errorf("Current should be nil after skipping close, got step %q", resp.Current.Name)
	}
}

func TestEngine_BootstrapSkip_Mandatory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	// Skip discover (mandatory) — should fail.
	_, err := eng.BootstrapSkip("discover", "skip reason")
	if err == nil {
		t.Fatal("expected error skipping mandatory step 'discover'")
	}
}

func TestEngine_BootstrapStatus_Active(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	// Complete 1 step.
	if _, err := eng.BootstrapComplete(context.Background(), "discover", "FRESH project, plan submitted", nil); err != nil {
		t.Fatalf("BootstrapComplete(discover): %v", err)
	}

	resp, err := eng.BootstrapStatus()
	if err != nil {
		t.Fatalf("BootstrapStatus: %v", err)
	}
	if resp.Progress.Completed != 1 {
		t.Errorf("Completed: want 1, got %d", resp.Progress.Completed)
	}
	if resp.Current == nil {
		t.Fatal("Current should not be nil")
	}
	if resp.Current.Name != "provision" {
		t.Errorf("Current.Name: want provision, got %s", resp.Current.Name)
	}
}

func TestEngine_BootstrapStatus_WithAttestations(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	if _, err := eng.BootstrapComplete(context.Background(), "discover", "FRESH project, plan submitted", nil); err != nil {
		t.Fatalf("BootstrapComplete: %v", err)
	}

	resp, err := eng.BootstrapStatus()
	if err != nil {
		t.Fatalf("BootstrapStatus: %v", err)
	}

	if len(resp.Progress.Steps) != 3 {
		t.Fatalf("Steps count: want 3, got %d", len(resp.Progress.Steps))
	}
	if resp.Progress.Steps[0].Status != "complete" {
		t.Errorf("step[0].Status: want complete, got %s", resp.Progress.Steps[0].Status)
	}
}

func TestBootstrapStatus_ReturnsFullGuide_AfterPriorDelivery(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	// First: complete discover to advance to provision.
	if _, err := eng.BootstrapComplete(context.Background(), "discover", "FRESH project, plan submitted", nil); err != nil {
		t.Fatalf("BootstrapComplete(discover): %v", err)
	}

	// BuildResponse already delivered guide for provision (via BootstrapComplete).
	// Now BootstrapStatus must still return full guide (context recovery).
	resp, err := eng.BootstrapStatus()
	if err != nil {
		t.Fatalf("BootstrapStatus: %v", err)
	}
	if resp.Current == nil {
		t.Fatal("Current should not be nil")
	}
	if resp.Current.DetailedGuide == "" {
		t.Error("BootstrapStatus must return full guide for context recovery")
	}
	if strings.Contains(resp.Current.DetailedGuide, "already delivered") {
		t.Error("BootstrapStatus must not return gating stub — it should always deliver full guide")
	}
}

func TestEngine_BootstrapStatus_NoSession(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	_, err := eng.BootstrapStatus()
	if err == nil {
		t.Fatal("expected error for status without session")
	}
}

// --- BootstrapCompletePlan tests ---

func TestEngine_BootstrapCompletePlan_Valid(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	plan := []BootstrapTarget{
		{
			Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2", ExplicitStage: "appstage"},
			Dependencies: []Dependency{
				{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA", Resolution: "CREATE"},
			},
		},
	}
	resp, err := eng.BootstrapCompletePlan(plan, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}
	if resp.Current == nil || resp.Current.Name != "provision" {
		t.Errorf("expected current step to be 'provision', got %v", resp.Current)
	}

	state, err := eng.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state.Bootstrap.Plan == nil {
		t.Fatal("Plan should be stored in bootstrap state")
	}
	if len(state.Bootstrap.Plan.Targets) != 1 {
		t.Errorf("Plan.Targets length: want 1, got %d", len(state.Bootstrap.Plan.Targets))
	}
}

func TestEngine_BootstrapCompletePlan_InvalidHostname(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	plan := []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "my-app", Type: "bun@1.2"}},
	}
	_, err := eng.BootstrapCompletePlan(plan, nil, nil)
	if err == nil {
		t.Fatal("expected error for invalid hostname")
	}
}

func TestEngine_BootstrapCompletePlan_RecipeModeDeviation(t *testing.T) {
	t.Parallel()

	docs := map[string]*knowledge.Document{
		"zerops://recipes/laravel-minimal": {
			URI:        "zerops://recipes/laravel-minimal",
			Title:      "Laravel Minimal",
			Languages:  []string{"php"},
			Frameworks: []string{"laravel"},
			ImportYAML: "services:\n  - hostname: appdev\n    type: php-nginx@8.4\n    zeropsSetup: dev\n  - hostname: appstage\n    type: php-nginx@8.4\n    zeropsSetup: prod\n",
		},
	}
	store, err := knowledge.NewStore(docs)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, store)

	// LLM explicitly picks recipe=laravel-minimal (standard mode per import YAML).
	if _, err := eng.BootstrapStartWithRoute("proj-1", "Laravel weather dashboard", BootstrapRouteRecipe, "laravel-minimal"); err != nil {
		t.Fatalf("BootstrapStartWithRoute: %v", err)
	}

	// Deviating plan: single runtime in simple mode.
	plan := []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "app", Type: "php-nginx@8.4", BootstrapMode: "simple"}},
	}
	_, err = eng.BootstrapCompletePlan(plan, nil, nil)
	if err == nil {
		t.Fatal("expected error rejecting simple-mode plan on standard-mode recipe route")
	}
	if !strings.Contains(err.Error(), "laravel-minimal") || !strings.Contains(err.Error(), "standard mode") {
		t.Errorf("error should name recipe + mode: got %q", err.Error())
	}
}

func TestEngine_BootstrapCompletePlan_RecipeModeMatches(t *testing.T) {
	t.Parallel()

	docs := map[string]*knowledge.Document{
		"zerops://recipes/laravel-minimal": {
			URI:        "zerops://recipes/laravel-minimal",
			Title:      "Laravel Minimal",
			Languages:  []string{"php"},
			Frameworks: []string{"laravel"},
			ImportYAML: "services:\n  - hostname: appdev\n    type: php-nginx@8.4\n    zeropsSetup: dev\n  - hostname: appstage\n    type: php-nginx@8.4\n    zeropsSetup: prod\n",
		},
	}
	store, err := knowledge.NewStore(docs)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, store)

	// LLM picks recipe=laravel-minimal explicitly.
	if _, err := eng.BootstrapStartWithRoute("proj-1", "Laravel weather dashboard", BootstrapRouteRecipe, "laravel-minimal"); err != nil {
		t.Fatalf("BootstrapStartWithRoute: %v", err)
	}

	// Matching plan: standard mode.
	plan := []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "php-nginx@8.4", BootstrapMode: "standard", ExplicitStage: "appstage"}},
	}
	if _, err := eng.BootstrapCompletePlan(plan, nil, nil); err != nil {
		t.Fatalf("expected standard plan to pass on standard recipe, got %v", err)
	}
}

// TestEngine_BootstrapCompletePlan_RecipeRoute_RenameAccepted pins F6:
// under recipe route, a plan that renames runtime hostnames (to resolve
// collisions with existing project services) passes validation and is
// stored. Rewriting at provision is verified separately by the
// bootstrap_guide_assembly_test suite.
func TestEngine_BootstrapCompletePlan_RecipeRoute_RenameAccepted(t *testing.T) {
	t.Parallel()

	docs := map[string]*knowledge.Document{
		"zerops://recipes/dotnet-hello-world": {
			URI:        "zerops://recipes/dotnet-hello-world",
			Title:      "Dotnet Hello World",
			Languages:  []string{"dotnet"},
			ImportYAML: "services:\n  - hostname: appdev\n    type: dotnet@9\n    zeropsSetup: dev\n  - hostname: appstage\n    type: dotnet@9\n    zeropsSetup: prod\n  - hostname: db\n    type: postgresql@16\n    mode: NON_HA\n",
		},
	}
	store, err := knowledge.NewStore(docs)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, store)

	if _, err := eng.BootstrapStartWithRoute("proj-1", "upload .NET service", BootstrapRouteRecipe, "dotnet-hello-world"); err != nil {
		t.Fatalf("BootstrapStartWithRoute: %v", err)
	}

	// Plan renames runtime hostnames (simulating collision recovery).
	plan := []BootstrapTarget{{
		Runtime: RuntimeTarget{
			DevHostname:   "uploaddev",
			ExplicitStage: "uploadstage",
			Type:          "dotnet@9",
			BootstrapMode: PlanModeStandard,
		},
		Dependencies: []Dependency{{
			Hostname: "db", Type: "postgresql@16", Mode: "NON_HA", Resolution: "CREATE",
		}},
	}}
	resp, err := eng.BootstrapCompletePlan(plan, nil, nil)
	if err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}
	if resp.Current == nil || resp.Current.Name != "provision" {
		t.Errorf("expected current step to be 'provision' after plan acceptance, got %+v", resp.Current)
	}

	state, _ := eng.GetState()
	if state.Bootstrap.Plan == nil || state.Bootstrap.Plan.Targets[0].Runtime.DevHostname != "uploaddev" {
		t.Errorf("plan with renamed hostname should be stored verbatim, got %+v", state.Bootstrap.Plan)
	}
}

// TestEngine_BootstrapCompletePlan_RecipeRoute_ManagedRenameRejected pins
// F6: renaming a managed dependency (Dependency.Hostname != recipe's
// managed service hostname) is rejected at plan-submit time because the
// recipe's app repo zerops.yaml holds ${hostname_*} env-var references
// that cannot be rewritten through the override.
func TestEngine_BootstrapCompletePlan_RecipeRoute_ManagedRenameRejected(t *testing.T) {
	t.Parallel()

	docs := map[string]*knowledge.Document{
		"zerops://recipes/dotnet-hello-world": {
			URI:        "zerops://recipes/dotnet-hello-world",
			Title:      "Dotnet Hello World",
			Languages:  []string{"dotnet"},
			ImportYAML: "services:\n  - hostname: appdev\n    type: dotnet@9\n    zeropsSetup: dev\n  - hostname: db\n    type: postgresql@16\n    mode: NON_HA\n",
		},
	}
	store, err := knowledge.NewStore(docs)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, store)

	if _, err := eng.BootstrapStartWithRoute("proj-1", "upload service", BootstrapRouteRecipe, "dotnet-hello-world"); err != nil {
		t.Fatalf("BootstrapStartWithRoute: %v", err)
	}

	// Plan renames managed dep — should be rejected.
	plan := []BootstrapTarget{{
		Runtime: RuntimeTarget{DevHostname: "uploaddev", Type: "dotnet@9", BootstrapMode: PlanModeDev},
		Dependencies: []Dependency{{
			Hostname: "mydb", Type: "postgresql@16", Mode: "NON_HA", Resolution: "CREATE",
		}},
	}}
	_, err = eng.BootstrapCompletePlan(plan, nil, nil)
	if err == nil {
		t.Fatal("expected error rejecting managed-dep rename")
	}
	if !strings.Contains(err.Error(), "managed service") {
		t.Errorf("error should name 'managed service' rename failure, got: %v", err)
	}
}

func TestEngine_BootstrapCompletePlan_WrongStep(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}
	if _, err := eng.BootstrapComplete(context.Background(), "discover", "FRESH project detected", nil); err != nil {
		t.Fatalf("BootstrapComplete(discover): %v", err)
	}

	plan := []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2", ExplicitStage: "appstage"}},
	}
	_, err := eng.BootstrapCompletePlan(plan, nil, nil)
	if err == nil {
		t.Fatal("expected error when current step is not 'discover'")
	}
}

func TestEngine_StoreDiscoveredEnvVars(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	err := eng.StoreDiscoveredEnvVars("db", []string{"connectionString", "port", "user"})
	if err != nil {
		t.Fatalf("StoreDiscoveredEnvVars: %v", err)
	}

	err = eng.StoreDiscoveredEnvVars("cache", []string{"connectionString"})
	if err != nil {
		t.Fatalf("StoreDiscoveredEnvVars: %v", err)
	}

	state, err := eng.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state.Bootstrap.DiscoveredEnvVars == nil {
		t.Fatal("DiscoveredEnvVars should not be nil")
	}
	if len(state.Bootstrap.DiscoveredEnvVars["db"]) != 3 {
		t.Errorf("db vars: want 3, got %d", len(state.Bootstrap.DiscoveredEnvVars["db"]))
	}
	if len(state.Bootstrap.DiscoveredEnvVars["cache"]) != 1 {
		t.Errorf("cache vars: want 1, got %d", len(state.Bootstrap.DiscoveredEnvVars["cache"]))
	}
}

func TestEngine_StoreDiscoveredEnvVars_NoBootstrap(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	if _, err := eng.Start("proj-1", "develop", "test"); err != nil {
		t.Fatalf("Start: %v", err)
	}

	err := eng.StoreDiscoveredEnvVars("db", []string{"connectionString"})
	if err == nil {
		t.Fatal("expected error when no bootstrap state")
	}
}

func TestEngine_StoreDiscoveredEnvVars_MultipleServices(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	services := map[string][]string{
		"db":      {"connectionString", "port", "user", "password"},
		"cache":   {"connectionString", "port"},
		"storage": {"accessKeyId", "secretAccessKey", "bucketName"},
	}
	for hostname, vars := range services {
		if err := eng.StoreDiscoveredEnvVars(hostname, vars); err != nil {
			t.Fatalf("StoreDiscoveredEnvVars(%s): %v", hostname, err)
		}
	}

	state, err := eng.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if len(state.Bootstrap.DiscoveredEnvVars) != 3 {
		t.Errorf("service count: want 3, got %d", len(state.Bootstrap.DiscoveredEnvVars))
	}
	for hostname, want := range services {
		got := state.Bootstrap.DiscoveredEnvVars[hostname]
		if len(got) != len(want) {
			t.Errorf("%s: want %d vars, got %d", hostname, len(want), len(got))
		}
	}
}

// --- StepChecker integration tests ---

func TestEngine_BootstrapComplete_WithChecker_Pass(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	checker := func(_ context.Context, _ *ServicePlan, _ *BootstrapState) (*StepCheckResult, error) {
		return &StepCheckResult{
			Passed:  true,
			Summary: "all good",
			Checks:  []StepCheck{{Name: "test_check", Status: "pass"}},
		}, nil
	}

	resp, err := eng.BootstrapComplete(context.Background(), "discover", "FRESH project detected fine", checker)
	if err != nil {
		t.Fatalf("BootstrapComplete: %v", err)
	}
	if resp.CheckResult == nil {
		t.Fatal("CheckResult should be populated on pass")
	}
	if !resp.CheckResult.Passed {
		t.Error("CheckResult.Passed should be true")
	}
	if resp.CheckResult.Summary != "all good" {
		t.Errorf("CheckResult.Summary: want 'all good', got %q", resp.CheckResult.Summary)
	}
	if len(resp.CheckResult.Checks) != 1 || resp.CheckResult.Checks[0].Name != "test_check" {
		t.Errorf("expected 1 check named 'test_check', got %v", resp.CheckResult.Checks)
	}
	if resp.Current == nil || resp.Current.Name != "provision" {
		t.Errorf("expected next step 'provision', got %v", resp.Current)
	}
}

func TestEngine_BootstrapComplete_WithChecker_Fail(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	checker := func(_ context.Context, _ *ServicePlan, _ *BootstrapState) (*StepCheckResult, error) {
		return &StepCheckResult{
			Passed:  false,
			Summary: "service missing",
			Checks: []StepCheck{
				{Name: "service_exists", Status: "fail", Detail: "appdev not found"},
			},
		}, nil
	}

	resp, err := eng.BootstrapComplete(context.Background(), "discover", "FRESH project detected fine", checker)
	if err != nil {
		t.Fatalf("BootstrapComplete should not return error on check failure: %v", err)
	}
	if resp.CheckResult == nil {
		t.Fatal("CheckResult should be populated on failure")
	}
	if resp.CheckResult.Passed {
		t.Error("CheckResult.Passed should be false")
	}
	if resp.CheckResult.Summary != "service missing" {
		t.Errorf("CheckResult.Summary: want 'service missing', got %q", resp.CheckResult.Summary)
	}
	// Step should NOT have advanced.
	state, _ := eng.GetState()
	if state.Bootstrap.CurrentStep != 0 {
		t.Errorf("CurrentStep should still be 0, got %d", state.Bootstrap.CurrentStep)
	}
}

func TestEngine_BootstrapComplete_NilChecker(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	resp, err := eng.BootstrapComplete(context.Background(), "discover", "FRESH project detected fine", nil)
	if err != nil {
		t.Fatalf("BootstrapComplete: %v", err)
	}
	if resp.Current == nil || resp.Current.Name != "provision" {
		t.Errorf("expected next step 'provision', got %v", resp.Current)
	}
}

func TestEngine_Iterate_MaxLimit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	if _, err := eng.Start("proj-1", "develop", "test"); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Iterate 5 times (default max, aligned with 3-tier STOP) — all should succeed.
	for i := range 5 {
		state, err := eng.Iterate()
		if err != nil {
			t.Fatalf("Iterate %d: %v", i+1, err)
		}
		if state.Iteration != i+1 {
			t.Errorf("Iteration %d: want %d, got %d", i+1, i+1, state.Iteration)
		}
	}

	// 6th iteration should fail with "max iterations" error.
	_, err := eng.Iterate()
	if err == nil {
		t.Fatal("expected error on 6th iteration")
	}
	if got := err.Error(); !contains(got, "max iterations") {
		t.Errorf("error should mention 'max iterations', got: %s", got)
	}
}

func TestEngine_Iterate_EnvOverride(t *testing.T) {
	t.Setenv("ZCP_MAX_ITERATIONS", "2")

	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	if _, err := eng.Start("proj-1", "develop", "test"); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Iterate twice — should succeed.
	for i := range 2 {
		if _, err := eng.Iterate(); err != nil {
			t.Fatalf("Iterate %d: %v", i+1, err)
		}
	}

	// Third iteration should fail.
	_, err := eng.Iterate()
	if err == nil {
		t.Fatal("expected error on 3rd iteration with max=2")
	}
	if got := err.Error(); !contains(got, "max iterations") {
		t.Errorf("error should mention 'max iterations', got: %s", got)
	}
}

func TestEngine_Iterate_CapAutoClosesWorkSession(t *testing.T) {
	t.Setenv("ZCP_MAX_ITERATIONS", "2")

	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)
	if _, err := eng.Start("proj-1", "develop", "test"); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Seed a current-PID work session so cap auto-close has something to close.
	ws := NewWorkSession("proj-1", "dev", "test task", []string{"appdev"})
	if err := SaveWorkSession(dir, ws); err != nil {
		t.Fatalf("SaveWorkSession: %v", err)
	}

	for i := range 2 {
		if _, err := eng.Iterate(); err != nil {
			t.Fatalf("Iterate %d: %v", i+1, err)
		}
	}
	// Hitting the cap must close the work session with CloseReasonIterationCap.
	if _, err := eng.Iterate(); err == nil {
		t.Fatal("expected error at cap")
	}
	got, err := LoadWorkSession(dir, ws.PID)
	if err != nil || got == nil {
		t.Fatalf("LoadWorkSession after cap: ws=%v err=%v", got, err)
	}
	if got.ClosedAt == "" {
		t.Fatal("work session should be closed after iteration cap")
	}
	if got.CloseReason != CloseReasonIterationCap {
		t.Errorf("CloseReason: want %q, got %q", CloseReasonIterationCap, got.CloseReason)
	}
}

// contains is a test helper for substring matching.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestEngine_BootstrapComplete_CheckerError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	checker := func(_ context.Context, _ *ServicePlan, _ *BootstrapState) (*StepCheckResult, error) {
		return nil, fmt.Errorf("API unreachable")
	}

	_, err := eng.BootstrapComplete(context.Background(), "discover", "FRESH project detected fine", checker)
	if err == nil {
		t.Fatal("expected error from checker")
	}
}

// --- Item 16: TOCTOU fix via InitSessionAtomic ---

func TestInitSessionAtomic_BootstrapExclusivity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		firstWf     string
		secondWf    string
		expectErr   bool
		errContains string
	}{
		{
			"second_bootstrap_allowed",
			"bootstrap", "bootstrap",
			false, "",
		},
		{
			"develop_after_bootstrap_ok",
			"bootstrap", "develop",
			false, "",
		},
		{
			"two_develops_ok",
			"develop", "develop",
			false, "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()

			// First session via InitSessionAtomic.
			_, err := InitSessionAtomic(dir, "proj-1", tt.firstWf, "first")
			if err != nil {
				t.Fatalf("first InitSessionAtomic: %v", err)
			}

			// Second session.
			_, err = InitSessionAtomic(dir, "proj-1", tt.secondWf, "second")
			if tt.expectErr {
				if err == nil {
					t.Fatal("expected error for second session")
				}
				if !contains(err.Error(), tt.errContains) {
					t.Errorf("error %q should contain %q", err.Error(), tt.errContains)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// --- Item 18: Session Resume ---

func TestEngine_Resume_DeadSession(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Create a session with a dead PID by writing state directly.
	sessionID := "dead-session-001"
	state := &WorkflowState{
		Version:   stateVersion,
		SessionID: sessionID,
		PID:       9999999, // Dead PID.
		ProjectID: "proj-1",
		Workflow:  "develop",
		Iteration: 2,
		Intent:    "deploy app",
		CreatedAt: "2026-03-01T00:00:00Z",
		UpdatedAt: "2026-03-01T01:00:00Z",
	}
	if err := saveSessionState(dir, sessionID, state); err != nil {
		t.Fatalf("saveSessionState: %v", err)
	}
	entry := SessionEntry{
		SessionID: sessionID,
		PID:       9999999,
		Workflow:  "develop",
		ProjectID: "proj-1",
		CreatedAt: "2026-03-01T00:00:00Z",
		UpdatedAt: "2026-03-01T01:00:00Z",
	}
	if err := RegisterSession(dir, entry); err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}

	eng := NewEngine(dir, EnvLocal, nil)
	resumed, err := eng.Resume(sessionID)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resumed.SessionID != sessionID {
		t.Errorf("SessionID: want %s, got %s", sessionID, resumed.SessionID)
	}
	if resumed.Iteration != 2 {
		t.Errorf("Iteration: want 2, got %d", resumed.Iteration)
	}
	if eng.SessionID() != sessionID {
		t.Errorf("engine SessionID: want %s, got %s", sessionID, eng.SessionID())
	}
	// PID should be updated to current process.
	reloaded, err := LoadSessionByID(dir, sessionID)
	if err != nil {
		t.Fatalf("LoadSessionByID: %v", err)
	}
	if reloaded.PID != currentPID() {
		t.Errorf("PID: want %d, got %d", currentPID(), reloaded.PID)
	}
}

func TestEngine_Resume_AliveSession(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng1 := NewEngine(dir, EnvLocal, nil)

	// Create an active session (current PID = alive).
	state, err := eng1.Start("proj-1", "develop", "test")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Try to resume from another engine — should fail because PID is alive.
	eng2 := NewEngine(dir, EnvLocal, nil)
	_, err = eng2.Resume(state.SessionID)
	if err == nil {
		t.Fatal("expected error resuming alive session")
	}
	if !contains(err.Error(), "still active") {
		t.Errorf("error should mention 'still active', got: %s", err.Error())
	}
}

func TestEngine_Resume_NotFound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	_, err := eng.Resume("nonexistent-session")
	if err == nil {
		t.Fatal("expected error resuming non-existent session")
	}
}

// currentPID is a test helper.
func currentPID() int {
	return os.Getpid()
}

// --- C4: Session file cleanup on completion ---

func TestEngine_BootstrapComplete_DeletesSessionFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}
	sessionID := eng.SessionID()

	// Complete discover with a plan.
	if _, err := eng.BootstrapCompletePlan([]BootstrapTarget{{
		Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", ExplicitStage: "appstage"},
	}}, nil, nil); err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}

	steps := []string{"provision", "close"}
	for _, step := range steps {
		if _, err := eng.BootstrapComplete(context.Background(), step, "step completed successfully", nil); err != nil {
			t.Fatalf("BootstrapComplete(%s): %v", step, err)
		}
	}

	// Session file must be deleted.
	path := sessionFilePath(dir, sessionID)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("session file should be deleted after bootstrap completion, stat err: %v", err)
	}
	// Engine session ID must be cleared.
	if eng.SessionID() != "" {
		t.Errorf("engine SessionID should be empty after bootstrap completion, got %q", eng.SessionID())
	}
}

func TestEngine_BootstrapComplete_Partial_KeepsSessionFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}
	sessionID := eng.SessionID()

	// Complete only the first step.
	if _, err := eng.BootstrapComplete(context.Background(), "discover", "step completed successfully", nil); err != nil {
		t.Fatalf("BootstrapComplete(discover): %v", err)
	}

	// Session file must still exist (workflow not complete).
	path := sessionFilePath(dir, sessionID)
	if _, err := os.Stat(path); err != nil {
		t.Errorf("session file should still exist after partial completion, stat err: %v", err)
	}
}

// RED phase test: BootstrapComplete should enforce Plan!=nil for non-discover steps (mode gate)
func TestBootstrapComplete_PlanNilCheck_NonDiscoverSteps(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	baseDir := t.TempDir()

	// Create engine
	e := NewEngine(baseDir, EnvLocal, nil)

	// Start bootstrap
	_, err := e.BootstrapStart("proj1", "test intent")
	if err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	// Manually advance to close step (skip discover+provision), but leave Plan nil
	// so the Plan-nil gate should trip on non-discover step completion.
	st, err := e.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if st.Bootstrap == nil {
		t.Fatal("Bootstrap should be initialized")
	}

	// Manually set CurrentStep to close (index 2).
	st.Bootstrap.CurrentStep = 2
	for i := range 2 {
		st.Bootstrap.Steps[i].Status = stepComplete
	}
	st.Bootstrap.Steps[2].Status = stepInProgress
	st.Bootstrap.Plan = nil // Explicitly no plan

	if err := saveSessionState(baseDir, st.SessionID, st); err != nil {
		t.Fatalf("saveSessionState: %v", err)
	}

	// Try to complete close without a plan — should fail.
	_, err = e.BootstrapComplete(ctx, "close", "Tried to complete close without plan", nil)

	// Expect error because Plan is nil for non-discover step.
	if err == nil {
		t.Error("BootstrapComplete should fail when Plan is nil for non-discover steps")
	}
}

func TestEngine_BootstrapComplete_CleanupWarningInResponse(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	if _, err := eng.BootstrapStart("proj-1", "test cleanup warning"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}

	// Complete discover with a plan.
	if _, err := eng.BootstrapCompletePlan([]BootstrapTarget{{
		Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", ExplicitStage: "appstage"},
	}}, nil, nil); err != nil {
		t.Fatalf("BootstrapCompletePlan: %v", err)
	}

	// Complete provision only (the last pre-close step in Option A).
	if _, err := eng.BootstrapComplete(context.Background(), "provision", "step completed successfully", nil); err != nil {
		t.Fatalf("BootstrapComplete(provision): %v", err)
	}

	// Make session dir read-only so ResetSessionByID fails on file removal.
	sessDir := filepath.Join(dir, sessionsDirName)
	if err := os.Chmod(sessDir, 0o555); err != nil {
		t.Fatalf("chmod sessions dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(sessDir, 0o755) })

	// Complete close — should succeed but include cleanup warning.
	resp, err := eng.BootstrapComplete(context.Background(), "close", "bootstrap finalized", nil)
	if err != nil {
		t.Fatalf("BootstrapComplete(close): %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}

	const wantSubstr = "Warning: session cleanup failed"
	if !strings.Contains(resp.Message, wantSubstr) {
		t.Errorf("response message should contain %q, got: %s", wantSubstr, resp.Message)
	}
}

func TestReset_CleansIncompleteMetas(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		metas       []*ServiceMeta
		wantRemoved []string // hostnames that should be deleted
		wantKept    []string // hostnames that should survive
	}{
		{
			name: "incomplete_meta_for_session_removed",
			metas: []*ServiceMeta{
				{Hostname: "appdev", BootstrapSession: "SESSION_ID", BootstrappedAt: ""},
			},
			wantRemoved: []string{"appdev"},
		},
		{
			name: "complete_meta_kept",
			metas: []*ServiceMeta{
				{Hostname: "appdev", BootstrapSession: "SESSION_ID", BootstrappedAt: "2026-04-14T12:00:00Z"},
			},
			wantKept: []string{"appdev"},
		},
		{
			name: "different_session_kept",
			metas: []*ServiceMeta{
				{Hostname: "appdev", BootstrapSession: "other-session", BootstrappedAt: ""},
			},
			wantKept: []string{"appdev"},
		},
		{
			name: "mixed_complete_and_incomplete",
			metas: []*ServiceMeta{
				{Hostname: "appdev", BootstrapSession: "SESSION_ID", BootstrappedAt: "2026-04-14T12:00:00Z"},
				{Hostname: "apidev", BootstrapSession: "SESSION_ID", BootstrappedAt: ""},
			},
			wantKept:    []string{"appdev"},
			wantRemoved: []string{"apidev"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			eng := NewEngine(dir, EnvLocal, nil)

			// Start a bootstrap to get a real session.
			if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
				t.Fatalf("BootstrapStart: %v", err)
			}
			sessionID := eng.SessionID()

			// Write metas, replacing SESSION_ID placeholder with real session ID.
			for _, m := range tt.metas {
				meta := *m
				if meta.BootstrapSession == "SESSION_ID" {
					meta.BootstrapSession = sessionID
				}
				if err := WriteServiceMeta(dir, &meta); err != nil {
					t.Fatalf("WriteServiceMeta(%s): %v", meta.Hostname, err)
				}
			}

			// Reset — should clean incomplete metas for this session.
			if err := eng.Reset(); err != nil {
				t.Fatalf("Reset: %v", err)
			}

			for _, hostname := range tt.wantRemoved {
				meta, err := ReadServiceMeta(dir, hostname)
				if err != nil {
					t.Fatalf("ReadServiceMeta(%s): %v", hostname, err)
				}
				if meta != nil {
					t.Errorf("meta %q should have been removed by Reset", hostname)
				}
			}
			for _, hostname := range tt.wantKept {
				meta, err := ReadServiceMeta(dir, hostname)
				if err != nil {
					t.Fatalf("ReadServiceMeta(%s): %v", hostname, err)
				}
				if meta == nil {
					t.Errorf("meta %q should have been kept by Reset", hostname)
				}
			}
		})
	}
}
