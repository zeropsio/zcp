package tools

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// blockedManualHosts returns the in-scope hostnames whose meta has
// CloseDeployMode ∈ {manual, unset, ""} — services that auto-close cannot
// fire on (deploy-decomp P6 §3.4 Scenario D). Empty result when every
// service has an auto-close-eligible mode (auto / git-push) or when meta
// lookup fails (legacy adopted-without-meta keeps the old auto-delete
// behavior). Hosts are returned in scope order to keep the agent's
// remediation message stable.
func blockedManualHosts(stateDir string, scope []string) []string {
	if stateDir == "" || len(scope) == 0 {
		return nil
	}
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil {
		return nil
	}
	idx := workflow.ManagedRuntimeIndex(metas)
	var blocked []string
	for _, h := range scope {
		m := idx[h]
		if m == nil {
			continue
		}
		if m.CloseDeployMode != topology.CloseModeAuto && m.CloseDeployMode != topology.CloseModeGitPush {
			blocked = append(blocked, h)
		}
	}
	return blocked
}

// handleDevelopBriefing returns the develop briefing and creates/resumes a
// per-PID WorkSession that records deploy/verify lifecycle for the task.
//
// The WorkSession survives context compaction via the system-prompt
// "Lifecycle Status" block, so the LLM never forgets what was deployed and
// what still needs verification — even across summarization boundaries.
//
// Scope is the explicit set of runtime hostnames this task works on —
// committed at start, stable through the task. Auto-close fires when every
// hostname in scope has a succeeded deploy and a passed verify. Services
// newly bootstrapped mid-task do NOT auto-join scope; the agent closes +
// restarts develop with the expanded scope, or treats them as out-of-band.
//
// New intent on an already-open session auto-closes the prior session —
// 1 task = 1 intent = 1 session. Same intent is idempotent (returns the
// current briefing without mutating state).
//
// Guidance is synthesized via the Layer 2 atom pipeline (ComputeEnvelope →
// Synthesize → BuildPlan → RenderStatus): runtime, strategy, mode and
// environment axes of the envelope drive which atoms match.
func handleDevelopBriefing(ctx context.Context, engine *workflow.Engine, client platform.Client, projectID string, input WorkflowInput, rt runtime.Info) (*mcp.CallToolResult, any, error) {
	metas, err := workflow.ListServiceMetas(engine.StateDir())
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Failed to read service metas: %v", err),
			"Run bootstrap first to create services"), WithRecoveryStatus()), nil, nil
	}

	// Prune stale metas against live services — keeps envelope coherent if
	// someone deleted a service in the Zerops UI while ZCP state lingered.
	if client != nil {
		services, listErr := ops.ListProjectServices(ctx, client, projectID)
		if listErr == nil {
			live := make(map[string]bool, len(services))
			for _, svc := range services {
				live[svc.Name] = true
			}
			workflow.PruneServiceMetas(engine.StateDir(), live)

			metas, err = workflow.ListServiceMetas(engine.StateDir())
			if err != nil {
				return convertError(platform.NewPlatformError(
					platform.ErrInvalidParameter,
					fmt.Sprintf("Failed to read service metas after pruning: %v", err),
					""), WithRecoveryStatus()), nil, nil
			}
		}
	}

	if len(metas) == 0 {
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			"No bootstrapped services found",
			"Run bootstrap first: action=\"start\" workflow=\"bootstrap\" (route=\"adopt\" if services already live)"), WithRecoveryStatus()), nil, nil
	}

	// Build deployable-runtime meta index for scope validation, honoring the
	// pair-keyed invariant (spec-workflows.md §8 E8): both halves of a
	// container+standard pair resolve to the single meta file. Without this,
	// scope=[devhost, stagehost] was silently rejecting stage despite the
	// atom telling the agent to include it.
	allRuntimes := workflow.ManagedRuntimeIndex(metas)
	runtimeMetas := make(map[string]*workflow.ServiceMeta, len(allRuntimes))
	for h, m := range allRuntimes {
		if !m.IsComplete() {
			continue
		}
		if m.Mode == "" && m.StageHostname == "" {
			continue
		}
		runtimeMetas[h] = m
	}
	if len(runtimeMetas) == 0 {
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			"No deployable runtime services found",
			"Run bootstrap first: action=\"start\" workflow=\"bootstrap\""), WithRecoveryStatus()), nil, nil
	}

	// Close-mode is NOT a gate: first deploy always uses the default
	// self-deploy mechanism regardless of meta.CloseDeployMode. The close-mode
	// decision surfaces post-first-deploy via the develop-strategy-review
	// atom (phases=develop-active, deployStates=[deployed],
	// closeDeployModes=[unset]).
	existing, _ := workflow.CurrentWorkSession(engine.StateDir())
	if existing != nil && existing.ClosedAt == "" {
		// Same intent — idempotent restart, return briefing without mutating
		// session state. Scope on this call is treated as confirmation, not
		// a mutation; a scope change requires an explicit close first.
		if existing.Intent != "" && existing.Intent == input.Intent {
			return renderDevelopBriefing(ctx, engine, client, projectID, rt)
		}
		// Different intent — manual/unset close-mode session blocks the
		// implicit auto-delete (deploy-decomp P6 §3.4 Scenario D + §6 step 5).
		// Auto-close cannot fire on such sessions, so silently discarding
		// would erase the user's in-flight work without a clear close.
		// Require an explicit close OR force=true to discard.
		if blocked := blockedManualHosts(engine.StateDir(), existing.Services); len(blocked) > 0 && !input.Force.Bool() {
			return jsonResult(map[string]any{
				"status":   "manualSessionActive",
				"intent":   existing.Intent,
				"services": existing.Services,
				"blocked":  blocked,
				"reason":   fmt.Sprintf("Active develop session has services with manual/unset close-mode: %s. Auto-close cannot fire on these.", strings.Join(blocked, ", ")),
				"options": []string{
					"Close the active session explicitly: zerops_workflow action=\"close\"",
					"Discard the active session and start fresh: re-call this start with force=true",
				},
			}), nil, nil
		}
		// Auto-close-eligible session OR force=true — auto-delete and create
		// fresh. "1 task = 1 session" invariant. Data loss is limited to
		// in-session attempt history; git + platform hold the durable record.
		_ = workflow.DeleteWorkSession(engine.StateDir(), os.Getpid())
		_ = workflow.UnregisterSession(engine.StateDir(), workflow.WorkSessionID(os.Getpid()))
	}

	// Scope is a required explicit input at start. No derivation from metas,
	// no "latest bootstrap targets", no fallback. Agent names the services
	// this task works on — the invariant CLAUDE.md promises: "auto-closes
	// once the services you're working on are deployed and verified."
	scope, err := validateDevelopScope(input.Scope, runtimeMetas)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			err.Error(),
			"Pass scope=[\"hostname1\",\"hostname2\"] listing the runtime services this task works on. Copy hostnames from the bootstrap close transition message, or call zerops_discover to list what's available."), WithRecoveryStatus()), nil, nil
	}

	ws := workflow.NewWorkSession(projectID, string(engine.Environment()), input.Intent, scope)
	if err := workflow.SaveWorkSession(engine.StateDir(), ws); err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Failed to save work session: %v", err),
			""), WithRecoveryStatus()), nil, nil
	}
	_ = workflow.RegisterSession(engine.StateDir(), workflow.SessionEntry{
		SessionID: workflow.WorkSessionID(os.Getpid()),
		PID:       os.Getpid(),
		Workflow:  workflow.WorkflowWork,
		ProjectID: projectID,
		Intent:    input.Intent,
	})

	return renderDevelopBriefing(ctx, engine, client, projectID, rt)
}

