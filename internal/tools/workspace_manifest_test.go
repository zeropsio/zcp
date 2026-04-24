package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/recipe"
)

func TestWorkspaceManifest_ReadReturnsSkeletonFirstCall(t *testing.T) {
	t.Parallel()
	engine := testEngine(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkspaceManifest(srv, engine, nil)

	result := callTool(t, srv, "zerops_workspace_manifest", map[string]any{
		"action": "read",
	})
	if result.IsError {
		t.Fatalf("read returned error: %s", getTextContent(t, result))
	}
	body := getTextContent(t, result)
	var m ops.WorkspaceManifest
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("response is not valid JSON manifest: %v; body=%s", err, body)
	}
	if m.SessionID != engine.SessionID() {
		t.Errorf("SessionID = %q, want engine session %q", m.SessionID, engine.SessionID())
	}
	// LastUpdated must be populated by the skeleton generator — proves the
	// response isn't an unmarshal artifact of a zero-value struct.
	if m.LastUpdated == "" {
		t.Error("skeleton should carry populated LastUpdated")
	}
}

func TestWorkspaceManifest_UpdateThenRead(t *testing.T) {
	t.Parallel()
	engine := testEngine(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkspaceManifest(srv, engine, nil)

	patch := map[string]any{
		"planSlug": "nestjs-showcase",
		"codebases": map[string]any{
			"apidev": map[string]any{
				"framework": "NestJS 11",
				"runtime":   "nodejs@22",
				"zeropsYaml": map[string]any{
					"setups":   []string{"dev", "prod"},
					"httpPort": 3000,
				},
			},
		},
		"featuresImplemented": []map[string]any{
			{"id": "items-crud", "touches": []string{"apidev/src/items"}},
		},
	}

	upd := callTool(t, srv, "zerops_workspace_manifest", map[string]any{
		"action":        "update",
		"updatePayload": patch,
	})
	if upd.IsError {
		t.Fatalf("update returned error: %s", getTextContent(t, upd))
	}

	// Read back via the tool.
	read := callTool(t, srv, "zerops_workspace_manifest", map[string]any{
		"action": "read",
	})
	if read.IsError {
		t.Fatalf("read returned error: %s", getTextContent(t, read))
	}
	var m ops.WorkspaceManifest
	if err := json.Unmarshal([]byte(getTextContent(t, read)), &m); err != nil {
		t.Fatalf("read response not JSON: %v", err)
	}
	if m.PlanSlug != "nestjs-showcase" {
		t.Errorf("PlanSlug = %q, want nestjs-showcase", m.PlanSlug)
	}
	api, ok := m.Codebases["apidev"]
	if !ok {
		t.Fatal("apidev missing from manifest after update")
	}
	if api.Framework != "NestJS 11" {
		t.Errorf("apidev framework = %q", api.Framework)
	}
	if api.ZeropsYAML == nil || api.ZeropsYAML.HTTPPort != 3000 {
		t.Errorf("apidev zerops.yaml not persisted: %+v", api.ZeropsYAML)
	}
	if len(m.FeaturesImplemented) != 1 || m.FeaturesImplemented[0].ID != "items-crud" {
		t.Errorf("features not persisted: %+v", m.FeaturesImplemented)
	}
}

func TestWorkspaceManifest_UpdateWithoutPayloadFails(t *testing.T) {
	t.Parallel()
	engine := testEngine(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkspaceManifest(srv, engine, nil)

	result := callTool(t, srv, "zerops_workspace_manifest", map[string]any{
		"action": "update",
	})
	text := getTextContent(t, result)
	if !strings.Contains(text, "updatePayload") {
		t.Errorf("expected updatePayload-required error, got: %s", text)
	}
}

func TestWorkspaceManifest_UnknownActionFails(t *testing.T) {
	t.Parallel()
	engine := testEngine(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkspaceManifest(srv, engine, nil)

	result := callTool(t, srv, "zerops_workspace_manifest", map[string]any{
		"action": "delete",
	})
	text := getTextContent(t, result)
	if !strings.Contains(text, "unknown action") {
		t.Errorf("expected unknown-action error, got: %s", text)
	}
}

// TestWorkspaceManifest_RoutesToRecipeSession — when the v2 engine has no
// session but a recipe session is open, manifest read/update must target the
// recipe's outputRoot, not the v2 /tmp path. Exercises Workstream E deferred
// plumbing: recipe sub-agents call zerops_workspace_manifest and land in the
// right place.
func TestWorkspaceManifest_RoutesToRecipeSession(t *testing.T) {
	t.Parallel()
	engine := testEngine(t)
	if err := engine.Reset(); err != nil {
		t.Fatalf("reset: %v", err)
	}

	store := recipe.NewStore(t.TempDir())
	outputRoot := filepath.Join(t.TempDir(), "recipe-run")
	if _, err := store.OpenOrCreate("alpha-showcase", outputRoot); err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkspaceManifest(srv, engine, store)

	// Update routes into the recipe's manifest path.
	upd := callTool(t, srv, "zerops_workspace_manifest", map[string]any{
		"action": "update",
		"updatePayload": map[string]any{
			"planSlug": "alpha-showcase",
		},
	})
	if upd.IsError {
		t.Fatalf("update returned error: %s", getTextContent(t, upd))
	}

	// The manifest file must land at <outputRoot>/workspace-manifest.json.
	manifestPath := filepath.Join(outputRoot, "workspace-manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read recipe manifest at %q: %v", manifestPath, err)
	}
	var m ops.WorkspaceManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if m.PlanSlug != "alpha-showcase" {
		t.Errorf("PlanSlug = %q, want alpha-showcase", m.PlanSlug)
	}

	// Read returns the same manifest.
	read := callTool(t, srv, "zerops_workspace_manifest", map[string]any{"action": "read"})
	if read.IsError {
		t.Fatalf("read returned error: %s", getTextContent(t, read))
	}
	var got ops.WorkspaceManifest
	if err := json.Unmarshal([]byte(getTextContent(t, read)), &got); err != nil {
		t.Fatalf("parse read response: %v", err)
	}
	if got.PlanSlug != "alpha-showcase" {
		t.Errorf("read PlanSlug = %q, want alpha-showcase", got.PlanSlug)
	}
	// SessionID reflects the recipe slug — not the v2 engine sessionID —
	// because the manifest is scoped to this recipe run.
	if got.SessionID != "alpha-showcase" {
		t.Errorf("SessionID = %q, want alpha-showcase", got.SessionID)
	}
}

// TestWorkspaceManifest_AmbiguousMultipleSessionsErrors — two open sessions
// make "which one?" unanswerable; the tool must error rather than picking.
func TestWorkspaceManifest_AmbiguousMultipleSessionsErrors(t *testing.T) {
	t.Parallel()
	engine := testEngine(t)
	if err := engine.Reset(); err != nil {
		t.Fatalf("reset: %v", err)
	}

	dir := t.TempDir()
	store := recipe.NewStore(dir)
	if _, err := store.OpenOrCreate("alpha", filepath.Join(dir, "a")); err != nil {
		t.Fatalf("alpha: %v", err)
	}
	if _, err := store.OpenOrCreate("beta", filepath.Join(dir, "b")); err != nil {
		t.Fatalf("beta: %v", err)
	}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkspaceManifest(srv, engine, store)

	result := callTool(t, srv, "zerops_workspace_manifest", map[string]any{"action": "read"})
	text := getTextContent(t, result)
	if !strings.Contains(strings.ToLower(text), "session") {
		t.Errorf("expected session-ambiguity error, got: %s", text)
	}
}

func TestWorkspaceManifest_RequiresActiveSession(t *testing.T) {
	t.Parallel()
	engine := testEngine(t)
	if err := engine.Reset(); err != nil {
		t.Fatalf("reset: %v", err)
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkspaceManifest(srv, engine, nil)

	result := callTool(t, srv, "zerops_workspace_manifest", map[string]any{
		"action": "read",
	})
	text := getTextContent(t, result)
	if !strings.Contains(strings.ToLower(text), "session") {
		t.Errorf("expected session error, got: %s", text)
	}
}
