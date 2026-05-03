// Tests for: workflow.go — zerops_workflow MCP tool handler.

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// --- Static-guidance path ---

func TestWorkflowTool_NoParams_ReturnsError(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "", nil, nil, nil, nil, "", "", nil, nil, runtime.Info{})

	result := callTool(t, srv, "zerops_workflow", nil)

	if !result.IsError {
		t.Error("expected IsError for empty call")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "No workflow or action specified") {
		t.Errorf("expected 'No workflow or action specified' error, got: %s", text)
	}
}

// TestWorkflowTool_Immediate_Export verifies the export workflow's
// defensive nil-client / empty-projectID gates. Pre-Phase-3 the path
// returned the static export.md atom; post-Phase-3 it routes to
// handleExport which requires API access for Discover. With a nil
// client the handler returns a structured error pointing at ZCP
// configuration. Live integration paths exercise the full multi-call
// flow in workflow_export_test.go (Phase 3).
func TestWorkflowTool_Immediate_Export(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "", nil, nil, nil, nil, "", "", nil, nil, runtime.Info{InContainer: true, ServiceName: "zcp"})

	result := callTool(t, srv, "zerops_workflow", map[string]any{"workflow": "export"})

	if !result.IsError {
		t.Errorf("expected IsError when client is nil, got: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "Platform client unavailable") {
		t.Errorf("expected nil-client guidance, got: %s", text)
	}
}

// TestWorkflowTool_Orchestrated_RequiresActionStart asserts that bootstrap,
// develop, and recipe cannot be fetched statically — the envelope pipeline
// needs an action=start session to synthesize guidance from atoms.
func TestWorkflowTool_Orchestrated_RequiresActionStart(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "", nil, nil, nil, nil, "", "", nil, nil, runtime.Info{})

	for _, wf := range []string{"bootstrap", "develop", "recipe"} {
		t.Run(wf, func(t *testing.T) {
			t.Parallel()
			result := callTool(t, srv, "zerops_workflow", map[string]any{"workflow": wf})
			if !result.IsError {
				t.Errorf("%s without action=start must error", wf)
			}
			text := getTextContent(t, result)
			// Response is JSON-encoded so literal quotes are escaped — check both forms.
			if !strings.Contains(text, `action="start"`) && !strings.Contains(text, `action=\"start\"`) {
				t.Errorf("%s error should direct caller to action=\"start\", got: %s", wf, text)
			}
		})
	}
}

func TestWorkflowTool_NotFound(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "", nil, nil, nil, nil, "", "", nil, nil, runtime.Info{})

	result := callTool(t, srv, "zerops_workflow", map[string]any{"workflow": "nonexistent_workflow"})

	if !result.IsError {
		t.Error("expected IsError for unknown workflow")
	}
}

// --- New Action-Based Workflow Tests ---

func TestWorkflowTool_Action_NoEngine(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "", nil, nil, nil, nil, "", "", nil, nil, runtime.Info{})

	result := callTool(t, srv, "zerops_workflow", map[string]any{"action": "start"})

	if !result.IsError {
		t.Error("expected IsError when engine is nil")
	}
}

func TestWorkflowTool_Action_UnknownAction(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "proj1", nil, nil, engine, nil, "", "", nil, nil, runtime.Info{})

	result := callTool(t, srv, "zerops_workflow", map[string]any{"action": "invalid"})

	if !result.IsError {
		t.Error("expected IsError for unknown action")
	}
}

func TestWorkflowTool_Action_Start_Develop_ReturnsBriefing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvLocal, nil)

	// Write a complete service meta so briefing finds targets.
	meta := &workflow.ServiceMeta{
		Hostname:                 "appdev",
		Mode:                     "standard",
		StageHostname:            "appstage",
		CloseDeployMode:          topology.CloseModeAuto,
		CloseDeployModeConfirmed: true,
		BootstrappedAt:           "2026-03-04T12:00:00Z",
	}
	if err := workflow.WriteServiceMeta(dir, meta); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	client := platform.NewMock().WithServices([]platform.ServiceStack{
		{
			ID: "svc-appdev", Name: "appdev",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
			Ports:                []platform.Port{{Port: 3000}},
		},
		{
			ID: "svc-appstage", Name: "appstage",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
			Ports:                []platform.Port{{Port: 3000}},
		},
	})
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, client, nil, "proj1", nil, nil, engine, nil, dir, "", nil, nil, runtime.Info{})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "develop",
		"intent":   "Deploy bun app",
		"scope":    []string{"appdev"},
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}
	// Develop is stateless at the engine-session level — should NOT create a
	// bootstrap/recipe session. A per-PID WorkSession is still written.
	if engine.HasActiveSession() {
		t.Error("develop should NOT create an engine session (stateless briefing)")
	}
	// Response should contain briefing content.
	text := getTextContent(t, result)
	if !strings.Contains(text, "briefing") && !strings.Contains(text, "Briefing") && !strings.Contains(text, "appdev") {
		t.Errorf("expected briefing response containing service info, got: %s", text[:min(len(text), 200)])
	}
}

