package tools

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// stackSteps are the steps where the stack catalog is useful.
var stackSteps = map[string]bool{
	workflow.StepDiscover: true,
	workflow.StepGenerate: true,
}

// needsStacks returns true if stacks should be populated for the response.
func needsStacks(resp *workflow.BootstrapResponse) bool {
	if resp == nil || resp.Current == nil {
		return true // inactive bootstrap or completed — include for safety
	}
	return stackSteps[resp.Current.Name]
}

func handleBootstrapComplete(ctx context.Context, engine *workflow.Engine, client platform.Client, cache *ops.StackTypeCache, input WorkflowInput, liveTypes []platform.ServiceStackType, logFetcher platform.LogFetcher, projectID string, stateDir string, mounter ops.Mounter) (*mcp.CallToolResult, any, error) {
	if input.Step == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Step is required for complete action",
			"Specify step name (e.g., step=\"discover\")")), nil, nil
	}

	// Structured plan routing for "discover" step (empty plan = managed-only).
	if input.Step == "discover" && input.Plan != nil {
		resp, err := engine.BootstrapCompletePlan(input.Plan, liveTypes, nil)
		if err != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("Plan validation failed: %v", err),
				"Provide valid plan: [{runtime: {devHostname, type}, dependencies: [{hostname, type, resolution}]}]. Hostnames: lowercase a-z0-9, max 25 chars.")), nil, nil
		}
		if needsStacks(resp) {
			populateStacks(ctx, resp, client, cache)
		}
		return jsonResult(resp), nil, nil
	}

	// Default: free-text attestation.
	if input.Attestation == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Attestation is required for complete action",
			"Describe what was accomplished in this step")), nil, nil
	}

	httpClient := &http.Client{
		Timeout:   15 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}},
	}
	checker := buildStepChecker(input.Step, client, logFetcher, projectID, httpClient, engine, stateDir)

	resp, err := engine.BootstrapComplete(ctx, input.Step, input.Attestation, checker)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrBootstrapNotActive,
			fmt.Sprintf("Complete step failed: %v", err),
			"Start bootstrap first with action=start workflow=bootstrap")), nil, nil
	}

	// Auto-mount runtime services after successful provision completion.
	// mounter is nil in local env — no-op naturally.
	if input.Step == workflow.StepProvision && (resp.CheckResult == nil || resp.CheckResult.Passed) {
		resp.AutoMounts = autoMountTargets(ctx, client, projectID, mounter, engine)
		cleanupImportYAML(stateDir, resp.AutoMounts)
	}

	// Append transition message when bootstrap completes (all steps done).
	if resp.Current == nil {
		state, stateErr := engine.GetState()
		if stateErr == nil {
			resp.Message = workflow.BuildTransitionMessage(state)
		}
	}
	if needsStacks(resp) {
		populateStacks(ctx, resp, client, cache)
	}
	return jsonResult(resp), nil, nil
}

func handleBootstrapSkip(ctx context.Context, engine *workflow.Engine, client platform.Client, cache *ops.StackTypeCache, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	if input.Step == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Step is required for skip action",
			"Specify step name (e.g., step=\"generate\")")), nil, nil
	}

	reason := input.Reason
	if reason == "" {
		reason = defaultSkipReason
	}

	resp, err := engine.BootstrapSkip(input.Step, reason)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrBootstrapNotActive,
			fmt.Sprintf("Skip step failed: %v", err),
			"Only skippable steps (generate, deploy, close) can be skipped")), nil, nil
	}
	if needsStacks(resp) {
		populateStacks(ctx, resp, client, cache)
	}
	return jsonResult(resp), nil, nil
}

func handleBootstrapStatus(ctx context.Context, engine *workflow.Engine, client platform.Client, cache *ops.StackTypeCache) (*mcp.CallToolResult, any, error) {
	return bootstrapStatusResult(ctx, engine, client, cache)
}

// bootstrapStatusResult returns the current bootstrap status as a BootstrapResponse.
// Shared by handleBootstrapStatus, handleResume, and handleIterate.
func bootstrapStatusResult(ctx context.Context, engine *workflow.Engine, client platform.Client, cache *ops.StackTypeCache) (*mcp.CallToolResult, any, error) {
	resp, err := engine.BootstrapStatus()
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrBootstrapNotActive,
			fmt.Sprintf("Bootstrap status failed: %v", err),
			"")), nil, nil
	}
	if needsStacks(resp) {
		populateStacks(ctx, resp, client, cache)
	}
	return jsonResult(resp), nil, nil
}

// autoMountTargets mounts runtime services from the bootstrap plan after provision.
// Best-effort: mount failures are reported but don't block step advancement.
// Returns nil when mounter is nil (local env) or no plan targets exist.
func autoMountTargets(ctx context.Context, client platform.Client, projectID string, mounter ops.Mounter, engine *workflow.Engine) []workflow.AutoMountInfo {
	if mounter == nil {
		return nil
	}
	state, err := engine.GetState()
	if err != nil || state.Bootstrap == nil || state.Bootstrap.Plan == nil {
		return nil
	}

	var results []workflow.AutoMountInfo
	for _, target := range state.Bootstrap.Plan.Targets {
		hostname := target.Runtime.DevHostname
		if hostname == "" {
			continue
		}
		result, mountErr := ops.MountService(ctx, client, projectID, mounter, hostname)
		if mountErr != nil {
			results = append(results, workflow.AutoMountInfo{
				Hostname: hostname,
				Status:   "FAILED",
				Error:    mountErr.Error(),
			})
			continue
		}
		results = append(results, workflow.AutoMountInfo{
			Hostname:  hostname,
			MountPath: result.MountPath,
			Status:    result.Status,
		})
	}
	return results
}

// populateStacks injects live stack catalog into a bootstrap response.
func populateStacks(ctx context.Context, resp *workflow.BootstrapResponse, client platform.Client, cache *ops.StackTypeCache) {
	if resp == nil || client == nil || cache == nil {
		return
	}
	if types := cache.Get(ctx, client); len(types) > 0 {
		resp.AvailableStacks = knowledge.FormatStackList(types)
	}
}

// injectStacks inserts the stack list section into workflow content.
// Replaces content between STACKS markers if present, otherwise inserts before "## Part 1".
func injectStacks(content, stackList string) string {
	const beginMarker = "<!-- STACKS:BEGIN -->"
	const endMarker = "<!-- STACKS:END -->"

	if beginIdx := strings.Index(content, beginMarker); beginIdx >= 0 {
		if endIdx := strings.Index(content, endMarker); endIdx > beginIdx {
			return content[:beginIdx] + stackList + content[endIdx+len(endMarker):]
		}
	}

	const anchor = "## Part 1"
	if idx := strings.Index(content, anchor); idx > 0 {
		return content[:idx] + stackList + "\n---\n\n" + content[idx:]
	}

	return content
}
