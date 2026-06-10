package tools

import (
	"context"
	"errors"
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
		if m.CloseDeployMode != topology.CloseModeAuto {
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
	// liveHostnames is also passed to validateDevelopScope below to catch
	// the half-pair case (PruneServiceMetas keeps a pair meta as long as
	// either half is alive, so the disk-side StageHostname can outlive the
	// stage service itself; auto-expanding scope to a dead hostname would
	// permanently block auto-close).
	var liveHostnames map[string]bool
	if client != nil {
		services, listErr := ops.ListProjectServices(ctx, client, projectID)
		if listErr == nil {
			liveHostnames = make(map[string]bool, len(services))
			for _, svc := range services {
				liveHostnames[svc.Name] = true
			}
			workflow.PruneServiceMetas(engine.StateDir(), liveHostnames)

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
		// H2 defense-in-depth: structured Recovery points at the exact
		// next call. The router (build_plan.go:382-388) already telegraphs
		// bootstrap+route=adopt for unmanaged-runtimes scenarios; this
		// rejection only fires when the agent ignored the router and
		// jumped to develop directly. Code is ErrAdoptRequired (the
		// semantic-specific narrow form of PrerequisiteMissing) so the
		// Recovery shape is self-evident from the wire code. Pinned by
		// TestErrAdoptRequiredCarriesAdoptRecovery.
		return convertError(platform.NewPlatformError(
			platform.ErrAdoptRequired,
			"No bootstrapped services found",
			"Run bootstrap first: action=\"start\" workflow=\"bootstrap\" (route=\"adopt\" if services already live)"),
			WithRecovery(&RecoveryHint{
				Tool:   "zerops_workflow",
				Action: "start",
				Args:   map[string]string{"workflow": "bootstrap", "route": "adopt"},
			})), nil, nil
	}

	// Build deployable-runtime meta index for scope validation, honoring the
	// pair-keyed invariant (spec-workflows.md §8 E8): both halves of a
	// container+standard pair resolve to the single meta file. Without this,
	// scope=[devhost, stagehost] was silently rejecting stage despite the
	// atom telling the agent to include it.
	//
	// Local-mode metas are project-keyed (m.Hostname = project.Name set by
	// LocalAutoAdopt), not service-keyed. Two consequences for runtimeMetas:
	//   - PlanModeLocalOnly metas have no deployable runtime — surfacing
	//     them as scope candidates sent agents chasing the project name as
	//     if it were a service.
	//   - PlanModeLocalStage metas index under both project name (m.Hostname)
	//     and stage hostname (m.StageHostname) via ManagedRuntimeIndex; only
	//     the stage hostname is a deployable scope target. The project-name
	//     key must be filtered out.
	allRuntimes := workflow.ManagedRuntimeIndex(metas)
	runtimeMetas := make(map[string]*workflow.ServiceMeta, len(allRuntimes))
	hasLocalOnly := false
	for h, m := range allRuntimes {
		if m.Mode == topology.PlanModeLocalOnly {
			hasLocalOnly = true
			continue
		}
		if m.Mode == topology.PlanModeLocalStage && h == m.Hostname {
			continue // skip the project-name key; the stage-hostname key remains
		}
		if !m.IsComplete() {
			continue
		}
		if m.Mode == "" && m.StageHostname == "" {
			continue
		}
		runtimeMetas[h] = m
	}
	if len(runtimeMetas) == 0 {
		if hasLocalOnly {
			// Local-only project: services may exist on Zerops but none is
			// linked as the local stage. The recovery is adopt-local
			// (subaction transitions local-only → local-stage by linking
			// one runtime), NOT bootstrap+adopt (which is for unmanaged
			// services or new projects).
			return convertError(platform.NewPlatformError(
				platform.ErrPrerequisiteMissing,
				"Project is adopted as local-only — link a Zerops runtime as the local stage before develop",
				"Run: zerops_workflow action=\"adopt-local\" targetService=\"<runtime-hostname>\". Pick the runtime you want this local checkout to deploy to."),
				WithRecovery(&RecoveryHint{
					Tool:   "zerops_workflow",
					Action: "adopt-local",
				})), nil, nil
		}
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
	// IsOpen, not a raw ClosedAt read: a DERIVED auto-complete session keeps
	// ClosedAt=="" on disk, so a raw read would re-enter the same-intent branch
	// and return the closed briefing forever (stuck on close+start-next). An
	// auto-completed session is NOT open — it falls straight through to a fresh
	// start below.
	if workflow.IsOpen(engine.StateDir(), existing) {
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
	}
	// Replace any stale session before opening the next: an OPEN session we were
	// cleared to discard (different intent, auto-eligible or force=true), OR a
	// DERIVED auto-complete session (done — IsOpen was false above). Delete +
	// unregister so the fresh RegisterSession below leaves no duplicate registry
	// entry. "1 task = 1 session" invariant; data loss is limited to in-session
	// attempt history — git + platform hold the durable record.
	if existing != nil {
		_ = workflow.DeleteWorkSession(engine.StateDir(), os.Getpid())
		_ = workflow.UnregisterSession(engine.StateDir(), workflow.WorkSessionID(os.Getpid()))
	}

	// Scope is a required explicit input at start. No derivation from metas,
	// no "latest bootstrap targets", no fallback. Agent names the services
	// this task works on — the invariant CLAUDE.md promises: "auto-closes
	// once the services you're working on are deployed and verified."
	scope, err := validateDevelopScope(input.Scope, runtimeMetas, liveHostnames)
	if err != nil {
		if errors.Is(err, errStandardPairStageMissing) {
			// Disk meta points at a stage hostname that's no longer in the
			// live service list. Same H2-class fix as the no-bootstrapped
			// site above — code is ErrAdoptRequired so Recovery shape is
			// self-evident, and Recovery itself is specific (re-bootstrap
			// with route=adopt repairs the pair meta). The prose hint
			// retains the alternative path (delete dev meta + re-bootstrap
			// with mode=dev/simple) for the case where the stage half was
			// intentionally removed.
			return convertError(platform.NewPlatformError(
				platform.ErrAdoptRequired,
				err.Error(),
				"Re-bootstrap to refresh the pair meta: zerops_workflow action=\"start\" workflow=\"bootstrap\" route=\"adopt\". If the stage half was intentionally removed, delete the dev meta and re-bootstrap with mode=dev or mode=simple."),
				WithRecovery(&RecoveryHint{
					Tool:   "zerops_workflow",
					Action: "start",
					Args:   map[string]string{"workflow": "bootstrap", "route": "adopt"},
				})), nil, nil
		}
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			err.Error(),
			"Pass scope=[\"hostname1\",\"hostname2\"] listing the runtime services this task works on. Copy hostnames from the bootstrap close transition message, or call zerops_discover to list what's available."), WithRecoveryStatus()), nil, nil
	}

	if errResult := startDevelopSession(engine, projectID, input, scope); errResult != nil {
		return errResult, nil, nil
	}

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

// errStandardPairStageMissing is returned by validateDevelopScope when a
// standard pair's meta names a stage hostname that's not present in the
// live service list. PruneServiceMetas keeps a pair meta as long as
// either half is alive, so the disk-side StageHostname can outlive the
// stage service itself. Detected via errors.Is by handleDevelopBriefing
// to switch the convertError code/suggestion to a repair guide.
var errStandardPairStageMissing = errors.New("standard pair stage half is not a live service")

// startDevelopSession builds, persists, and registers the work session for a
// fresh develop start. Returns a non-nil error CallToolResult when role
// validation or the save fails; nil on success. Extracted from
// handleDevelopBriefing to keep that handler's maintainability index within
// budget after RC-B added role wiring.
func startDevelopSession(engine *workflow.Engine, projectID string, input WorkflowInput, scope []string) *mcp.CallToolResult {
	// RC-B: per-session roles. outOfScope hostnames must be declared in the
	// (widened) scope and must not empty the required set — otherwise the
	// session could never auto-complete and the agent would silently stall.
	roles, err := developRoles(scope, input.OutOfScope)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			err.Error(),
			"Pass outOfScope hostnames that are part of this develop scope (e.g. the standard-pair stage half), and keep at least one service required."), WithRecoveryStatus())
	}

	ws := workflow.NewWorkSession(projectID, string(engine.Environment()), input.Intent, scope)
	ws.Roles = roles
	if err := workflow.SaveWorkSession(engine.StateDir(), ws); err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Failed to save work session: %v", err),
			""), WithRecoveryStatus())
	}
	_ = workflow.RegisterSession(engine.StateDir(), workflow.SessionEntry{
		SessionID: workflow.WorkSessionID(os.Getpid()),
		PID:       os.Getpid(),
		StartTime: workflow.CurrentProcessStartTime(),
		Workflow:  workflow.WorkflowWork,
		ProjectID: projectID,
		Intent:    input.Intent,
	})
	return nil
}