func TestWorkflowTool_Action_Start_Develop_ManualStrategy_ReturnsBriefing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvLocal, nil)

	meta := &workflow.ServiceMeta{
		Hostname:                 "appdev",
		Mode:                     "dev",
		CloseDeployMode:          topology.CloseModeManual,
		CloseDeployModeConfirmed: true,
		BootstrappedAt:           "2026-03-04T12:00:00Z",
	}
	if err := workflow.WriteServiceMeta(dir, meta); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	client := platform.NewMock().WithServices([]platform.ServiceStack{
		{
			ID: "svc-appdev", Name: "appdev",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
			Ports:                []platform.Port{{Port: 3000}},
		},
	})
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, client, nil, "proj1", nil, nil, engine, nil, dir, "", nil, nil, runtime.Info{})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "develop",
		"intent":   "Deploy app",
		"scope":    []string{"appdev"},
	})

	if result.IsError {
		t.Fatalf("manual strategy should not return error: %s", getTextContent(t, result))
	}
	// Develop does not create an engine-level bootstrap/recipe session —
	// only the per-PID WorkSession is written.
	if engine.HasActiveSession() {
		t.Error("manual strategy should NOT create an engine session (stateless briefing)")
	}
	// Briefing should mention manual strategy.
	text := getTextContent(t, result)
	if !strings.Contains(text, "manual") {
		t.Errorf("expected briefing to reference manual strategy, got: %s", text[:min(len(text), 200)])
	}
}

func TestWorkflowTool_Action_Start_Develop_NoMetas(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "proj1", nil, nil, engine, nil, "", "", nil, nil, runtime.Info{})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "develop",
		"intent":   "Deploy app",
	})

	if !result.IsError {
		t.Error("expected error when no service metas exist")
	}
}

func TestWorkflowTool_Action_Start_Develop_IncompleteMetas(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvLocal, nil)

	// Write an incomplete meta (no BootstrappedAt — bootstrap didn't finish).
	meta := &workflow.ServiceMeta{
		Hostname: "appdev",
		Mode:     "dev",
	}
	if err := workflow.WriteServiceMeta(dir, meta); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "proj1", nil, nil, engine, nil, dir, "", nil, nil, runtime.Info{})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "develop",
		"intent":   "Deploy app",
	})

	if !result.IsError {
		t.Error("expected error when service metas are incomplete (bootstrap not finished)")
	}
}

// TestWorkflowTool_Action_Start_Immediate verifies the export
// invocation through the action="start" path. Phase 3 routes both
// no-action AND action=start invocations of workflow="export" through
// handleExport (Codex Phase 3 POST-WORK Blocker 2 — no split-brain
// between the two entry shapes). With a nil client the handler errors
// out the same way as the no-action path; the test pins that
// equivalence.
func TestWorkflowTool_Action_Start_Immediate(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvContainer, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "proj1", nil, nil, engine, nil, "", "", nil, nil, runtime.Info{InContainer: true, ServiceName: "zcp"})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "export",
	})

	if !result.IsError {
		t.Fatalf("expected nil-client error from handleExport, got: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "Platform client unavailable") {
		t.Errorf("expected nil-client guidance, got: %s", text)
	}
}

func TestWorkflowTool_Action_Start_ImmediateNoSession(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "proj1", nil, nil, engine, nil, "", "", nil, nil, runtime.Info{})

	// Start an immediate workflow — even on the new export path, no
	// session must be created. The defensive nil-client error fires
	// before any state mutation, satisfying both the original "no
	// session" intent AND the Phase 3 routing change.
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "export",
	})
	if !result.IsError {
		t.Errorf("expected error from nil-client handleExport, got: %s", getTextContent(t, result))
	}

	// Verify no session was created.
	if engine.HasActiveSession() {
		t.Error("immediate workflow should not create a session")
	}
}

func TestWorkflowTool_Action_Start_AutoResetDone(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "proj1", nil, nil, engine, nil, "", "", nil, nil, runtime.Info{})

	// Start and complete a bootstrap to get to DONE.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "route": "classic",
		"intent": "first bootstrap",
	})
	// Submit empty plan (managed-only) to satisfy mode gate.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete", "step": "discover",
		"plan": []map[string]any{},
	})
	for _, step := range []string{"provision", "generate", "deploy", "close"} {
		callTool(t, srv, "zerops_workflow", map[string]any{
			"action":      "complete",
			"step":        step,
			"attestation": "Attestation for " + step + " completed ok",
		})
	}

	// Now start a new bootstrap — should auto-reset the completed session.
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"intent":   "second bootstrap",
	})
	if result.IsError {
		t.Errorf("expected auto-reset of completed session, got error: %s", getTextContent(t, result))
	}
}

