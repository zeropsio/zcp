package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestRecipeStart_ModelGate covers the v14 client-model enforcement that
// replaces v13's silent acceptance of any model. The recipe workflow
// rejects non-Opus models (v13 shipped on Sonnet and doubled wall time)
// and Opus variants without 1M context (the full guidance payload plus
// code-writing context does not fit in 200k). Missing clientModel is
// also rejected so the agent has to surface the requirement — we cannot
// observe the actual running model from the server side, the gate has
// to force the agent to report it.
func TestRecipeStart_ModelGate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		clientModel string
		wantError   bool
	}{
		{name: "accepted Opus 4.6 1m", clientModel: "claude-opus-4-6[1m]", wantError: false},
		{name: "accepted Opus 4.7 1m", clientModel: "claude-opus-4-7[1m]", wantError: false},
		{name: "missing clientModel rejected", clientModel: "", wantError: true},
		{name: "Sonnet rejected", clientModel: "claude-sonnet-4-6", wantError: true},
		{name: "Sonnet 1m rejected", clientModel: "claude-sonnet-4-6[1m]", wantError: true},
		{name: "Opus without 1m rejected", clientModel: "claude-opus-4-6", wantError: true},
		{name: "Opus 4.7 without 1m rejected", clientModel: "claude-opus-4-7", wantError: true},
		{name: "plain opus alias rejected", clientModel: "opus", wantError: true},
		{name: "Haiku rejected", clientModel: "claude-haiku-4-5", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
			RegisterWorkflow(srv, nil, "proj1", nil, nil, engine, nil, "", "", nil, runtime.Info{})

			input := map[string]any{
				"action":   "start",
				"workflow": "recipe",
				"intent":   "Create a Laravel minimal recipe",
				"tier":     "minimal",
			}
			if tt.clientModel != "" {
				input["clientModel"] = tt.clientModel
			}
			result := callTool(t, srv, "zerops_workflow", input)
			if result.IsError != tt.wantError {
				t.Errorf("clientModel=%q: IsError=%v, want %v (response: %s)", tt.clientModel, result.IsError, tt.wantError, getTextContent(t, result))
			}
		})
	}
}

func TestRecipeStart_Success(t *testing.T) {
	t.Parallel()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, nil, engine, nil, "", "", nil, runtime.Info{})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":      "start",
		"workflow":    "recipe",
		"intent":      "Create a Laravel minimal recipe",
		"tier":        "minimal",
		"clientModel": "claude-opus-4-6[1m]",
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", getTextContent(t, result))
	}

	text := getTextContent(t, result)
	var resp workflow.RecipeResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.Progress.Total != 6 {
		t.Errorf("expected 6 total steps, got %d", resp.Progress.Total)
	}
	if resp.Current == nil {
		t.Fatal("expected Current to be set")
	}
	if resp.Current.Name != "research" {
		t.Errorf("expected current step %q, got %q", "research", resp.Current.Name)
	}
	if !engine.HasActiveSession() {
		t.Error("expected active session after start")
	}
	// v39 Commit 5b — start response carries the canonical step + sub-step
	// breakdown as starter todos. The main agent pastes these into its
	// first TodoWrite call; absence here forces the v38 re-derivation loop.
	if len(resp.StartingTodos) < 6 {
		t.Errorf("expected starting todos to cover every step (≥ 6 entries), got %d", len(resp.StartingTodos))
	}
}

