package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// EnvInput is the input type for zerops_env.
type EnvInput struct {
	Action          string   `json:"action"                    jsonschema:"Action: set, delete, or generate-dotenv. generate-dotenv reads zerops.yaml envVariables, resolves ${hostname_varName} refs via API, and writes .env file."`
	ServiceHostname string   `json:"serviceHostname,omitempty" jsonschema:"Hostname of the service to modify env vars on. Required unless project=true."`
	Project         bool     `json:"project,omitempty"         jsonschema:"Set to true to manage project-level env vars instead of service-level."`
	Variables       []string `json:"variables,omitempty"       jsonschema:"List of env vars. For set: KEY=VALUE strings (literal values). For delete: KEY names only."`
	SkipRestart     bool     `json:"skipRestart,omitempty"     jsonschema:"set/delete: skip the automatic service restart after the env change. Default false (auto-restart affected services so the new value takes effect). Pass true only if you will redeploy immediately afterwards and the restart would be wasted."`
}

// envChangeResult wraps the underlying set/delete result with the list of
// services that were auto-restarted so the new env value takes effect.
type envChangeResult struct {
	Process            *platform.Process   `json:"process,omitempty"`
	RestartedServices  []string            `json:"restartedServices,omitempty"`
	RestartWarnings    []string            `json:"restartWarnings,omitempty"`
	RestartSkipped     bool                `json:"restartSkipped,omitempty"`
	RestartedProcesses []*platform.Process `json:"restartedProcesses,omitempty"`
	NextActions        string              `json:"nextActions,omitempty"`
}

// RegisterEnv registers the zerops_env tool.
// selfHostname is the hostname of the service running ZCP — it is excluded
// from auto-restart so the tool does not kill its own MCP connection.
func RegisterEnv(srv *mcp.Server, client platform.Client, projectID, selfHostname string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_env",
		Description: "Manage environment variables. Actions: set, delete, generate-dotenv. Scope: service (serviceHostname) or project (project=true). Values are stored literally. After set/delete affected services AUTO-RESTART so the new value takes effect. Pass skipRestart=true only if deploying immediately. generate-dotenv: resolves ${hostname_varName} refs, writes .env. Read keys via zerops_discover includeEnvs=true.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Manage environment variables",
			DestructiveHint: boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input EnvInput) (*mcp.CallToolResult, any, error) {
		onProgress := buildProgressCallback(ctx, req)

		switch input.Action {
		case "set":
			setResult, err := ops.EnvSet(ctx, client, projectID, input.ServiceHostname, input.Project, input.Variables)
			if err != nil {
				return convertError(err), nil, nil
			}
			if setResult.Process != nil {
				setResult.Process, _ = pollManageProcess(ctx, client, setResult.Process, onProgress)
			}
			resp := envChangeResult{Process: setResult.Process}
			applyAutoRestart(ctx, client, projectID, input, selfHostname, &resp, onProgress)
			return jsonResult(resp), nil, nil
		case "delete":
			delResult, err := ops.EnvDelete(ctx, client, projectID, input.ServiceHostname, input.Project, input.Variables)
			if err != nil {
				return convertError(err), nil, nil
			}
			if delResult.Process != nil {
				delResult.Process, _ = pollManageProcess(ctx, client, delResult.Process, onProgress)
			}
			resp := envChangeResult{Process: delResult.Process}
			applyAutoRestart(ctx, client, projectID, input, selfHostname, &resp, onProgress)
			return jsonResult(resp), nil, nil
		case "generate-dotenv":
			result, err := ops.EnvGenerateDotenv(ctx, client, projectID, input.ServiceHostname, "")
			if err != nil {
				return convertError(err), nil, nil
			}
			return jsonResult(result), nil, nil
		case "":
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter, "Action is required",
				"Use set, delete, or generate-dotenv")), nil, nil
		default:
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter, "Invalid action '"+input.Action+"'",
				"Use set, delete, or generate-dotenv")), nil, nil
		}
	})
}