func TestWorkflowTool_Action_Reset(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "proj1", nil, nil, engine, nil, "", "", nil, nil, runtime.Info{})

	// Start bootstrap and reset. The reset response carries a structured
	// audit (cleared / preserved) instead of the old one-line success;
	// check for the shape, not the word "reset". Per G11 the response
	// no longer carries a `next` hint — agent calls action="status" for
	// the post-reset Plan against the live envelope.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
	})
	result := callTool(t, srv, "zerops_workflow", map[string]any{"action": "reset"})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	for _, needle := range []string{`"cleared"`, `"preserved"`} {
		if !strings.Contains(text, needle) {
			t.Errorf("expected reset audit field %q in:\n%s", needle, text)
		}
	}
	if strings.Contains(text, `"next"`) {
		t.Errorf("reset response must not include `next` hint (G11 — agent calls status for the next plan); got:\n%s", text)
	}
}

// P6: reset preserves complete ServiceMetas and reports them. Classic
// "reset nukes everything" misreading hit an agent in the fizzy log
// (they had to reverse-engineer state via zerops_discover because the
// old single-line response didn't say).
func TestWorkflowTool_Action_Reset_PreservesCompleteMetas(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)

	// Complete meta — survives reset.
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeDev,
		BootstrapSession: "old-sess",
		BootstrappedAt:   "2026-04-10",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "proj1", nil, nil, engine, nil, dir, "", nil, nil, runtime.Info{})

	result := callTool(t, srv, "zerops_workflow", map[string]any{"action": "reset"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "appdev") {
		t.Errorf("preserved metas should list appdev; got:\n%s", text)
	}
	// Metas file must still be there after reset.
	metas, _ := workflow.ListServiceMetas(dir)
	found := false
	for _, m := range metas {
		if m.Hostname == "appdev" {
			found = true
			break
		}
	}
	if !found {
		t.Error("appdev meta should survive reset — it was complete, not incomplete")
	}
}

func TestWorkflowTool_Action_ShowRemoved(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "proj1", nil, nil, engine, nil, "", "", nil, nil, runtime.Info{})

	result := callTool(t, srv, "zerops_workflow", map[string]any{"action": "show"})

	if !result.IsError {
		t.Error("expected IsError for removed show action")
	}
}

// --- Bootstrap Conductor Tests ---

func TestWorkflowTool_Action_BootstrapStart(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "proj1", nil, nil, engine, nil, "", "", nil, nil, runtime.Info{})

	// Commit path: explicit route=classic skips the discovery response and
	// writes a session with the default manual plan.
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"intent":   "bun + postgres",
		"route":    "classic",
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	var resp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.Progress.Total != 3 {
		t.Errorf("Total: want 3, got %d", resp.Progress.Total)
	}
	if resp.Current == nil || resp.Current.Name != "discover" {
		t.Error("expected current step to be 'discover'")
	}
}

// P4': plan is rejected in action="start". The two-phase bootstrap
// (route commit → plan production in discover step) keeps reasoning
// spaces distinct. Silent-accept of plan in start was a principle-of-
// least-astonishment violation — the agent passed it, thought it stuck,
// and didn't notice until three calls later.
func TestWorkflowTool_Action_BootstrapStart_RejectsPlan(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "proj1", nil, nil, engine, nil, "", "", nil, nil, runtime.Info{})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"intent":   "ruby app",
		"route":    "classic",
		"plan": []map[string]any{
			{
				"runtime": map[string]any{
					"devHostname":   "appdev",
					"type":          "nodejs@22",
					"bootstrapMode": "standard",
				},
			},
		},
	})
	if !result.IsError {
		t.Fatalf("expected error when plan is submitted in start, got: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	for _, needle := range []string{"plan is not accepted", "complete", "discover"} {
		if !strings.Contains(text, needle) {
			t.Errorf("error missing hint %q. Got:\n%s", needle, text)
		}
	}
}

// TestWorkflowTool_Action_BootstrapStart_Discovery covers the first-call
// discovery response. No route → ranked options + no session committed.
func TestWorkflowTool_Action_BootstrapStart_Discovery(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "proj1", nil, nil, engine, nil, "", "", nil, nil, runtime.Info{})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"intent":   "bun + postgres",
	})
	if result.IsError {
		t.Fatalf("unexpected error on discovery call: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	var resp workflow.BootstrapDiscoveryResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("failed to parse discovery response: %v", err)
	}
	if len(resp.RouteOptions) == 0 {
		t.Fatal("discovery response must include at least one option (classic is always included)")
	}
	// Last option is always classic.
	last := resp.RouteOptions[len(resp.RouteOptions)-1]
	if last.Route != workflow.BootstrapRouteClassic {
		t.Errorf("last route option = %q, want classic", last.Route)
	}
	// No session should have been committed.
	if engine.HasActiveSession() {
		t.Error("discovery call committed a session; it must not")
	}
}