// TestWorkflowStart_IncludesStartingTodos — v39 Commit 5b. Asserts the
// showcase-tier action=start response includes the canonical step +
// sub-step breakdown the main agent pastes into TodoWrite. Minimal
// tier also carries starter todos but without showcase-only sub-steps.
func TestWorkflowStart_IncludesStartingTodos(t *testing.T) {
	t.Parallel()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, nil, engine, nil, "", "", nil, runtime.Info{})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":      "start",
		"workflow":    "recipe",
		"intent":      "Create a NestJS showcase recipe",
		"tier":        "showcase",
		"clientModel": "claude-opus-4-7[1m]",
	})
	if result.IsError {
		t.Fatalf("expected success, got error: %s", getTextContent(t, result))
	}

	var resp workflow.RecipeResponse
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Showcase tier includes the feature sub-agent + close-review +
	// browser-walk sub-steps — their presence in the starter todos is
	// the signal the tier path is live.
	wantSubstring := []string{
		"Recipe step: research",
		"Recipe step: deploy",
		"Recipe step: close",
		"substep deploy.subagent",
		"substep deploy.browser-walk",
		"substep close.editorial-review",
		"substep close.code-review",
		"substep close.close-browser-walk",
	}
	joined := strings.Join(resp.StartingTodos, "\n")
	for _, want := range wantSubstring {
		if !strings.Contains(joined, want) {
			t.Errorf("showcase starter todos missing %q\nfull list:\n%s", want, joined)
		}
	}
}

// TestRecipeStart_DefaultTierRejected — v8.100 replaces the prior
// TestRecipeStart_DefaultTier which asserted an empty tier silently
// defaulted to "minimal". The silent default dropped showcase-intent
// agents into minimal research guidance without the showcase rules
// (NATS queue required, BullMQ disqualifies shared-codebase, load ONE
// reference recipe). Empty tier is now rejected up front with a
// message naming both valid values.
func TestRecipeStart_DefaultTierRejected(t *testing.T) {
	t.Parallel()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, nil, engine, nil, "", "", nil, runtime.Info{})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":      "start",
		"workflow":    "recipe",
		"intent":      "Create a recipe",
		"clientModel": "claude-opus-4-6[1m]",
	})

	if !result.IsError {
		t.Fatalf("expected tier-required rejection, got success: %s", getTextContent(t, result))
	}
}

func TestRecipeStart_InvalidTier(t *testing.T) {
	t.Parallel()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, nil, engine, nil, "", "", nil, runtime.Info{})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "recipe",
		"intent":   "Create a recipe",
		"tier":     "invalid",
	})

	if !result.IsError {
		t.Fatal("expected error for invalid tier")
	}
}

func TestRecipeComplete_Research(t *testing.T) {
	t.Parallel()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, nil, engine, nil, "", "", nil, runtime.Info{})

	// Start recipe session.
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":      "start",
		"workflow":    "recipe",
		"intent":      "Create a Laravel recipe",
		"tier":        "minimal",
		"clientModel": "claude-opus-4-6[1m]",
	})
	if result.IsError {
		t.Fatalf("start failed: %s", getTextContent(t, result))
	}

	// Complete research with plan.
	result = callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete",
		"step":   "research",
		"recipePlan": map[string]any{
			"framework":   "laravel",
			"tier":        "minimal",
			"slug":        "laravel-hello-world",
			"runtimeType": "php-nginx@8.4",
			"buildBases":  []any{"php@8.4", "nodejs@22"},
			"decisions": map[string]any{
				"webServer":  "nginx-sidecar",
				"buildBase":  "php@8.4",
				"os":         "ubuntu-22",
				"devTooling": "manual",
			},
			"research": map[string]any{
				"serviceType":    "php-nginx",
				"packageManager": "composer",
				"httpPort":       80,
				"buildCommands":  []any{"composer install", "npm run build"},
				"deployFiles":    []any{"."},
				"startCommand":   "php artisan serve",
				"cacheStrategy":  []any{"vendor", "node_modules"},
				"dbDriver":       "mysql",
				"migrationCmd":   "php artisan migrate",
				"needsAppSecret": true,
				"loggingDriver":  "stderr",
			},
			"targets": []any{
				map[string]any{
					"hostname":     "app",
					"type":         "php-nginx@8.4",
					"environments": []any{"0", "1", "2", "3", "4", "5"},
				},
			},
			"features": []any{
				map[string]any{
					"id":          "greeting",
					"description": "Fetch a greeting row from the database and render it.",
					"surface":     []any{"api", "ui", "db"},
					"healthCheck": "/api/greeting",
					"uiTestId":    "greeting",
					"interaction": "Open the page; observe the greeting section populate.",
					"mustObserve": "[data-feature=\"greeting\"] [data-value] text non-empty.",
				},
			},
		},
	})
	if result.IsError {
		t.Fatalf("complete research failed: %s", getTextContent(t, result))
	}

	text := getTextContent(t, result)
	var resp workflow.RecipeResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.Current == nil {
		t.Fatal("expected Current step after research")
	}
	if resp.Current.Name != "provision" {
		t.Errorf("expected next step %q, got %q", "provision", resp.Current.Name)
	}
	if resp.Progress.Completed != 1 {
		t.Errorf("expected 1 completed, got %d", resp.Progress.Completed)
	}
}

