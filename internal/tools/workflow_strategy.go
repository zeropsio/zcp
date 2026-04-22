package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// validStrategies is the set of allowed strategy values.
var validStrategies = map[string]bool{
	workflow.StrategyPushDev: true,
	workflow.StrategyPushGit: true,
	workflow.StrategyManual:  true,
}

// strategyListEntry is one row in the listing-mode response: current strategy
// + the options the agent can switch to.
type strategyListEntry struct {
	Hostname string                  `json:"hostname"`
	Current  workflow.DeployStrategy `json:"current"`
	Options  []string                `json:"options"`
	Hint     string                  `json:"hint"`
}

type strategyListResponse struct {
	Status   string              `json:"status"`
	Services []strategyListEntry `json:"services"`
	Next     string              `json:"next"`
}

// handleStrategy is the central configuration point for service deploy
// strategy. Three modes:
//
//   - Listing: empty strategies map → returns current strategy + options per
//     service. No mutation.
//   - Simple update: strategies={X:push-dev|manual} → write meta, return
//     confirmation. No setup needed.
//   - Setup: strategies={X:push-git} → write meta AND synthesize the
//     push-git setup atom (Option A/B, token, optional CI/CD, verify).
func handleStrategy(input WorkflowInput, stateDir string, rt runtime.Info) (*mcp.CallToolResult, any, error) {
	if len(input.Strategies) == 0 {
		return handleStrategyList(stateDir)
	}

	for hostname, strategy := range input.Strategies {
		if !validStrategies[strategy] {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("Invalid strategy %q for %q", strategy, hostname),
				"Valid strategies: push-dev, push-git, manual")), nil, nil
		}
	}

	// push-git + trigger input must match — reject unknown trigger values
	// up front. Empty trigger is legitimate: the intro atom will surface
	// the choice to the user, who re-calls with an explicit trigger.
	if input.Trigger != "" && input.Trigger != string(workflow.TriggerWebhook) && input.Trigger != string(workflow.TriggerActions) {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Invalid trigger %q", input.Trigger),
			"Valid triggers (push-git only): 'webhook' or 'actions'")), nil, nil
	}
	if input.Trigger != "" && !anyStrategyIs(input.Strategies, workflow.StrategyPushGit) {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"trigger is only valid when setting strategy=push-git",
			"Drop the trigger param, or set strategies={hostname:\"push-git\"}")), nil, nil
	}

	// Only complete (bootstrapped) metas are valid strategy targets — auto-
	// creating orphan metas here poisons every downstream consumer (router,
	// briefing, locks).
	updated := make([]string, 0, len(input.Strategies))
	for hostname, strategy := range input.Strategies {
		meta, err := workflow.ReadServiceMeta(stateDir, hostname)
		if err != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrServiceNotFound,
				fmt.Sprintf("Read service meta %q: %v", hostname, err),
				"Ensure the service was bootstrapped first")), nil, nil
		}
		if meta == nil || !meta.IsComplete() {
			return convertError(platform.NewPlatformError(
				platform.ErrServiceNotFound,
				fmt.Sprintf("Service %q is not bootstrapped", hostname),
				"Run bootstrap first: zerops_workflow action=\"start\" workflow=\"bootstrap\"")), nil, nil
		}
		// Gate local-only + push-dev: no stage target.
		if strategy == workflow.StrategyPushDev && meta.Mode == workflow.PlanModeLocalOnly {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("Service %q is local-only — push-dev needs a Zerops stage to deploy to", hostname),
				fmt.Sprintf("Link a stage first: zerops_workflow action=\"adopt-local\" targetService=<runtime-hostname>. Or pick push-git / manual, which work without a stage.")),
			), nil, nil
		}
		updated = append(updated, fmt.Sprintf("%s=%s", hostname, strategy))
		// Detect no-op: same strategy AND same trigger (if applicable) AND
		// already-confirmed state.
		sameStrategy := meta.DeployStrategy == strategy && meta.StrategyConfirmed
		sameTrigger := strategy != workflow.StrategyPushGit || meta.PushGitTrigger == input.Trigger
		if sameStrategy && sameTrigger {
			continue
		}
		meta.DeployStrategy = strategy
		meta.StrategyConfirmed = true
		if strategy == workflow.StrategyPushGit {
			// Only write trigger when one was provided; empty leaves the
			// previous value (which might be "" on fresh push-git setup
			// → intro atom handles the ask).
			if input.Trigger != "" {
				meta.PushGitTrigger = input.Trigger
			}
		} else {
			// Non-push-git strategies can't carry a trigger — clear.
			meta.PushGitTrigger = ""
		}
		if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrServiceNotFound,
				fmt.Sprintf("Write service meta %q: %v", hostname, err),
				"")), nil, nil
		}
	}

	// push-git needs the full setup atom chain (intro → push → trigger).
	// Service-scoped atoms (strategies, triggers) need a snapshot in the
	// envelope to match against; we synthesize one minimally from the just-
	// written meta(s). Other strategies reuse the existing develop-phase
	// iteration atoms.
	var guidance string
	if anyStrategyIs(input.Strategies, workflow.StrategyPushGit) {
		env := workflow.DetectEnvironment(rt)
		envelope := workflow.StateEnvelope{
			Phase:       workflow.PhaseStrategySetup,
			Environment: env,
			Services:    buildStrategySetupSnapshots(stateDir, input.Strategies, input.Trigger),
		}
		g, err := workflow.SynthesizeImmediateWorkflow(envelope)
		if err == nil {
			guidance = g
		}
	} else {
		guidance = buildStrategyGuidance(input.Strategies)
	}

	nextHint := `When code is ready: zerops_workflow action="start" workflow="develop"`
	if allStrategiesAre(input.Strategies, workflow.StrategyManual) {
		nextHint = `When code is ready: zerops_deploy targetService="..." (manual strategy — deploy directly)`
	} else if allStrategiesAre(input.Strategies, workflow.StrategyPushGit) {
		nextHint = `Follow the setup guidance below. Push code with: zerops_deploy targetService="..." strategy="git-push"`
	}

	result := map[string]string{
		"status":   "updated",
		"services": strings.Join(updated, ", "),
		"next":     nextHint,
	}
	if guidance != "" {
		result["guidance"] = guidance
	}
	return jsonResult(result), nil, nil
}