// TestWorkflowTool_Action_BootstrapStart_Discovery_ThenCommitRecipe chains
// the two-step flow end-to-end: call start without route, read a recipe slug
// from the DiscoveryResponse, then call start again with that slug. The
// commit must resolve the slug and pre-populate RecipeMatch on state.
func TestWorkflowTool_Action_BootstrapStart_Discovery_ThenCommitRecipe(t *testing.T) {
	t.Parallel()
	docs := map[string]*knowledge.Document{
		"zerops://recipes/laravel-minimal": {
			URI:        "zerops://recipes/laravel-minimal",
			Title:      "Laravel Minimal",
			Languages:  []string{"php"},
			Frameworks: []string{"laravel"},
			ImportYAML: "services:\n  - hostname: app\n    type: php-nginx@8.4\n",
		},
	}
	store, err := knowledge.NewStore(docs)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, store)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "proj1", nil, nil, engine, nil, "", "", nil, nil, runtime.Info{})

	// Step 1 — discovery, no route.
	discResult := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"intent":   "Laravel weather dashboard",
	})
	if discResult.IsError {
		t.Fatalf("discovery: %s", getTextContent(t, discResult))
	}
	var disc workflow.BootstrapDiscoveryResponse
	if err := json.Unmarshal([]byte(getTextContent(t, discResult)), &disc); err != nil {
		t.Fatalf("parse discovery: %v", err)
	}

	// Find the first recipe option — it's the slug we'll commit.
	var slug string
	for _, opt := range disc.RouteOptions {
		if opt.Route == workflow.BootstrapRouteRecipe {
			slug = opt.RecipeSlug
			break
		}
	}
	if slug == "" {
		t.Fatalf("no recipe option in discovery response: %+v", disc.RouteOptions)
	}
	if slug != "laravel-minimal" {
		t.Errorf("top recipe slug = %q, want laravel-minimal", slug)
	}
	if engine.HasActiveSession() {
		t.Fatal("discovery leaked a session")
	}

	// Step 2 — commit with the chosen slug.
	commitResult := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":     "start",
		"workflow":   "bootstrap",
		"intent":     "Laravel weather dashboard",
		"route":      "recipe",
		"recipeSlug": slug,
	})
	if commitResult.IsError {
		t.Fatalf("commit: %s", getTextContent(t, commitResult))
	}
	var resp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(getTextContent(t, commitResult)), &resp); err != nil {
		t.Fatalf("parse commit: %v", err)
	}
	if resp.SessionID == "" {
		t.Error("commit response missing sessionId")
	}
	if !engine.HasActiveSession() {
		t.Fatal("commit did not create a session")
	}

	// Verify the recipe match was preloaded onto state from the slug.
	state, err := engine.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state.Bootstrap == nil || state.Bootstrap.Route != workflow.BootstrapRouteRecipe {
		t.Errorf("state route = %q, want recipe", state.Bootstrap.Route)
	}
	if state.Bootstrap.RecipeMatch == nil || state.Bootstrap.RecipeMatch.Slug != slug {
		t.Errorf("recipe match not preloaded: %+v", state.Bootstrap.RecipeMatch)
	}
	if state.Bootstrap.RecipeMatch.ImportYAML == "" {
		t.Error("recipe ImportYAML not preloaded from corpus lookup")
	}
}

// TestWorkflowTool_Action_BootstrapStart_RouteResume_DispatchesToResume
// covers the tool-layer dispatch for route="resume": the handler should
// delegate to handleResume with the sessionId from the input. Uses a
// planted dead session to verify the takeover path fires without error.
func TestWorkflowTool_Action_BootstrapStart_RouteResume_DispatchesToResume(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Plant an abandoned bootstrap session with a dead PID.
	const sessionID = "resume-dispatch-001"
	planted := &workflow.WorkflowState{
		Version:   "2",
		SessionID: sessionID,
		PID:       9999999, // dead PID
		ProjectID: "proj1",
		Workflow:  workflow.WorkflowBootstrap,
		Intent:    "interrupted",
		CreatedAt: "2026-04-20T10:00:00Z",
		UpdatedAt: "2026-04-20T10:00:00Z",
		Bootstrap: workflow.NewBootstrapState(),
	}
	if err := workflow.SaveSessionState(dir, sessionID, planted); err != nil {
		t.Fatalf("SaveSessionState: %v", err)
	}
	entry := workflow.SessionEntry{
		SessionID: sessionID,
		PID:       9999999,
		Workflow:  workflow.WorkflowBootstrap,
		ProjectID: "proj1",
		CreatedAt: "2026-04-20T10:00:00Z",
		UpdatedAt: "2026-04-20T10:00:00Z",
	}
	if err := workflow.RegisterSession(dir, entry); err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}

	engine := workflow.NewEngine(dir, workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "proj1", nil, nil, engine, nil, "", "", nil, nil, runtime.Info{})

	// Error path: route=resume without sessionId must surface INVALID_PARAMETER.
	missingSid := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"route":    "resume",
	})
	if !missingSid.IsError {
		t.Fatal("expected error when route=resume omits sessionId")
	}
	if !strings.Contains(getTextContent(t, missingSid), "sessionId") {
		t.Errorf("error should mention sessionId: %s", getTextContent(t, missingSid))
	}

	// Happy path: sessionId supplied → tool layer delegates to handleResume,
	// engine claims the dead session, response is the bootstrap status.
	ok := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":    "start",
		"workflow":  "bootstrap",
		"route":     "resume",
		"sessionId": sessionID,
	})
	if ok.IsError {
		t.Fatalf("route=resume dispatch failed: %s", getTextContent(t, ok))
	}
	if !engine.HasActiveSession() {
		t.Error("after resume dispatch, engine should hold the claimed session")
	}
	if engine.SessionID() != sessionID {
		t.Errorf("engine sessionId = %q, want %q", engine.SessionID(), sessionID)
	}
}