// renderDevelopBriefing runs the atom pipeline and returns the rendered status
// block. Extracted so the idempotent-restart path in handleDevelopBriefing can
// skip session mutation but still return fresh guidance.
func renderDevelopBriefing(ctx context.Context, engine *workflow.Engine, client platform.Client, projectID string, rt runtime.Info) (*mcp.CallToolResult, any, error) {
	envelope, err := workflow.ComputeEnvelope(ctx, client, engine.StateDir(), projectID, rt, time.Now())
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrNotImplemented,
			fmt.Sprintf("Compute envelope: %v", err),
			""), WithRecoveryStatus()), nil, nil
	}
	corpus, err := workflow.LoadAtomCorpus()
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrNotImplemented,
			fmt.Sprintf("Load knowledge atoms: %v", err),
			""), WithRecoveryStatus()), nil, nil
	}
	matches, err := workflow.Synthesize(envelope, corpus)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrNotImplemented,
			fmt.Sprintf("Synthesize guidance: %v", err),
			""), WithRecoveryStatus()), nil, nil
	}
	plan := workflow.BuildPlan(envelope)
	return textResult(workflow.RenderStatus(workflow.Response{
		Envelope: envelope,
		Guidance: workflow.BodiesOf(matches),
		Plan:     &plan,
	})), nil, nil
}

// validateDevelopScope checks the agent-supplied scope against runtime metas.
// Returns the ordered, deduplicated scope on success. Rejects empty scope,
// unknown hostnames, and hostnames that resolve to managed services (which
// have no mode/stage and cannot be deploy targets).
//
// The returned slice is sorted by hostname for deterministic work session
// serialization — envelope and status output depend on stable ordering.
func validateDevelopScope(requested []string, runtimeMetas map[string]*workflow.ServiceMeta) ([]string, error) {
	available := sortedHostnames(runtimeMetas)
	if len(requested) == 0 {
		return nil, fmt.Errorf("scope is required — name the runtime service hostnames this task works on. Available: %v", available)
	}
	seen := make(map[string]bool, len(requested))
	scope := make([]string, 0, len(requested))
	var unknown []string
	for _, h := range requested {
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		if _, ok := runtimeMetas[h]; !ok {
			unknown = append(unknown, h)
			continue
		}
		scope = append(scope, h)
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("scope contains unknown or non-deployable hostnames %v — available runtime services: %v", unknown, available)
	}
	if len(scope) == 0 {
		return nil, fmt.Errorf("scope is empty after deduplication — name at least one runtime service")
	}
	sort.Strings(scope)
	return scope, nil
}

func sortedHostnames(metas map[string]*workflow.ServiceMeta) []string {
	out := make([]string, 0, len(metas))
	for h := range metas {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}