func TestRecipeComplete_MissingPlan(t *testing.T) {
	t.Parallel()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, nil, engine, nil, "", "", nil, runtime.Info{})

	// Start.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "recipe", "intent": "test", "tier": "minimal", "clientModel": "claude-opus-4-6[1m]",
	})

	// Complete research without plan.
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete",
		"step":   "research",
	})

	if !result.IsError {
		t.Fatal("expected error when completing research without plan")
	}
}

func TestRecipeSkip_CloseAllowed(t *testing.T) {
	t.Parallel()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvLocal, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, nil, engine, nil, dir, "", nil, runtime.Info{})

	// Start and advance to close step via engine directly.
	resp, err := engine.RecipeStart("proj1", "test recipe", "minimal")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp

	// Complete 5 steps to reach close.
	plan := workflow.RecipePlan{
		Framework:   "bun",
		Tier:        "minimal",
		Slug:        "bun-hello-world",
		RuntimeType: "bun@1",
		Research: workflow.ResearchData{
			ServiceType:    "bun",
			PackageManager: "bun",
			HTTPPort:       3000,
			BuildCommands:  []string{"bun install"},
			DeployFiles:    []string{"."},
			StartCommand:   "bun run start",
			DBDriver:       "none",
			MigrationCmd:   "none",
			LoggingDriver:  "stderr",
		},
		Targets: []workflow.RecipeTarget{
			{Hostname: "app", Type: "bun@1", Environments: []string{"0", "1", "2", "3", "4", "5"}},
		},
		Features: minimalToolsTestFeatures(),
	}
	if _, err := engine.RecipeCompletePlan(plan, "research done for bun", nil, nil); err != nil {
		t.Fatalf("complete plan: %v", err)
	}

	steps := []string{"provision", "generate", "deploy", "finalize"}
	for _, step := range steps {
		if _, err := engine.RecipeComplete(context.TODO(), step, "completed: "+step+" step", nil); err != nil {
			t.Fatalf("complete %s: %v", step, err)
		}
	}

	// Now skip close via tool.
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "skip",
		"step":   "close",
		"reason": "not needed",
	})

	if result.IsError {
		t.Fatalf("skip close should succeed: %s", getTextContent(t, result))
	}
}

func TestRecipeStatus(t *testing.T) {
	t.Parallel()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, nil, engine, nil, "", "", nil, runtime.Info{})

	// Start recipe.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "recipe", "intent": "test", "tier": "minimal", "clientModel": "claude-opus-4-6[1m]",
	})

	// Get status.
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "status",
	})

	if result.IsError {
		t.Fatalf("status failed: %s", getTextContent(t, result))
	}

	text := getTextContent(t, result)
	var resp workflow.RecipeResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.Progress.Total != 6 {
		t.Errorf("expected 6 steps, got %d", resp.Progress.Total)
	}
}