func TestWorkflowTool_Action_BootstrapComplete(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "proj1", nil, nil, engine, nil, "", "", nil, nil, runtime.Info{})

	// Start bootstrap.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "route": "classic",
	})

	// Complete discover step.
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":      "complete",
		"step":        "discover",
		"attestation": "FRESH project, no existing services",
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	var resp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.Current == nil || resp.Current.Name != "provision" {
		t.Error("expected current step to be 'provision'")
	}
}

func TestWorkflowTool_Action_BootstrapComplete_MissingFields(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "proj1", nil, nil, engine, nil, "", "", nil, nil, runtime.Info{})

	// Missing step.
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete", "attestation": "test attestation here",
	})
	if !result.IsError {
		t.Error("expected IsError for missing step")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "INVALID_PARAMETER") {
		t.Errorf("expected INVALID_PARAMETER error, got: %s", text)
	}

	// Missing attestation.
	result = callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete", "step": "discover",
	})
	if !result.IsError {
		t.Error("expected IsError for missing attestation")
	}
}

func TestWorkflowTool_Action_BootstrapSkip(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "proj1", nil, nil, engine, nil, "", "", nil, nil, runtime.Info{})

	// Start and advance to close (managed-only plan, so close can be skipped).
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "route": "classic",
	})
	// Submit empty plan (managed-only) so close can be skipped.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete", "step": "discover",
		"plan": []map[string]any{},
	})
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete", "step": "provision",
		"attestation": "Attestation for provision completed ok",
	})

	// Skip close (allowed for managed-only plan — no runtime services to register).
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "skip",
		"step":   "close",
		"reason": "managed-only plan, no runtime registration needed",
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	var resp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	// After skipping the final step, bootstrap is complete — Current is nil.
	if resp.Current != nil {
		t.Errorf("expected nil current after skipping final step, got: %s", resp.Current.Name)
	}
	if resp.Progress.Completed != 3 {
		t.Errorf("Completed: want 3 (2 done + 1 skipped), got %d", resp.Progress.Completed)
	}
}

func TestWorkflowTool_Action_BootstrapStatus(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "proj1", nil, nil, engine, nil, "", "", nil, nil, runtime.Info{})

	// Start bootstrap.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "route": "classic",
	})

	// Get status.
	result := callTool(t, srv, "zerops_workflow", map[string]any{"action": "status"})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	var resp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.Progress.Total != 3 {
		t.Errorf("Total: want 3, got %d", resp.Progress.Total)
	}
}

func TestWorkflowTool_Action_BootstrapComplete_DiscoverStep_Structured(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "proj1", nil, nil, engine, nil, "", "", nil, nil, runtime.Info{})

	// Start bootstrap.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "route": "classic",
	})

	// Complete discover step with structured plan.
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete",
		"step":   "discover",
		"plan": []any{
			map[string]any{
				"runtime": map[string]any{"devHostname": "appdev", "type": "bun@1.2", "stageHostname": "appstage"},
				"dependencies": []any{
					map[string]any{"hostname": "db", "type": "postgresql@16", "mode": "NON_HA", "resolution": "CREATE"},
				},
			},
		},
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	var resp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.Current == nil || resp.Current.Name != "provision" {
		t.Error("expected current step to be 'provision'")
	}
}

func TestWorkflowTool_Action_BootstrapComplete_DiscoverStep_InvalidPlan(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "proj1", nil, nil, engine, nil, "", "", nil, nil, runtime.Info{})

	// Start bootstrap.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "route": "classic",
	})

	// Complete discover step with invalid hostname.
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete",
		"step":   "discover",
		"plan": []any{
			map[string]any{
				"runtime": map[string]any{"devHostname": "my-app", "type": "bun@1.2"},
			},
		},
	})

	if !result.IsError {
		t.Error("expected error for invalid hostname in plan")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "invalid hostname") {
		t.Errorf("expected 'invalid hostname' error, got: %s", text)
	}
}

func TestWorkflow_BootstrapStart_IncludesStacks(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServiceStackTypes([]platform.ServiceStackType{
		{
			Name:     "Go",
			Category: "USER",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "go@1", Status: statusActive},
			},
		},
		{
			Name:     "PostgreSQL",
			Category: "STANDARD",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "postgresql@16", Status: statusActive},
			},
		},
	})
	cache := ops.NewStackTypeCache(1 * time.Hour)
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, mock, nil, "proj1", cache, nil, engine, nil, "", "", nil, nil, runtime.Info{})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"route":    "classic",
		"intent":   "go + postgres",
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	var resp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.AvailableStacks == "" {
		t.Error("expected availableStacks to be populated")
	}
	if !strings.Contains(resp.AvailableStacks, "go@1") {
		t.Errorf("availableStacks missing go@1: %s", resp.AvailableStacks)
	}
	if !strings.Contains(resp.AvailableStacks, "postgresql@16") {
		t.Errorf("availableStacks missing postgresql@16: %s", resp.AvailableStacks)
	}
}

