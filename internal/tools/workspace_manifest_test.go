package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
)

func TestWorkspaceManifest_ReadReturnsSkeletonFirstCall(t *testing.T) {
	t.Parallel()
	engine := testEngine(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkspaceManifest(srv, engine)

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
	RegisterWorkspaceManifest(srv, engine)

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
	RegisterWorkspaceManifest(srv, engine)

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
	RegisterWorkspaceManifest(srv, engine)

	result := callTool(t, srv, "zerops_workspace_manifest", map[string]any{
		"action": "delete",
	})
	text := getTextContent(t, result)
	if !strings.Contains(text, "unknown action") {
		t.Errorf("expected unknown-action error, got: %s", text)
	}
}

func TestWorkspaceManifest_RequiresActiveSession(t *testing.T) {
	t.Parallel()
	engine := testEngine(t)
	if err := engine.Reset(); err != nil {
		t.Fatalf("reset: %v", err)
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkspaceManifest(srv, engine)

	result := callTool(t, srv, "zerops_workspace_manifest", map[string]any{
		"action": "read",
	})
	text := getTextContent(t, result)
	if !strings.Contains(strings.ToLower(text), "session") {
		t.Errorf("expected session error, got: %s", text)
	}
}
