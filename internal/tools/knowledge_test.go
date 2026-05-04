// Tests for: knowledge.go — zerops_knowledge MCP tool handler.

package tools

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

func testKnowledgeStore(t *testing.T) *knowledge.Store {
	t.Helper()
	docs := map[string]*knowledge.Document{
		"zerops://themes/core": {
			URI:     "zerops://themes/core",
			Title:   "Zerops Core Reference",
			Content: "# Zerops Core Reference\n\nUniversal rules here.",
		},
		"zerops://themes/model": {
			URI:     "zerops://themes/model",
			Title:   "Zerops Platform Model",
			Content: "# Zerops Platform Model\n\n## Container Universe\n\nLinux containers.\n\n## Platform Constraints\n\nBind 0.0.0.0. deployFiles mandatory.",
		},
		"zerops://recipes/php-hello-world": {
			URI:     "zerops://recipes/php-hello-world",
			Title:   "PHP Hello World on Zerops",
			Content: "# PHP Hello World on Zerops\n\nPHP-specific rules.\n\n### Details\nPHP-specific rules.",
		},
		"zerops://recipes/nodejs-hello-world": {
			URI:     "zerops://recipes/nodejs-hello-world",
			Title:   "Node.js Hello World on Zerops",
			Content: "# Node.js Hello World on Zerops\n\nNode.js-specific rules.\n\n### Details\nNode.js-specific rules.",
		},
		"zerops://themes/services": {
			URI:     "zerops://themes/services",
			Title:   "Managed Service Reference",
			Content: "## PostgreSQL\n\nPort 5432.\n\n## Valkey\n\nPort 6379.",
		},
		"zerops://themes/wiring": {
			URI:     "zerops://themes/wiring",
			Title:   "Wiring Patterns",
			Content: "## Syntax Rules\n\nUse ${hostname_var}.\n\n## PostgreSQL\n\nDATABASE_URL.\n\n## Valkey\n\nREDIS_URL.",
		},
		"zerops://recipes/ghost": {
			URI:     "zerops://recipes/ghost",
			Title:   "Ghost Recipe",
			Content: "maxContainers: 1",
		},
	}
	store, err := knowledge.NewStore(docs)
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	return store
}

func TestKnowledgeTool_Basic(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, store, nil, nil, nil, nil)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{"query": "postgresql"})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)

	var parsed []knowledge.SearchResult
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("failed to parse results: %v", err)
	}
	if len(parsed) == 0 {
		t.Error("expected at least one search result")
	}
}

func TestKnowledgeTool_WithLimit(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, store, nil, nil, nil, nil)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{"query": "zerops", "limit": 1})

	if result.IsError {
		t.Errorf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed []knowledge.SearchResult
	text := getTextContent(t, result)
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("failed to parse results: %v", err)
	}
	if len(parsed) > 1 {
		t.Errorf("expected at most 1 result, got %d", len(parsed))
	}
}

func TestKnowledgeTool_EmptyQuery(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, store, nil, nil, nil, nil)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{"query": ""})

	if !result.IsError {
		t.Error("expected IsError for empty query")
	}
}

// --- New Mode Tests ---

func TestKnowledgeTool_BriefingMode(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, store, nil, nil, nil, nil)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{
		"runtime":  "php-nginx@8.4",
		"services": []string{"postgresql@16"},
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}

	text := getTextContent(t, result)
	// Verify briefing contains stack-specific sections (no core — that's scope="infrastructure")
	if strings.Contains(text, "Zerops Core Reference") {
		t.Error("briefing should NOT contain core reference")
	}
	if !strings.Contains(text, "PHP") {
		t.Error("briefing missing PHP runtime delta")
	}
	if !strings.Contains(text, "PostgreSQL") {
		t.Error("briefing missing PostgreSQL card")
	}
}

func TestKnowledgeTool_RecipeMode(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, store, nil, nil, nil, nil)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{"recipe": "ghost"})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}

	text := getTextContent(t, result)
	if !strings.Contains(text, "maxContainers") {
		t.Error("recipe missing expected content")
	}
}