func TestWorkflow_BootstrapStart_NoCache_OmitsStacks(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "proj1", nil, nil, engine, nil, "", "", nil, nil, runtime.Info{})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"intent":   "bun app",
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	var resp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.AvailableStacks != "" {
		t.Errorf("expected empty availableStacks without cache, got: %s", resp.AvailableStacks)
	}
}

func TestWorkflow_BootstrapComplete_IncludesStacks_OnDiscoverStep(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServiceStackTypes([]platform.ServiceStackType{
		{
			Name:     "Bun",
			Category: "USER",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "bun@1.2", Status: statusActive},
			},
		},
	})
	cache := ops.NewStackTypeCache(1 * time.Hour)
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, mock, nil, "proj1", cache, nil, engine, nil, "", "", nil, nil, runtime.Info{})

	// Start bootstrap — current step is discover, should include stacks.
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "route": "classic",
	})
	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	var resp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.AvailableStacks == "" {
		t.Error("expected availableStacks at discover step")
	}
	if !strings.Contains(resp.AvailableStacks, "bun@1.2") {
		t.Errorf("availableStacks missing bun@1.2: %s", resp.AvailableStacks)
	}

	// Complete discover — moves to provision, which should NOT include stacks.
	result = callTool(t, srv, "zerops_workflow", map[string]any{
		"action":      "complete",
		"step":        "discover",
		"attestation": "FRESH project, no existing services",
	})
	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
	text = getTextContent(t, result)
	var resp2 workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(text), &resp2); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp2.AvailableStacks != "" {
		t.Errorf("expected empty availableStacks at provision step, got: %s", resp2.AvailableStacks)
	}
}

func TestWorkflow_BootstrapStatus_IncludesStacks(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServiceStackTypes([]platform.ServiceStackType{
		{
			Name:     "Node.js",
			Category: "USER",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "nodejs@22", Status: statusActive},
			},
		},
	})
	cache := ops.NewStackTypeCache(1 * time.Hour)
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, mock, nil, "proj1", cache, nil, engine, nil, "", "", nil, nil, runtime.Info{})

	// Start bootstrap.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "route": "classic",
	})

	// Get status.
	result := callTool(t, srv, "zerops_workflow", map[string]any{"action": "status"})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	var resp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.AvailableStacks == "" {
		t.Error("expected availableStacks in status response")
	}
	if !strings.Contains(resp.AvailableStacks, "nodejs@22") {
		t.Errorf("availableStacks missing nodejs@22: %s", resp.AvailableStacks)
	}
}

func TestWorkflowTool_Action_Resume_MissingSessionID(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "proj1", nil, nil, engine, nil, "", "", nil, nil, runtime.Info{})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "resume",
	})
	if !result.IsError {
		t.Error("expected error for resume without sessionId")
	}
}

// --- Item 26: populateStacks gated to discover+generate ---

func TestWorkflowTool_BootstrapStatus_NoStacks_DeployStep(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServiceStackTypes([]platform.ServiceStackType{
		{
			Name:     "Node.js",
			Category: "USER",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "nodejs@22", Status: statusActive},
			},
		},
	})
	cache := ops.NewStackTypeCache(1 * time.Hour)
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, mock, nil, "proj1", cache, nil, engine, nil, "", "", nil, nil, runtime.Info{})

	// Start bootstrap and advance to deploy step.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "route": "classic",
	})
	for _, step := range []string{"discover", "provision", "generate"} {
		callTool(t, srv, "zerops_workflow", map[string]any{
			"action": "complete", "step": step,
			"attestation": "Attestation for " + step + " completed ok",
		})
	}

	// At deploy step, status should NOT include stacks.
	result := callTool(t, srv, "zerops_workflow", map[string]any{"action": "status"})
	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	var resp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.AvailableStacks != "" {
		t.Errorf("expected empty availableStacks at deploy step, got: %s", resp.AvailableStacks)
	}
}

func TestWorkflowTool_Action_BootstrapComplete_DiscoverStep_FallbackAttestation(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "proj1", nil, nil, engine, nil, "", "", nil, nil, runtime.Info{})

	// Start bootstrap.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "route": "classic",
	})

	// Complete discover step with attestation only (no structured plan).
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":      "complete",
		"step":        "discover",
		"attestation": "Services: appdev (bun@1.2), db (postgresql@16 NON_HA) — validated manually",
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
}