// handleStrategyList returns current deploy strategy per bootstrapped service
// plus the set of options the agent can switch to. Pure read — no mutation.
func handleStrategyList(stateDir string) (*mcp.CallToolResult, any, error) {
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("List service metas: %v", err),
			"")), nil, nil
	}
	options := []string{workflow.StrategyPushDev, workflow.StrategyPushGit, workflow.StrategyManual}

	entries := make([]strategyListEntry, 0, len(metas))
	for _, m := range metas {
		if !m.IsComplete() {
			continue
		}
		current := workflow.DeployStrategy(m.DeployStrategy)
		if current == "" {
			current = workflow.StrategyUnset
		}
		entries = append(entries, strategyListEntry{
			Hostname: m.Hostname,
			Current:  current,
			Options:  options,
			Hint:     fmt.Sprintf(`zerops_workflow action="strategy" strategies={%q:%q}`, m.Hostname, workflow.StrategyPushDev),
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Hostname < entries[j].Hostname })

	resp := strategyListResponse{
		Status:   "list",
		Services: entries,
		Next:     `Pick a strategy per service: zerops_workflow action="strategy" strategies={"hostname":"push-dev|push-git|manual"}. For push-git, the response includes the full setup flow (tokens + optional CI/CD).`,
	}
	return jsonResult(resp), nil, nil
}

// buildStrategyGuidance returns strategy-specific guidance synthesised from
// the Layer 2 atom corpus. Used for non-push-git strategies (push-dev,
// manual) which just need iteration pointers, not setup.
func buildStrategyGuidance(strategies map[string]string) string {
	g, _ := workflow.BuildStrategyGuidance(strategies)
	return g
}