func TestRecipeAutoReset_CompletedSession(t *testing.T) {
	t.Parallel()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, nil, engine, nil, "", "", nil, runtime.Info{})

	// Start and complete all recipe steps.
	if _, err := engine.RecipeStart("proj1", "test", "minimal"); err != nil {
		t.Fatal(err)
	}

	plan := workflow.RecipePlan{
		Framework: "bun", Tier: "minimal", Slug: "bun-hello-world",
		RuntimeType: "bun@1",
		Research: workflow.ResearchData{
			ServiceType: "bun", PackageManager: "bun", HTTPPort: 3000,
			BuildCommands: []string{"bun install"}, DeployFiles: []string{"."},
			StartCommand: "bun run start", DBDriver: "none",
			MigrationCmd: "none", LoggingDriver: "stderr",
		},
		Targets: []workflow.RecipeTarget{
			{Hostname: "app", Type: "bun@1", Environments: []string{"0", "1", "2", "3", "4", "5"}},
		},
		Features: minimalToolsTestFeatures(),
	}
	if _, err := engine.RecipeCompletePlan(plan, "research done for testing", nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, step := range []string{"provision", "generate", "deploy", "finalize", "close"} {
		if _, err := engine.RecipeComplete(context.TODO(), step, "completed: "+step+" step", nil); err != nil {
			t.Fatalf("complete %s: %v", step, err)
		}
	}

	// Starting a new recipe should auto-reset the completed session.
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "recipe", "intent": "new recipe", "tier": "showcase", "clientModel": "claude-opus-4-6[1m]",
	})

	if result.IsError {
		t.Fatalf("auto-reset should allow new session: %s", getTextContent(t, result))
	}
}

// minimalToolsTestFeatures returns the minimal valid feature set for
// test fixtures that submit a recipe plan through the tool boundary.
// Uses a single api+ui+db feature — the smallest shape that passes
// validateFeatures for a minimal-tier bun recipe.
func minimalToolsTestFeatures() []workflow.RecipeFeature {
	return []workflow.RecipeFeature{
		{
			ID:          "greeting",
			Description: "Fetch a greeting row from the database and render it.",
			Surface:     []string{"api", "ui", "db"},
			HealthCheck: "/api/greeting",
			UITestID:    "greeting",
			Interaction: "Open the page; observe the greeting section populate.",
			MustObserve: "[data-feature=\"greeting\"] [data-value] text non-empty.",
		},
	}
}

// TestRecipeStart_TierRequired — v8.100. Empty tier is rejected with a
// message naming valid values. Replaces the prior silent "default to
// minimal" which dropped showcase-intent agents into minimal guidance.
func TestRecipeStart_TierRequired(t *testing.T) {
	t.Parallel()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, nil, engine, nil, "", "", nil, runtime.Info{})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":      "start",
		"workflow":    "recipe",
		"intent":      "Create a Nest.js showcase recipe",
		"clientModel": "claude-opus-4-7[1m]",
		// No tier.
	})
	if !result.IsError {
		t.Fatalf("expected tier-required rejection, got success: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "tier is required") {
		t.Errorf("error message must name \"tier is required\"; got: %s", text)
	}
	// Must name both valid values so the agent can classify on the retry.
	for _, v := range []string{"minimal", "showcase"} {
		if !strings.Contains(text, v) {
			t.Errorf("error message must name tier value %q; got: %s", v, text)
		}
	}
}

// TestRecipeStart_TierInvalid — v8.100. Unknown tier values are rejected
// with a message listing valid ones. Replaces relying on the engine's
// generic "invalid tier" error which doesn't surface the valid set.
func TestRecipeStart_TierInvalid(t *testing.T) {
	t.Parallel()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, nil, engine, nil, "", "", nil, runtime.Info{})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":      "start",
		"workflow":    "recipe",
		"intent":      "Create a recipe",
		"tier":        "intermediate", // not valid
		"clientModel": "claude-opus-4-7[1m]",
	})
	if !result.IsError {
		t.Fatalf("expected invalid-tier rejection, got success: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "invalid tier") {
		t.Errorf("error must name \"invalid tier\"; got: %s", text)
	}
	if !strings.Contains(text, "minimal") || !strings.Contains(text, "showcase") {
		t.Errorf("error must list valid values; got: %s", text)
	}
}