func TestKnowledgeTool_ModeMixError(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, store, nil, nil, nil, nil)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{
		"query":   "test",
		"runtime": "php@8",
	})

	if !result.IsError {
		t.Error("expected error for mixed modes")
	}
	// v8.96: rejection MUST name the specific modes the agent passed,
	// not just say "cannot mix" generically. The agent fixes the call
	// from this list alone — no rule re-derivation needed.
	body := extractText(result)
	if !strings.Contains(body, "MODE 1 query=") || !strings.Contains(body, "MODE 2 runtime=") {
		t.Errorf("v8.96: rejection must name the specific conflicting modes (MODE 1 query= + MODE 2 runtime=); got: %s", body)
	}
}

// TestKnowledgeTool_ModeMixError_AllFourCombination — v8.96. When the
// agent passes ALL FOUR modes, the rejection must list all four in the
// canonical order (MODE 4 → MODE 3 → MODE 2 → MODE 1) so the agent
// learns the taxonomy from the rejection alone.
func TestKnowledgeTool_ModeMixError_AllFourCombination(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, store, nil, nil, nil, nil)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{
		"query":   "anything",
		"runtime": "php@8",
		"scope":   "infrastructure",
		"recipe":  "php-hello-world",
	})
	if !result.IsError {
		t.Fatal("expected error when all four modes are passed together")
	}
	body := extractText(result)
	for _, want := range []string{"MODE 4 recipe=", "MODE 3 scope=", "MODE 2 runtime=", "MODE 1 query="} {
		if !strings.Contains(body, want) {
			t.Errorf("rejection must name %q; got: %s", want, body)
		}
	}
	// The remediation must tell the agent the positive form.
	if !strings.Contains(body, "Re-call with only one of") {
		t.Errorf("rejection must include positive remediation 'Re-call with only one of...': %s", body)
	}
}

// TestKnowledgeTool_FetchMode_ReturnsFullDocumentBody — run-23 F-25.
// A new fetch mode (URI=zerops://...) returns the full Document body
// for an explicit URI rather than a SearchResult list. Used by the
// refinement sub-agent to fetch a specific reference distillation atom
// on demand instead of preloading all 7 into the brief.
func TestKnowledgeTool_FetchMode_ReturnsFullDocumentBody(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, store, nil, nil, nil, nil)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{
		"uri": "zerops://themes/core",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "Universal rules here") {
		t.Errorf("fetch mode should return full body of zerops://themes/core; got: %s", text)
	}
}

// TestKnowledgeTool_FetchMode_UnknownURI_Errors — fetch mode rejects
// an unknown URI with a recovery-class platform error so the agent can
// retry rather than treat the empty body as success.
func TestKnowledgeTool_FetchMode_UnknownURI_Errors(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, store, nil, nil, nil, nil)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{
		"uri": "zerops://themes/nonexistent",
	})
	if !result.IsError {
		t.Fatal("expected error for unknown uri")
	}
}

// TestKnowledgeTool_FetchMode_RejectsCombinedWithOtherModes — like
// other modes, fetch is exclusive.
func TestKnowledgeTool_FetchMode_RejectsCombinedWithOtherModes(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, store, nil, nil, nil, nil)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{
		"uri":   "zerops://themes/core",
		"query": "anything",
	})
	if !result.IsError {
		t.Fatal("expected error when uri + query are combined")
	}
}

func TestKnowledgeTool_NoModeError(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, store, nil, nil, nil, nil)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{})

	if !result.IsError {
		t.Error("expected error for no mode")
	}
	// v8.96: empty-call rejection must restate the decision tree (the
	// four mode names) so the agent picks one without re-fetching the
	// tool description.
	body := extractText(result)
	for _, want := range []string{"recipe", "scope", "runtime", "query"} {
		if !strings.Contains(body, want) {
			t.Errorf("empty-call rejection must mention %q to guide the next call: %s", want, body)
		}
	}
}

// --- Scope Mode Tests ---

func TestKnowledgeTool_ScopeInfrastructure_ReturnsCore(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, store, nil, nil, nil, nil)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{
		"scope": "infrastructure",
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}

	text := getTextContent(t, result)
	if !strings.Contains(text, "Zerops Core Reference") {
		t.Error("scope=infrastructure should return core reference content")
	}
	if !strings.Contains(text, "Universal rules here") {
		t.Error("scope=infrastructure should return full core content")
	}
}

