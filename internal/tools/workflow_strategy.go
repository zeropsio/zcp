package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/content"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// validStrategies is the set of allowed strategy values.
var validStrategies = map[string]bool{
	workflow.StrategyPushDev: true,
	workflow.StrategyPushGit: true,
	workflow.StrategyManual:  true,
}

// handleStrategy handles post-bootstrap strategy updates for individual services.
func handleStrategy(_ *workflow.Engine, input WorkflowInput, stateDir string) (*mcp.CallToolResult, any, error) {
	if len(input.Strategies) == 0 {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Strategies map is required for strategy action",
			"Provide strategies: {\"hostname\": \"push-dev|push-git|manual\"}")), nil, nil
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
	var updated []string
	for hostname, strategy := range input.Strategies {
		meta, err := workflow.ReadServiceMeta(stateDir, hostname)
		if err != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrServiceNotFound,
				fmt.Sprintf("Read service meta %q: %v", hostname, err),
				"Ensure the service was bootstrapped first")), nil, nil
		}
		if meta == nil {
			meta = &workflow.ServiceMeta{
				Hostname: hostname,
			}
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

	// Build guidance from deploy.md sections.
	guidance := buildStrategyGuidance(input.Strategies)

	// Build strategy-aware next step hint.
	nextHint := `When code is ready: zerops_workflow action="start" workflow="develop"`
	if allStrategiesAre(input.Strategies, workflow.StrategyManual) {
		nextHint = `When code is ready: zerops_deploy targetService="..." (manual strategy — deploy directly)`
	} else if allStrategiesAre(input.Strategies, workflow.StrategyPushGit) {
		nextHint = `Push to git: zerops_workflow action="start" workflow="develop". CI/CD setup: zerops_workflow action="start" workflow="cicd"`
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

// buildStrategyGuidance extracts strategy-specific guidance from deploy.md.
func buildStrategyGuidance(strategies map[string]string) string {
	md, err := content.GetWorkflow("develop")
	if err != nil {
		return ""
	}

	seen := make(map[string]bool)
	var parts []string
	for _, strategy := range strategies {
		section, ok := workflow.StrategyToSection[strategy]
		if !ok || seen[section] {
			continue
		}
		seen[section] = true
		extracted := workflow.ExtractSection(md, section)
		if extracted != "" {
			parts = append(parts, extracted)
		}
	}
	return strings.Join(parts, "\n\n---\n\n")
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