// applyAutoRestart restarts the services affected by an env change so the new
// value takes effect. Populates resp with the outcomes. Best-effort — restart
// failures are reported as warnings; the env change itself has already
// succeeded by the time this is called.
func applyAutoRestart(
	ctx context.Context,
	client platform.Client,
	projectID string,
	input EnvInput,
	selfHostname string,
	resp *envChangeResult,
	onProgress ops.ProgressCallback,
) {
	if input.SkipRestart {
		resp.RestartSkipped = true
		resp.NextActions = "skipRestart=true — env values are NOT yet live in containers. Restart manually (zerops_manage action=restart) or deploy to pick them up."
		return
	}

	targets, warn := resolveRestartTargets(ctx, client, projectID, input, selfHostname)
	if warn != "" {
		resp.RestartWarnings = append(resp.RestartWarnings, warn)
	}
	if len(targets) == 0 {
		// No ACTIVE runtime services to restart — the env value is stored and
		// will be injected at the next service start/deploy.
		resp.NextActions = "No ACTIVE services needed restart. The new env value will be injected when a service starts or deploys."
		return
	}

	for _, t := range targets {
		proc, err := client.RestartService(ctx, t.id)
		if err != nil {
			resp.RestartWarnings = append(resp.RestartWarnings,
				fmt.Sprintf("%s: restart failed: %v — run zerops_manage action=restart manually", t.hostname, err))
			continue
		}
		if proc != nil {
			polled, _ := pollManageProcess(ctx, client, proc, onProgress)
			resp.RestartedProcesses = append(resp.RestartedProcesses, polled)
		}
		resp.RestartedServices = append(resp.RestartedServices, t.hostname)
	}

	switch {
	case len(resp.RestartedServices) == 0:
		resp.NextActions = "Restart failed on all affected services — see restartWarnings."
	case len(resp.RestartWarnings) > 0:
		resp.NextActions = fmt.Sprintf("Restarted %d service(s), %d failed — see restartWarnings.", len(resp.RestartedServices), len(resp.RestartWarnings))
	default:
		resp.NextActions = fmt.Sprintf("Restarted %s — env values are live.", strings.Join(resp.RestartedServices, ", "))
	}
}

type restartTarget struct {
	id       string
	hostname string
}

// resolveRestartTargets returns the services that should be restarted after
// an env change. Scoping rules:
//
//   - Service-level change: just the named service, if ACTIVE.
//   - Project-level change: all ACTIVE user-runtime services, EXCLUDING the
//     ZCP service running this code (would kill our own MCP connection) and
//     managed services (they consume their own generated credentials, not
//     user-set project envs).
//
// Returns a warning string if the target service is not found or not ACTIVE
// (so the agent understands why no restart happened).
func resolveRestartTargets(
	ctx context.Context,
	client platform.Client,
	projectID string,
	input EnvInput,
	selfHostname string,
) ([]restartTarget, string) {
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return nil, fmt.Sprintf("could not list services for auto-restart: %v", err)
	}

	if input.Project {
		var targets []restartTarget
		for _, svc := range services {
			if !isAutoRestartEligible(svc, selfHostname) {
				continue
			}
			targets = append(targets, restartTarget{id: svc.ID, hostname: svc.Name})
		}
		return targets, ""
	}

	// Service-level: only the named service.
	for _, svc := range services {
		if svc.Name != input.ServiceHostname {
			continue
		}
		if svc.Status != statusActive {
			return nil, fmt.Sprintf("%s is %s (not ACTIVE) — env stored, will apply on next start", svc.Name, svc.Status)
		}
		return []restartTarget{{id: svc.ID, hostname: svc.Name}}, ""
	}
	return nil, fmt.Sprintf("service %q not found for auto-restart", input.ServiceHostname)
}

// isAutoRestartEligible reports whether a service should be restarted after a
// project-level env change.
func isAutoRestartEligible(svc platform.ServiceStack, selfHostname string) bool {
	if svc.Status != statusActive {
		return false
	}
	if svc.IsSystem() {
		return false
	}
	if selfHostname != "" && svc.Name == selfHostname {
		return false
	}
	// Managed services (databases, caches, search, object/shared storage,
	// messaging) consume their own credentials — user-set project envs do
	// not affect their operation, so restarting is unnecessary downtime.
	if workflow.IsManagedService(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName) {
		return false
	}
	return true
}
