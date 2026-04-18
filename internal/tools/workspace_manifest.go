package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/workflow"
)

// WorkspaceManifestInput is the input for zerops_workspace_manifest. v8.94
// §5.8 — the main agent maintains a structured workspace snapshot so
// subsequent subagents (feature, code-review, content-authoring) skip the
// 30-file orientation crawl.
//
// Two actions:
//   - "read"   — returns the current manifest JSON, or an empty skeleton when
//     not yet initialized.
//   - "update" — merges the UpdatePayload into the on-disk manifest. Codebases
//     entries overwrite per-hostname; FeaturesImplemented appends; Contracts
//     replaces whole.
//
// Subagents read but do not write — manifest authorship is main-agent-only
// per the v8.90 "workflow state is main-agent-only" policy. Subagents return
// structured data in their completion message; the main agent calls
// action=update on receipt.
type WorkspaceManifestInput struct {
	Action        string         `json:"action"                  jsonschema:"required,One of: read, update. 'read' returns the current manifest (or an empty skeleton) as JSON. 'update' merges the UpdatePayload into the on-disk manifest."`
	UpdatePayload map[string]any `json:"updatePayload,omitempty" jsonschema:"When action=update, the partial manifest to merge. Fields: planSlug (string), codebases (map of hostname → CodebaseInfo — entries overwrite per-hostname, nil deletes), contracts (replaces whole when non-nil), featuresImplemented (array — appended to existing), notes (map — keys overwrite). See ops.WorkspaceManifestUpdate for the full schema."`
}

// RegisterWorkspaceManifest registers the zerops_workspace_manifest MCP tool.
func RegisterWorkspaceManifest(srv *mcp.Server, engine *workflow.Engine) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_workspace_manifest",
		Description: "Read or update the recipe workspace manifest — a structured JSON snapshot of scaffold state, source-file purposes, managed-service wiring, pre-flight results, cross-codebase contracts, and features implemented. Subagents read this instead of crawling the filesystem. Main agent updates it after each subagent return. Action=read returns the current manifest (or an empty skeleton if uninitialized). Action=update merges the UpdatePayload — Codebases overwrite per-hostname, FeaturesImplemented appends, Contracts replaces whole.",
		Annotations: &mcp.ToolAnnotations{
			Title:          "Workspace manifest (read/update)",
			ReadOnlyHint:   false,
			IdempotentHint: false,
		},
	}, func(_ context.Context, _ *mcp.CallToolRequest, input WorkspaceManifestInput) (*mcp.CallToolResult, any, error) {
		if engine == nil {
			return textResult("Error: workflow engine not initialized"), nil, nil
		}
		sessionID := engine.SessionID()
		if sessionID == "" {
			return textResult("Error: no active workflow session — zerops_workspace_manifest is only meaningful during an active recipe session"), nil, nil
		}
		path := ops.WorkspaceManifestPath(sessionID)

		switch input.Action {
		case "read":
			m, err := ops.ReadWorkspaceManifest(path, sessionID)
			if err != nil {
				return textResult(fmt.Sprintf("Error reading manifest: %v", err)), nil, nil
			}
			data, err := json.MarshalIndent(m, "", "  ")
			if err != nil {
				return textResult(fmt.Sprintf("Error marshaling manifest: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil

		case "update":
			if len(input.UpdatePayload) == 0 {
				return textResult("Error: action=update requires a non-empty updatePayload"), nil, nil
			}
			// Round-trip through JSON so the untyped map is decoded into the
			// strongly-typed WorkspaceManifestUpdate schema — this keeps the
			// MCP contract permissive (agent passes a plain object) while
			// the ops layer stays strictly typed.
			payloadJSON, err := json.Marshal(input.UpdatePayload)
			if err != nil {
				return textResult(fmt.Sprintf("Error re-encoding updatePayload: %v", err)), nil, nil
			}
			var patch ops.WorkspaceManifestUpdate
			if err := json.Unmarshal(payloadJSON, &patch); err != nil {
				return textResult(fmt.Sprintf("Error parsing updatePayload: %v", err)), nil, nil
			}
			m, err := ops.ApplyWorkspaceManifestUpdate(path, sessionID, patch)
			if err != nil {
				return textResult(fmt.Sprintf("Error applying manifest update: %v", err)), nil, nil
			}
			data, err := json.MarshalIndent(m, "", "  ")
			if err != nil {
				return textResult(fmt.Sprintf("Error marshaling updated manifest: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil

		default:
			return textResult(fmt.Sprintf("Error: unknown action %q (expected 'read' or 'update')", input.Action)), nil, nil
		}
	})
}
