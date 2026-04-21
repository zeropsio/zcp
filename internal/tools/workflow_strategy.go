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
		updated = append(updated, fmt.Sprintf("%s=%s", hostname, strategy))
		if meta.DeployStrategy == strategy && meta.StrategyConfirmed {
			continue
		}
		meta.DeployStrategy = strategy
		meta.StrategyConfirmed = true
		if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrServiceNotFound,
				fmt.Sprintf("Write service meta %q: %v", hostname, err),
				"")), nil, nil
		}
	}

	// push-git needs the full setup atom (tokens, optional CI/CD). Other
	// strategies reuse the existing develop-phase iteration atoms.
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
