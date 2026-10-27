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
	ServiceHostname string `json:"serviceHostname"   jsonschema:"Hostname of the service to delete."`
	Consent         string `json:"consent,omitempty" jsonschema:"serviceHostname again, only once the person agreed to delete it by name."`
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
			"THIS SPECIFIC service by name. Never delete proactively — only when the user explicitly asks. " +
			"Without consent naming the service, the call is refused.",
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

		// A dev service the pool migration is retiring is deleted there, by
		// the id the retire recorded and after its bundle: a delete here
		// would drop its pair's meta mid-move (docs/spec-workflows.md §13
		// PM-7).
		// A variant-B pair's further dev services and its code volume go
		// with it, the volume last.
		rec, _ := readPoolRetireRecord(stateDir, input.ServiceHostname)
		if rec == nil || !poolRetireInFlight(rec.Phase) {
			rec = poolRetireRecordNaming(stateDir, input.ServiceHostname)
		}
		if rec != nil && poolRetireInFlight(rec.Phase) {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidUsage,
				fmt.Sprintf("%q is being retired by the pool migration of %s (phase %s): its delete runs there, by the id the retire read, after the bundle of its repository", input.ServiceHostname, rec.Dev, rec.Phase),
				fmt.Sprintf(`Call zerops_workflow workflow="pool-migrate" action="retire" service=%q to resume it.`, rec.Dev),
			), WithRecoveryStatus()), nil, nil
		}

		// zcp never deletes a service without the person's explicit consent
		// to deleting that service by name.
		if input.Consent != input.ServiceHostname {
			return convertError(platform.NewPlatformError(
				platform.ErrConsentRequired,
				fmt.Sprintf("Deleting %q is permanent and needs the person's explicit consent to deleting it by name", input.ServiceHostname),
				fmt.Sprintf("Ask the person whether to delete %q. Only after they agree, call zerops_delete serviceHostname=%q consent=%q.", input.ServiceHostname, input.ServiceHostname, input.ServiceHostname),
			)), nil, nil
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
			if delErr := forgetHQPairState(stateDir, input.ServiceHostname); delErr != nil {
				fmt.Fprintf(os.Stderr, "zcp: %v\n", delErr)
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
	return fmt.Sprintf("%s was a part of the pair %q, and the group recipe's tiers on main may still carry it: call zerops_workflow action=\"group-recipe\" service=%q to propose its removal as the Mate's recipe change.",
		hostname, meta.Hostname, hostname)
}