func TestWorkflowTool_Resume_Bootstrap_ReturnsBootstrapResponse(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvLocal, nil)

	// Start bootstrap and advance to provision.
	resp, err := engine.BootstrapStart("proj1", "bun + postgres")
	if err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}
	sessionID := resp.SessionID

	// Complete discover to advance to provision.
	if _, err := engine.BootstrapComplete(context.Background(), "discover", "FRESH project, plan submitted", nil); err != nil {
		t.Fatalf("BootstrapComplete: %v", err)
	}

	// Overwrite session PID to a dead value.
	state, err := workflow.LoadSessionByID(dir, sessionID)
	if err != nil {
		t.Fatalf("LoadSessionByID: %v", err)
	}
	state.PID = 9999999
	if err := workflow.SaveSessionState(dir, sessionID, state); err != nil {
		t.Fatalf("SaveSessionState: %v", err)
	}

	// Create new engine (fresh PID) and resume.
	engine2 := workflow.NewEngine(dir, workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "proj1", nil, nil, engine2, nil, "", "", nil, nil, runtime.Info{})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":    "resume",
		"sessionId": sessionID,
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	var bootstrapResp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(text), &bootstrapResp); err != nil {
		t.Fatalf("failed to parse BootstrapResponse: %v", err)
	}
	if bootstrapResp.Current == nil {
		t.Fatal("expected non-nil current step")
	}
	if bootstrapResp.Progress.Total != 3 {
		t.Errorf("Progress.Total: want 3, got %d", bootstrapResp.Progress.Total)
	}
	if bootstrapResp.Current.DetailedGuide == "" {
		t.Error("expected non-empty detailedGuide in resume response")
	}
}

func TestWorkflowTool_Iterate_Bootstrap_ReturnsBootstrapResponse(t *testing.T) {
	t.Parallel()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "proj1", nil, nil, engine, nil, "", "", nil, nil, runtime.Info{})

	// Start bootstrap and advance to a mid-flight step.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "route": "classic", "intent": "test",
	})
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete", "step": "discover",
		"plan": []map[string]any{},
	})

	// Iterate — under Option A, bootstrap doesn't reset steps (retry hard-stops and
	// escalates to the user). Iteration counter increments, step state is preserved.
	result := callTool(t, srv, "zerops_workflow", map[string]any{"action": "iterate"})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	var bootstrapResp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(text), &bootstrapResp); err != nil {
		t.Fatalf("failed to parse BootstrapResponse: %v", err)
	}
	if bootstrapResp.Current == nil {
		t.Fatal("expected non-nil current step after iterate")
	}
	if bootstrapResp.Current.Name != "provision" {
		t.Errorf("Current.Name: want provision (unchanged by iterate), got %s", bootstrapResp.Current.Name)
	}
	if bootstrapResp.Progress.Total != 3 {
		t.Errorf("Progress.Total: want 3, got %d", bootstrapResp.Progress.Total)
	}
}

// --- Auto-Mount Tests ---

// testMounter is a minimal Mounter mock for auto-mount tests.
type testMounter struct {
	mounted  map[string]bool
	mountErr error
}

func newTestMounter() *testMounter {
	return &testMounter{mounted: make(map[string]bool)}
}

func (m *testMounter) CheckMount(_ context.Context, path string) (platform.MountState, error) {
	if m.mounted[path] {
		return platform.MountStateActive, nil
	}
	return platform.MountStateNotMounted, nil
}
func (m *testMounter) Mount(_ context.Context, _, localPath string) error {
	if m.mountErr != nil {
		return m.mountErr
	}
	m.mounted[localPath] = true
	return nil
}
func (m *testMounter) Unmount(_ context.Context, _, _ string) error                { return nil }
func (m *testMounter) ForceUnmount(_ context.Context, _, _ string) error           { return nil }
func (m *testMounter) IsWritable(_ context.Context, _ string) (bool, error)        { return true, nil }
func (m *testMounter) ListMountDirs(_ context.Context, _ string) ([]string, error) { return nil, nil }
func (m *testMounter) HasUnit(_ context.Context, _ string) (bool, error)           { return false, nil }
func (m *testMounter) CleanupUnit(_ context.Context, _ string) error               { return nil }

func TestBootstrapProvision_AutoMount_ContainerEnv(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-1", Name: "appdev", Status: serviceStatusRunning},
		{ID: "svc-2", Name: "db", Status: serviceStatusRunning},
	}).WithServiceEnv("svc-2", []platform.EnvVar{{Key: "connectionString", Content: "pg://..."}})
	mounter := newTestMounter()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvContainer, nil)

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, mock, nil, "proj1", nil, nil, engine, nil, "", "", mounter, nil, runtime.Info{})

	// Start bootstrap.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "route": "classic", "intent": "node + postgres",
	})

	// Complete discover with plan (dev mode — no stage service).
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete", "step": "discover",
		"plan": []any{
			map[string]any{
				"runtime":      map[string]any{"devHostname": "appdev", "type": "nodejs@22", "bootstrapMode": "dev"},
				"dependencies": []any{map[string]any{"hostname": "db", "type": "postgresql@16", "resolution": "CREATE"}},
			},
		},
	})

	// Complete provision — should trigger auto-mount.
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete", "step": "provision",
		"attestation": "All services created, env vars discovered",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	var resp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	// Verify auto-mount results.
	if len(resp.AutoMounts) == 0 {
		t.Fatal("expected auto-mount results after provision")
	}
	if resp.AutoMounts[0].Hostname != "appdev" {
		t.Errorf("AutoMounts[0].Hostname = %q, want appdev", resp.AutoMounts[0].Hostname)
	}
	if resp.AutoMounts[0].Status != mountStatusMounted {
		t.Errorf("AutoMounts[0].Status = %q, want MOUNTED", resp.AutoMounts[0].Status)
	}
	if resp.AutoMounts[0].MountPath == "" {
		t.Error("expected non-empty MountPath")
	}

	// Verify mounter was called.
	if !mounter.mounted["/var/www/appdev"] {
		t.Error("expected /var/www/appdev to be mounted")
	}
}

