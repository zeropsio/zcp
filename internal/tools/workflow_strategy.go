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

// strategyListEntry describes one service's current strategy and options for
// the listing mode returned when `action=strategy` is called with no
// strategies map. Listing mode is the "central point" for deploy config:
// the agent sees current strategy per service and the available options.
type strategyListEntry struct {
	Hostname string   `json:"hostname"`
	Current  string   `json:"current"`           // "push-dev" | "push-git" | "manual" | "unset"
	Options  []string `json:"options"`           // always [push-dev, push-git, manual]
	Hint     string   `json:"hint"`              // example invocation to change it
	StageOf  string   `json:"stageOf,omitempty"` // dev hostname if this is a stage pair half
}

type strategyListResponse struct {
	Status   string              `json:"status"` // "list"
	Services []strategyListEntry `json:"services"`
	Next     string              `json:"next"`
}

// handleStrategy is the central configuration point for service deploy
// strategy. Three modes:
//
//   - Listing: empty strategies map → returns current strategy + options per
//     service. No mutation. Discovery mode.
//   - Simple update: strategies={X:push-dev|manual} → write meta, return
//     short confirmation. No setup needed (those strategies are self-
//     contained).
//   - Setup: strategies={X:push-git} → write meta AND synthesize the
//     push-git setup atom (Option A/B, token, optional CI/CD, verify).
//     One call gives the agent the whole setup flow.
func handleStrategy(_ *workflow.Engine, input WorkflowInput, stateDir string, rt runtime.Info) (*mcp.CallToolResult, any, error) {
	// Listing mode: no strategies map provided → show current state + options.
	// This is how the agent asks "what can be set and what is set?" without
	// changing anything.
	if len(input.Strategies) == 0 {
		return handleStrategyList(stateDir)
	}

	// Validate all strategy values first.
	for hostname, strategy := range input.Strategies {
		if !validStrategies[strategy] {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("Invalid strategy %q for %q", strategy, hostname),
				"Valid strategies: push-dev, push-git, manual")), nil, nil
		}
	}

	// Update each service meta.
	// Only complete (bootstrapped) metas are valid strategy targets — auto-creating
	// orphan metas here poisons every downstream consumer (router, briefing, locks).
	var updated []string
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
		meta.DeployStrategy = strategy
		meta.StrategyConfirmed = true
		if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrServiceNotFound,
				fmt.Sprintf("Write service meta %q: %v", hostname, err),
				"")), nil, nil
		}
		updated = append(updated, fmt.Sprintf("%s=%s", hostname, strategy))
	}

	// For push-git: synthesize the full setup atom (tokens, optional CI/CD,
	// first push). This replaces the old split between `action=strategy`
	// (flag-only) + `workflow=cicd` (setup) into one action — the central
	// deploy-config entry point. Other strategies (push-dev, manual) need
	// no further setup; they get the short develop-phase guidance.
	var guidance string
	if anyStrategyIs(input.Strategies, workflow.StrategyPushGit) {
		g, err := workflow.SynthesizeImmediateWorkflow(workflow.PhaseStrategySetup, workflow.DetectEnvironment(rt))
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

	var entries []strategyListEntry
	for _, m := range metas {
		if !m.IsComplete() {
			continue
		}
		current := m.DeployStrategy
		if current == "" {
			current = string(workflow.StrategyUnset)
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

// anyStrategyIs returns true if at least one value in the map equals the
// given strategy.
func anyStrategyIs(strategies map[string]string, strategy string) bool {
	for _, s := range strategies {
		if s == strategy {
			return true
		}
	}
	return false
}

// allStrategiesAre returns true if all values in the map equal the given strategy.
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
	return jsonResult(workflow.Route(workflow.RouterInput{
		ServiceMetas:      metas,
		ActiveSessions:    sessions,
		LiveServices:      liveHostnames,
		UnmanagedRuntimes: unmanagedRuntimes,
	})), nil, nil
}