// developRoles builds the per-session role map (RC-B) from the validated
// scope and the agent-supplied outOfScope list. Returns nil (all required)
// when outOfScope is empty. Each outOfScope hostname MUST be in scope, and at
// least one service must remain required — a session with zero required
// services can never auto-complete, so we reject that up front rather than
// let the agent stall silently.
func developRoles(scope, outOfScope []string) (map[string]string, error) {
	if len(outOfScope) == 0 {
		return map[string]string{}, nil // no non-required roles; all scope services required
	}
	inScope := make(map[string]bool, len(scope))
	for _, h := range scope {
		inScope[h] = true
	}
	roles := make(map[string]string, len(outOfScope))
	for _, h := range outOfScope {
		if h == "" {
			continue
		}
		if !inScope[h] {
			return nil, fmt.Errorf("outOfScope hostname %q is not in develop scope %v — only declared scope services can be marked out-of-scope", h, scope)
		}
		roles[h] = workflow.RoleOutOfScope
	}
	required := 0
	for _, h := range scope {
		if roles[h] == "" {
			required++
		}
	}
	if required == 0 {
		return nil, fmt.Errorf("outOfScope %v would leave no required service — at least one scope service must remain required for the session to complete", outOfScope)
	}
	return roles, nil
}

// validateDevelopScope checks the agent-supplied scope against runtime metas.
// Returns the ordered, deduplicated scope on success. Rejects empty scope,
// unknown hostnames, and hostnames that resolve to managed services (which
// have no mode/stage and cannot be deploy targets).
//
// Standard-mode dev halves auto-include their paired stage hostname so the
// auto-close gate counts both halves of the pair and develop-active atoms
// can fire on the (standard, deployed, unset) triple — without this the
// stage half is invisible to session progress and the agent stops at the
// dev URL with the stage artifact stale (real-session evidence in two
// adopted-pair workflows that ended at the dev preview without promoting).
// Local pair modes are pair-keyed differently and not expanded here.
//
// liveHostnames (when non-nil) is consulted before adding a stage hostname
// to scope — if the meta names a stage that's not in the live set, the
// pair is broken and validateDevelopScope returns errStandardPairStageMissing
// rather than silently widening scope to a hostname deploy/verify can't
// reach. liveHostnames==nil signals the platform listing wasn't available
// (degraded mode); the live-aware check is skipped and the meta is trusted.
//
// The returned slice is sorted by hostname for deterministic work session
// serialization — envelope and status output depend on stable ordering.
func validateDevelopScope(requested []string, runtimeMetas map[string]*workflow.ServiceMeta, liveHostnames map[string]bool) ([]string, error) {
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
	// Auto-include the paired stage hostname for any standard-mode dev
	// half present in scope. runtimeMetas is pair-keyed (ManagedRuntimeIndex)
	// so both halves resolve to the same meta; we only expand when the
	// scoped hostname IS the dev half (m.Hostname == h), never when the
	// agent already named the stage half directly.
	for _, h := range append([]string(nil), scope...) {
		m := runtimeMetas[h]
		if m == nil || m.Mode != topology.PlanModeStandard {
			continue
		}
		stage := m.StageHostname
		if stage == "" || h != m.Hostname {
			continue
		}
		if liveHostnames != nil && !liveHostnames[stage] {
			return nil, fmt.Errorf("%w: dev half %q meta names stage %q, but %q is not in the project's live service list", errStandardPairStageMissing, h, stage, stage)
		}
		if seen[stage] {
			continue
		}
		seen[stage] = true
		scope = append(scope, stage)
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