func TestBootstrapProvision_AutoMount_LocalEnv_NoMount(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-1", Name: "appdev", Status: serviceStatusRunning},
	})
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	// mounter is nil — simulates local environment.
	RegisterWorkflow(srv, mock, nil, "proj1", nil, nil, engine, nil, "", "", nil, nil, runtime.Info{})

	// Start and advance to provision.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "route": "classic", "intent": "node app",
	})
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete", "step": "discover",
		"plan": []any{
			map[string]any{
				"runtime": map[string]any{"devHostname": "appdev", "type": "nodejs@22", "bootstrapMode": "dev"},
			},
		},
	})

	// Complete provision — no mounter, no auto-mount.
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete", "step": "provision",
		"attestation": "All services created, no env vars",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	var resp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if len(resp.AutoMounts) != 0 {
		t.Errorf("expected no auto-mounts in local env, got %d", len(resp.AutoMounts))
	}
}

func TestBootstrapProvision_AutoMount_MultipleTargets(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-1", Name: "appdev", Status: serviceStatusRunning},
		{ID: "svc-2", Name: "apidev", Status: serviceStatusRunning},
	})
	mounter := newTestMounter()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvContainer, nil)

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, mock, nil, "proj1", nil, nil, engine, nil, "", "", mounter, nil, runtime.Info{})

	// Start bootstrap.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "route": "classic", "intent": "app + api",
	})

	// Plan with 2 runtime targets (dev mode, no managed deps).
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete", "step": "discover",
		"plan": []any{
			map[string]any{
				"runtime": map[string]any{"devHostname": "appdev", "type": "nodejs@22", "bootstrapMode": "dev"},
			},
			map[string]any{
				"runtime": map[string]any{"devHostname": "apidev", "type": "go@1", "bootstrapMode": "dev"},
			},
		},
	})

	// Complete provision.
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete", "step": "provision",
		"attestation": "All services created and running",
	})

	text := getTextContent(t, result)
	var resp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	// Both runtime targets should be mounted.
	if len(resp.AutoMounts) != 2 {
		t.Fatalf("expected 2 auto-mounts, got %d", len(resp.AutoMounts))
	}

	hostnames := map[string]bool{}
	for _, am := range resp.AutoMounts {
		hostnames[am.Hostname] = true
		if am.Status != mountStatusMounted {
			t.Errorf("AutoMount %s status = %q, want MOUNTED", am.Hostname, am.Status)
		}
	}
	if !hostnames["appdev"] || !hostnames["apidev"] {
		t.Errorf("expected appdev and apidev in auto-mounts, got %v", hostnames)
	}
}

func TestBootstrapProvision_AutoMount_Failure_NonFatal(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-1", Name: "appdev", Status: serviceStatusRunning},
	})
	mounter := newTestMounter()
	mounter.mountErr = errors.New("mount: SSHFS unavailable")
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvContainer, nil)

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, mock, nil, "proj1", nil, nil, engine, nil, "", "", mounter, nil, runtime.Info{})

	// Start and plan.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "route": "classic", "intent": "node app",
	})
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete", "step": "discover",
		"plan": []any{
			map[string]any{
				"runtime": map[string]any{"devHostname": "appdev", "type": "nodejs@22", "bootstrapMode": "dev"},
			},
		},
	})

	// Complete provision — mount will fail but step should still succeed.
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete", "step": "provision",
		"attestation": "All services created, env vars discovered",
	})

	if result.IsError {
		t.Fatalf("mount failure should not fail the step: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	var resp workflow.BootstrapResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	// Step advanced to close despite mount failure.
	if resp.Current == nil || resp.Current.Name != "close" {
		t.Error("expected current step to be 'close' even after mount failure")
	}

	// Mount failure reported in auto-mounts.
	if len(resp.AutoMounts) == 0 {
		t.Fatal("expected auto-mount results even on failure")
	}
	if resp.AutoMounts[0].Status != "FAILED" {
		t.Errorf("AutoMounts[0].Status = %q, want FAILED", resp.AutoMounts[0].Status)
	}
	if resp.AutoMounts[0].Error == "" {
		t.Error("expected error message in failed auto-mount")
	}
}