func TestKnowledgeTool_ScopeInfrastructure_PrependsUniversals(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, store, nil, nil, nil, nil)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{
		"scope": "infrastructure",
	})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}

	text := getTextContent(t, result)
	if !strings.Contains(text, "Platform Constraints") {
		t.Error("scope=infrastructure should prepend universals")
	}
	// Universals should appear before core content
	uIdx := strings.Index(text, "Platform Constraints")
	cIdx := strings.Index(text, "Zerops Core Reference")
	if uIdx >= cIdx {
		t.Error("universals should appear before core reference")
	}
}

func TestKnowledgeTool_RecipeMode_PrependsUniversals(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, store, nil, nil, nil, nil)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{"recipe": "ghost"})

	if result.IsError {
		t.Errorf("unexpected error: %s", getTextContent(t, result))
	}

	text := getTextContent(t, result)
	if !strings.Contains(text, "Platform Constraints") {
		t.Error("recipe should be prepended with universals")
	}
	if !strings.Contains(text, "maxContainers") {
		t.Error("recipe should still contain original content")
	}
}

func TestKnowledgeTool_ScopeInvalid_Error(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, store, nil, nil, nil, nil)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{
		"scope": "unknown",
	})

	if !result.IsError {
		t.Error("expected error for unknown scope")
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "Unknown scope") {
		t.Errorf("error should mention unknown scope, got: %s", text)
	}
}

func TestKnowledgeTool_ScopePlusBriefing_Error(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, store, nil, nil, nil, nil)

	result := callTool(t, srv, "zerops_knowledge", map[string]any{
		"scope":   "infrastructure",
		"runtime": "nodejs@22",
	})

	if !result.IsError {
		t.Error("expected error for mixed scope + briefing modes")
	}
}

// --- Scope + Live Stacks Tests ---

func TestKnowledgeTool_ScopeWithLiveStacks(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)

	tests := []struct {
		name          string
		client        platform.Client
		cache         *ops.StackTypeCache
		wantStacks    bool
		wantCore      bool
		wantUniversal bool
	}{
		{
			name:          "with_cache_and_types",
			client:        platform.NewMock().WithServiceStackTypes(testStackTypes()),
			cache:         ops.NewStackTypeCache(time.Hour),
			wantStacks:    true,
			wantCore:      true,
			wantUniversal: true,
		},
		{
			name:          "nil_cache_no_stacks",
			client:        nil,
			cache:         nil,
			wantStacks:    false,
			wantCore:      true,
			wantUniversal: true,
		},
		{
			name:          "nil_client_no_stacks",
			client:        nil,
			cache:         ops.NewStackTypeCache(time.Hour),
			wantStacks:    false,
			wantCore:      true,
			wantUniversal: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterKnowledge(srv, store, tt.client, tt.cache, nil, nil)

			result := callTool(t, srv, "zerops_knowledge", map[string]any{"scope": "infrastructure"})
			if result.IsError {
				t.Fatalf("unexpected error: %s", getTextContent(t, result))
			}
			text := getTextContent(t, result)

			if tt.wantStacks {
				if !strings.Contains(text, "Available Service Stacks (live)") {
					t.Error("scope with cache should include live stacks header")
				}
				if !strings.Contains(text, "nodejs") {
					t.Error("scope with cache should include nodejs in stacks")
				}
				// Stacks should appear before universals/core
				sIdx := strings.Index(text, "Available Service Stacks")
				uIdx := strings.Index(text, "Platform Constraints")
				if sIdx >= uIdx {
					t.Error("stacks should appear before universals")
				}
			} else if strings.Contains(text, "Available Service Stacks (live)") {
				t.Error("scope without cache should NOT include live stacks")
			}
			if tt.wantCore && !strings.Contains(text, "Zerops Core Reference") {
				t.Error("scope should include core reference")
			}
			if tt.wantUniversal && !strings.Contains(text, "Platform Constraints") {
				t.Error("scope should include universals")
			}
		})
	}
}

func testStackTypes() []platform.ServiceStackType {
	return []platform.ServiceStackType{
		{
			Name:     "nodejs",
			Category: "USER",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "nodejs@22", Status: serviceStatusActive},
				{Name: "nodejs@24", Status: serviceStatusActive},
			},
		},
	}
}

