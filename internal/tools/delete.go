package tools

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// DeleteInput is the input type for zerops_delete.
type DeleteInput struct {
	ServiceHostname string `json:"serviceHostname" jsonschema:"Hostname of the service to delete."`
}

// deleteResponse wraps the delete process with a poll-timeout signal so a
// timed-out delete is not read as confirmed.
type deleteResponse struct {
	Process  *platform.Process `json:"process,omitempty"`
	TimedOut bool              `json:"timedOut,omitempty"`
	Warning  string            `json:"warning,omitempty"`
	// Recipe names the call that proposes a deleted part's removal from the
	// group recipe's tiers (hq_recipe_parts.go).
	Recipe string `json:"recipe,omitempty"`
}

// RegisterDelete registers the zerops_delete tool.
// stateDir is the workflow state directory; empty string disables service meta cleanup.
// mounter, when non-nil, enables best-effort SSHFS unmount of the deleted service.
func RegisterDelete(srv *mcp.Server, client platform.Client, projectID string, stateDir string, mounter ops.Mounter, rtInfo runtime.Info) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "zerops_delete",
		Description: "Delete a service. This is destructive and permanent. " +
			"IMPORTANT: You MUST have explicit user approval in the current conversation to delete " +
			"THIS SPECIFIC service by name. Never delete proactively — only when the user explicitly asks.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Delete a service",
			DestructiveHint: boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input DeleteInput) (*mcp.CallToolResult, any, error) {
		// Guard: prevent self-deletion when running inside a Zerops container.
		if rtInfo.InContainer && strings.EqualFold(input.ServiceHostname, rtInfo.ServiceName) {
			return convertError(platform.NewPlatformError(
				platform.ErrSelfServiceBlocked,
				fmt.Sprintf("Cannot delete %q — ZCP is running on this service", input.ServiceHostname),
				"Delete this service manually via zcli or Zerops GUI.",
			), WithRecoveryStatus()), nil, nil
		}

		// Best-effort: unmount SSHFS before delete so the service still exists for clean unmount.
		if mounter != nil {
			if res, umErr := ops.UnmountService(ctx, client, projectID, mounter, input.ServiceHostname); umErr != nil {
				fmt.Fprintf(os.Stderr, "zcp: pre-delete unmount %s: %v\n", input.ServiceHostname, umErr)
			} else {
				fmt.Fprintf(os.Stderr, "zcp: pre-delete unmount %s: %s\n", input.ServiceHostname, res.Status)
			}
		}

		proc, err := ops.Delete(ctx, client, projectID, input.ServiceHostname)
		if err != nil {
			return convertError(err, WithRecoveryStatus()), nil, nil
		}
		onProgress := buildProgressCallback(ctx, req)
		finalProc, timedOut := pollManageProcess(ctx, client, proc, onProgress)

		// Best-effort: clean up service meta after successful delete+poll.
		var recipe string
		if stateDir != "" && finalProc.Status == statusFinished {
			recipe = forgetStageOfSeveral(stateDir, input.ServiceHostname)
			if delErr := workflow.DeleteServiceMeta(stateDir, input.ServiceHostname); delErr != nil {
				fmt.Fprintf(os.Stderr, "zcp: delete service meta %s: %v\n", input.ServiceHostname, delErr)
			}
		}

		// Surface a poll timeout so the agent doesn't read a still-running
		// delete as confirmed (the ServiceMeta cleanup above is also skipped
		// on timeout, so the local state intentionally still references it).
		resp := deleteResponse{Process: finalProc, TimedOut: timedOut, Recipe: recipe}
		if timedOut {
			resp.Warning = "delete did not confirm within the poll window — verify with zerops_discover; the service may still be deleting."
		}
		return jsonResult(resp), nil, nil
	})
}

// forgetStageOfSeveral takes a deleted stage out of its pair when the pair
// has others: the pair stays, one part fewer (spec-mate §2.8a.4). It returns
// the sentence naming the call that proposes the part's removal from the
// group recipe's tiers when the pair builds from HQ, "" otherwise. A pair's
// only stage is left as it always was.
func forgetStageOfSeveral(stateDir, hostname string) string {
	meta, err := workflow.FindServiceMeta(stateDir, hostname)
	if err != nil || meta == nil || meta.Hostname == hostname || len(meta.Stages) < 2 {
		return ""
	}
	if err := workflow.UpdateServiceMeta(stateDir, meta.Hostname, func(m *workflow.ServiceMeta) error {
		m.Stages = slices.DeleteFunc(m.Stages, func(s topology.StageRef) bool { return s.Hostname == hostname })
		return nil
	}); err != nil {
		fmt.Fprintf(os.Stderr, "zcp: forget stage %s of %s: %v\n", hostname, meta.Hostname, err)
		return ""
	}
	if !hqPairWired(meta) {
		return ""
	}
	return fmt.Sprintf("%s was a part of the pair %q, and the group recipe's tiers on main may still carry it: call zerops_workflow action=\"group-recipe\" service=%q to propose its removal, as a change the person reviews.",
		hostname, meta.Hostname, hostname)
}
