package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// adoptActivityGate refuses an adopt that targets a service with a LIVE
// build/deploy/lifecycle process — adopting mid-first-deploy is premature (the
// service reads READY_TO_DEPLOY the whole time). It sits ahead of BOTH adopt
// dispatch branches (empty-plan+scope auto-derive AND explicit plan=[...]), so a
// busy target is caught whichever path the agent took; it resolves the target
// hostname set from scope ∪ plan. Returns the refusal error, or nil to proceed.
//
// Best-effort + loop-safe by construction:
//   - an activity-fetch error never blocks adoption (returns nil) — the gate is
//     a guard, not a hard requirement, and recovery must not deadlock on an API
//     hiccup.
//   - each busy verdict is FRESHENED via GetProcess (authoritative + current)
//     before refusing, so a stale search row (a FINISHED process still indexed
//     as RUNNING) cannot deadlock the gate.
//   - a FAILED/terminal target is never busy (IsProcessLive is false), so a
//     post-failure adopt + corrective deploy are never gated.
func adoptActivityGate(ctx context.Context, client platform.Client, projectID string, input WorkflowInput, existing []platform.ServiceStack) *platform.PlatformError {
	targets := adoptTargetHostnames(input)
	if len(targets) == 0 {
		return nil
	}
	idToHost := make(map[string]string, len(existing))
	for _, s := range existing {
		idToHost[s.ID] = s.Name
	}
	activity, err := ops.ProjectActivity(ctx, client, projectID, idToHost)
	if err != nil {
		//nolint:nilerr // intentional fail-open: the gate is a best-effort guard, never block adoption (recovery) on an activity-fetch hiccup
		return nil
	}

	var busyHosts []string
	busy := map[string][]ops.LiveOp{}
	for _, h := range targets {
		if _, already := busy[h]; already {
			continue
		}
		live := activity[h]
		if len(live) == 0 {
			continue
		}
		// Freshen each op: keep only those STILL live right now, so a stale search
		// row (a FINISHED process still indexed as RUNNING) can't deadlock the gate.
		var stillLive []ops.LiveOp
		for _, op := range live {
			if processStillLive(ctx, client, op.ProcessID) {
				stillLive = append(stillLive, op)
			}
		}
		if len(stillLive) == 0 {
			continue
		}
		busy[h] = stillLive
		busyHosts = append(busyHosts, h)
	}
	if len(busyHosts) == 0 {
		return nil
	}
	return busyAdoptRefusal(busyHosts, busy)
}

// adoptTargetHostnames is the resolved target set the adopt will write meta for:
// the scope hostnames plus every dev+stage hostname named in an explicit plan.
// Covers both dispatch branches so neither path can slip a busy target past the
// gate.
func adoptTargetHostnames(input WorkflowInput) []string {
	out := make([]string, 0, len(input.Scope)+len(input.Plan)*2)
	out = append(out, input.Scope...)
	for _, t := range input.Plan {
		if t.Runtime.DevHostname != "" {
			out = append(out, t.Runtime.DevHostname)
		}
		if t.Runtime.ExplicitStage != "" {
			out = append(out, t.Runtime.ExplicitStage)
		}
	}
	return out
}

// processStillLive confirms via GetProcess (authoritative, current) that the
// named process is non-terminal right now. A not-found / errored / terminal
// process returns false — i.e. the gate opens (fail-open), defeating both a
// stale search row and a transient hiccup. ops.IsProcessLive is the single owner
// of the live-status set (shared with ProjectActivity).
func processStillLive(ctx context.Context, client platform.Client, processID string) bool {
	if processID == "" {
		return false
	}
	p, err := client.GetProcess(ctx, processID)
	if err != nil {
		return false
	}
	return ops.IsProcessLive(p.Status)
}

// busyAdoptRefusal builds the ADOPT_TARGET_BUSY refusal: it names each busy
// target's FULL live-op list (action/status/processId per op), steers the agent
// to block on the wait action until the service settles then re-run adopt, and
// points at the cancelable processId as the stuck-process escape (the loop-safety
// recovery pointer).
func busyAdoptRefusal(hostnames []string, activity map[string][]ops.LiveOp) *platform.PlatformError {
	parts := make([]string, 0, len(hostnames))
	for _, h := range hostnames {
		live := activity[h]
		ops := make([]string, 0, len(live))
		for _, op := range live {
			ops = append(ops, fmt.Sprintf("%s %s processId=%s", op.Action, op.Status, op.ProcessID))
		}
		parts = append(parts, fmt.Sprintf("%s [%s]", h, strings.Join(ops, "; ")))
	}
	msg := fmt.Sprintf("Cannot adopt — a live operation is in progress on: %s. Adopting a service mid-build/deploy is premature (a first deploy reads a resting status like \"READY_TO_DEPLOY\" or \"NEW\" the whole time it runs).", strings.Join(parts, "; "))
	suggestion := "Block until the service is done with `zerops_process action=\"wait\" service=<hostname>` (drains build, deploy, and any queued op), then re-run this adopt. A genuinely stuck process can be canceled with `zerops_process processId=<id> action=\"cancel\"`."
	return platform.NewPlatformError(platform.ErrAdoptTargetBusy, msg, suggestion)
}