// --- No-dedup tests: every call returns full content ---

func TestKnowledgeTool_Scope_CallTwice_BothReturnFull(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	engine := testBootstrapEngine(t)
	RegisterKnowledge(srv, store, nil, nil, nil, engine)

	result1 := callTool(t, srv, "zerops_knowledge", map[string]any{"scope": "infrastructure"})
	if result1.IsError {
		t.Fatalf("first call error: %s", getTextContent(t, result1))
	}
	text1 := getTextContent(t, result1)

	result2 := callTool(t, srv, "zerops_knowledge", map[string]any{"scope": "infrastructure"})
	if result2.IsError {
		t.Fatalf("second call error: %s", getTextContent(t, result2))
	}
	text2 := getTextContent(t, result2)

	// Both calls should return identical full content (no dedup).
	if text1 != text2 {
		t.Error("second scope call should return same full content as first (no dedup)")
	}
	if !strings.Contains(text1, "Platform Constraints") {
		t.Error("scope call should always include universals")
	}
}

func TestKnowledgeTool_Briefing_CallTwice_SameKey_BothReturnFull(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	engine := testBootstrapEngine(t)
	RegisterKnowledge(srv, store, nil, nil, nil, engine)

	args := map[string]any{"runtime": "php-nginx@8.4", "services": []string{"postgresql@16"}}

	result1 := callTool(t, srv, "zerops_knowledge", args)
	if result1.IsError {
		t.Fatalf("first call error: %s", getTextContent(t, result1))
	}
	text1 := getTextContent(t, result1)
	if !strings.Contains(text1, "PHP") {
		t.Error("first briefing should include PHP content")
	}

	result2 := callTool(t, srv, "zerops_knowledge", args)
	if result2.IsError {
		t.Fatalf("second call error: %s", getTextContent(t, result2))
	}
	text2 := getTextContent(t, result2)

	// Both calls should return identical full content (no dedup stub).
	if text1 != text2 {
		t.Error("second briefing call should return same full content as first (no dedup)")
	}
	if strings.Contains(text2, "already loaded") {
		t.Error("no dedup stub should ever be returned")
	}
}

func TestKnowledgeTool_BriefingRuntime_EmptyServices(t *testing.T) {
	t.Parallel()
	store := testKnowledgeStore(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, store, nil, nil, nil, nil)

	tests := []struct {
		name string
		args map[string]any
	}{
		{
			name: "empty_services_array",
			args: map[string]any{
				"runtime":  "nodejs@22",
				"services": []string{},
			},
		},
		{
			name: "omitted_services",
			args: map[string]any{
				"runtime": "nodejs@22",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := callTool(t, srv, "zerops_knowledge", tt.args)
			if result.IsError {
				t.Errorf("should not error with runtime set: %s", getTextContent(t, result))
			}
			text := getTextContent(t, result)
			if !strings.Contains(text, "Node.js") {
				t.Error("briefing should contain Node.js runtime content")
			}
		})
	}
}

// testBootstrapEngine creates a workflow engine with an active bootstrap session.
func testBootstrapEngine(t *testing.T) *workflow.Engine {
	t.Helper()
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	if _, err := engine.BootstrapStart("proj-1", "test intent"); err != nil {
		t.Fatalf("bootstrap start: %v", err)
	}
	return engine
}

