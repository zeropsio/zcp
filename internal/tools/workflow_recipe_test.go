package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestRecipeStart_Success(t *testing.T) {
	t.Parallel()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, nil, engine, nil, "", "", nil)

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "recipe",
		"intent":   "Create a Laravel minimal recipe",
		"tier":     "minimal",
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
}

func TestRecipeStart_DefaultTier(t *testing.T) {
	t.Parallel()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, nil, engine, nil, "", "", nil)

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "recipe",
		"intent":   "Create a recipe",
	})

	if result.IsError {
		t.Fatalf("expected success with default tier, got error: %s", getTextContent(t, result))
	}
}

func TestRecipeStart_InvalidTier(t *testing.T) {
	t.Parallel()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	RegisterWorkflow(srv, nil, "proj1", nil, nil, engine, nil, "", "", nil)

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
	RegisterWorkflow(srv, nil, "proj1", nil, nil, engine, nil, "", "", nil)

	// Start recipe session.
	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "recipe",
		"intent":   "Create a Laravel recipe",
		"tier":     "minimal",
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
	RegisterWorkflow(srv, nil, "proj1", nil, nil, engine, nil, "", "", nil)

	// Start.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "recipe", "intent": "test", "tier": "minimal",
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
	RegisterWorkflow(srv, nil, "proj1", nil, nil, engine, nil, dir, "", nil)

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
	RegisterWorkflow(srv, nil, "proj1", nil, nil, engine, nil, "", "", nil)

	// Start recipe.
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "recipe", "intent": "test", "tier": "minimal",
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
	RegisterWorkflow(srv, nil, "proj1", nil, nil, engine, nil, "", "", nil)

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
		"action": "start", "workflow": "recipe", "intent": "new recipe", "tier": "showcase",
	})

	if result.IsError {
		t.Fatalf("auto-reset should allow new session: %s", getTextContent(t, result))
	}
}