// buildStrategySetupSnapshots constructs minimal ServiceSnapshots for every
// service being configured in this handleStrategy call, so push-git setup
// atoms — which filter on strategies/triggers/mode — can match. The
// snapshots reflect POST-write state: Strategy is what the caller asked
// for, Trigger is what the caller passed (possibly empty → intro atom).
// Mode comes from the freshly-read meta so the env-specific push atoms
// (container vs local) dispatch correctly.
func buildStrategySetupSnapshots(stateDir string, strategies map[string]string, trigger string) []workflow.ServiceSnapshot {
	out := make([]workflow.ServiceSnapshot, 0, len(strategies))
	for hostname, strategy := range strategies {
		snap := workflow.ServiceSnapshot{
			Hostname:     hostname,
			Bootstrapped: true,
			Strategy:     workflow.DeployStrategy(strategy),
		}
		if meta, _ := workflow.ReadServiceMeta(stateDir, hostname); meta != nil {
			snap.Mode = workflow.Mode(meta.Mode)
			if meta.StageHostname != "" {
				snap.StageHostname = meta.StageHostname
			}
		}
		// Trigger axis only carries meaning on push-git — for push-dev/manual
		// we leave it unset so trigger-filtered atoms don't accidentally match.
		if strategy == workflow.StrategyPushGit {
			if trigger == "" {
				snap.Trigger = workflow.TriggerUnset
			} else {
				snap.Trigger = workflow.PushGitTrigger(trigger)
			}
		}
		out = append(out, snap)
	}
	return out
}

// anyStrategyIs returns true if at least one value in the map equals strategy.
func anyStrategyIs(strategies map[string]string, strategy string) bool {
	for _, s := range strategies {
		if s == strategy {
			return true
		}
	}
	return false
}

// allStrategiesAre returns true if all values in the map equal strategy. Empty
// map returns false (no strategies to match).
func allStrategiesAre(strategies map[string]string, strategy string) bool {
	if len(strategies) == 0 {
		return false
	}
	for _, s := range strategies {
		if s != strategy {
			return false
		}
	}
	return true
}

// handleRoute gathers router input from live API + local state and returns flow offerings.
func handleRoute(ctx context.Context, _ *workflow.Engine, client platform.Client, projectID, stateDir, selfHostname string) (*mcp.CallToolResult, any, error) {
	var liveHostnames []string
	var unmanagedRuntimes []string
	liveStatus := make(map[string]string)

	metas, _ := workflow.ListServiceMetas(stateDir)
	metaMap := make(map[string]*workflow.ServiceMeta, len(metas))
	for _, m := range metas {
		metaMap[m.Hostname] = m
	}
	stageOf := make(map[string]bool)
	for _, m := range metas {
		if m.IsComplete() && m.StageHostname != "" {
			stageOf[m.StageHostname] = true
		}
	}

	if client != nil && projectID != "" {
		if svcs, err := client.ListServices(ctx, projectID); err == nil {
			for _, s := range svcs {
				if s.IsSystem() || (selfHostname != "" && s.Name == selfHostname) {
					continue
				}
				liveHostnames = append(liveHostnames, s.Name)
				liveStatus[s.Name] = s.Status
				typeName := s.ServiceStackTypeInfo.ServiceStackTypeVersionName
				if !workflow.IsManagedService(typeName) && !stageOf[s.Name] {
					if m, ok := metaMap[s.Name]; !ok || !m.IsComplete() {
						unmanagedRuntimes = append(unmanagedRuntimes, s.Name)
					}
				}
			}
		}
	}

	sessions, _ := workflow.ListSessions(stateDir)
	ws, _ := workflow.CurrentWorkSession(stateDir)
	return jsonResult(workflow.Route(workflow.RouterInput{
		ServiceMetas:      metas,
		ActiveSessions:    sessions,
		LiveServices:      liveHostnames,
		LiveServiceStatus: liveStatus,
		UnmanagedRuntimes: unmanagedRuntimes,
		WorkSession:       ws,
	})), nil, nil
}