func TestResolveKnowledgeMode_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		engine    *workflow.Engine
		inputMode string
		want      topology.Mode
	}{
		{
			name:      "nil_engine_returns_empty",
			engine:    nil,
			inputMode: "",
			want:      "",
		},
		{
			name:      "explicit_override_wins",
			engine:    nil,
			inputMode: "stage",
			want:      "stage",
		},
		{
			name:      "explicit_override_with_engine",
			engine:    testBootstrapEngine(t),
			inputMode: "simple",
			want:      "simple",
		},
		{
			name:      "bootstrap_no_plan_returns_empty",
			engine:    testBootstrapEngine(t),
			inputMode: "",
			want:      "", // discover step — plan not submitted yet, PlanMode() returns ""
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := resolveKnowledgeMode(tt.engine, tt.inputMode)
			if got != tt.want {
				t.Errorf("resolveKnowledgeMode() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestKnowledgeTool_BriefingWithModeOverride(t *testing.T) {
	t.Parallel()
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, store, nil, nil, nil, nil)

	// Without mode: runtime guide content visible, no mode adaptation header.
	result := callTool(t, srv, "zerops_knowledge", map[string]any{
		"runtime": "go@1",
	})
	text := getTextContent(t, result)
	if !strings.Contains(text, "Go") || !strings.Contains(text, "on Zerops") {
		t.Errorf("briefing should contain Go runtime guide, got: %s", text[:min(200, len(text))])
	}

	// With mode=standard: runtime guide visible, mode handled by prependModeAdaptation in recipe path.
	result = callTool(t, srv, "zerops_knowledge", map[string]any{
		"runtime": "go@1",
		"mode":    "standard",
	})
	text = getTextContent(t, result)
	if !strings.Contains(text, "Go") || !strings.Contains(text, "on Zerops") {
		t.Errorf("standard mode briefing should contain Go runtime guide, got: %s", text[:min(200, len(text))])
	}

	// With mode=stage: runtime guide visible.
	result = callTool(t, srv, "zerops_knowledge", map[string]any{
		"runtime": "go@1",
		"mode":    "stage",
	})
	text = getTextContent(t, result)
	if !strings.Contains(text, "Go") || !strings.Contains(text, "on Zerops") {
		t.Errorf("stage mode briefing should contain Go runtime guide, got: %s", text[:min(200, len(text))])
	}
}

func TestKnowledgeTool_RecipeWithModeOverride(t *testing.T) {
	t.Parallel()
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, store, nil, nil, nil, nil)

	// Without mode: no mode adaptation header.
	result := callTool(t, srv, "zerops_knowledge", map[string]any{
		"recipe": "php-hello-world",
	})
	text := getTextContent(t, result)
	if strings.Contains(text, "Mode: dev") {
		t.Error("unfiltered recipe should NOT have mode adaptation header")
	}

	// With mode=standard: concise mode adaptation header pointing to dev setup block.
	result = callTool(t, srv, "zerops_knowledge", map[string]any{
		"recipe": "php-hello-world",
		"mode":   "standard",
	})
	text = getTextContent(t, result)
	if !strings.Contains(text, "Mode: dev") {
		t.Error("standard mode recipe should have mode adaptation header")
	}
	if !strings.Contains(text, "`dev`") {
		t.Error("standard mode recipe should point to dev setup block")
	}
}

func TestKnowledgeTool_QueryCache_DeduplicatesCalls(t *testing.T) {
	t.Parallel()

	store := &countingKnowledgeStore{Store: testKnowledgeStore(t)}
	engine := workflow.NewEngine(t.TempDir(), "", nil)

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterKnowledge(srv, store, nil, nil, nil, engine)

	// First call — hits the store.
	r1 := callTool(t, srv, "zerops_knowledge", map[string]any{"query": "postgresql", "limit": 3})
	if r1.IsError {
		t.Fatalf("first call failed: %s", getTextContent(t, r1))
	}

	// Second identical call — should be cached.
	r2 := callTool(t, srv, "zerops_knowledge", map[string]any{"query": "postgresql", "limit": 3})
	if r2.IsError {
		t.Fatalf("second call failed: %s", getTextContent(t, r2))
	}

	if store.searchCount != 1 {
		t.Errorf("expected store.Search called once (cached), got %d", store.searchCount)
	}

	// Different query — should hit the store again.
	r3 := callTool(t, srv, "zerops_knowledge", map[string]any{"query": "valkey", "limit": 3})
	if r3.IsError {
		t.Fatalf("third call failed: %s", getTextContent(t, r3))
	}

	if store.searchCount != 2 {
		t.Errorf("expected store.Search called twice (different query), got %d", store.searchCount)
	}
}

// countingKnowledgeStore wraps a knowledge.Store and counts Search calls.
type countingKnowledgeStore struct {
	*knowledge.Store
	searchCount int
}

func (c *countingKnowledgeStore) Search(query string, limit int) []knowledge.SearchResult {
	c.searchCount++
	return c.Store.Search(query, limit)
}
