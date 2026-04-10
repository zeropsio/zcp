package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// EnvInput is the input type for zerops_env.
//
// Project and SkipRestart are FlexBool so stringified boolean values
// from some LLM agents — e.g. `{"project": "true"}` — unmarshal
// cleanly instead of being rejected at the MCP schema layer with a
// non-actionable "has type 'string', want 'boolean'" error. The v7
// post-mortem log (LOG.txt line 65) caught exactly this failure mode
// on the first zerops_env call.
type EnvInput struct {
	Action          string   `json:"action"`
	ServiceHostname string   `json:"serviceHostname,omitempty"`
	Project         FlexBool `json:"project,omitempty"`
	Variables       []string `json:"variables,omitempty"`
	SkipRestart     FlexBool `json:"skipRestart,omitempty"`
}

// envInputSchema is the explicit InputSchema for zerops_env. It
// declares project/skipRestart as FlexBool (oneOf boolean|string), and
// documents every action in the action description — including `get`,
// which was previously implicit (the agent had to guess that reading
// env vars belongs to zerops_discover). The v7 post-mortem log showed
// an agent trying `get` five times in a row, then cascading into
// `generate-dotenv` attempts that failed because the cwd had no
// zerops.yaml — a UX failure that cost ~10 tool calls. Exposing `get`
// as a first-class action eliminates that branch entirely.
func envInputSchema() *jsonschema.Schema {
	return objectSchema(map[string]*jsonschema.Schema{
		"action": {
			Type:        "string",
			Enum:        []any{"get", "set", "delete", "generate-dotenv"},
			Description: "get: return env var keys and values for a service (serviceHostname) or the project (project=true). set: upsert KEY=VALUE pairs. delete: remove keys. generate-dotenv: reads a local zerops.yaml and writes a resolved .env (requires zerops.yaml in the working directory).",
		},
		"serviceHostname": {
			Type:        "string",
			Description: "Hostname of the service to operate on. Required for get/set/delete unless project=true. Ignored by generate-dotenv (which reads zerops.yaml instead).",
		},
		"project": flexBoolSchema("Set to true to operate on project-level env vars instead of service-level. Valid for get/set/delete."),
		"variables": {
			Type:        "array",
			Items:       &jsonschema.Schema{Type: "string"},
			Description: "List of env vars. set: KEY=VALUE strings (literal values). delete: KEY names only. Ignored by get and generate-dotenv.",
		},
		"skipRestart": flexBoolSchema("set/delete: skip the automatic service restart after the env change. Default false (auto-restart affected services so the new value takes effect). Pass true only if you will redeploy immediately afterwards and the restart would be wasted."),
	}, "action")
}

// envChangeResult wraps the underlying set/delete result with the list of
// services that were auto-restarted so the new env value takes effect.
type envChangeResult struct {
	Process            *platform.Process   `json:"process,omitempty"`
	Stored             []ops.StoredEnv     `json:"stored,omitempty"`
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
		Description: "Manage env vars. Actions: get (read), set (upsert), delete, generate-dotenv (write local .env from local zerops.yaml). Scope: service via serviceHostname, or project=true. set values expand <@...> via zParser; encoding prefixes (base64:, hex:) are rejected. Response 'stored' verifies what landed. set/delete auto-restart affected services unless skipRestart=true. For bulk env reads across many services, prefer zerops_discover includeEnvs=true.",
		InputSchema: envInputSchema(),
		Annotations: &mcp.ToolAnnotations{
			Title:           "Manage environment variables",
			DestructiveHint: boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input EnvInput) (*mcp.CallToolResult, any, error) {
		onProgress := buildProgressCallback(ctx, req)

		switch input.Action {
		case "get":
			// get delegates to the same discovery path used by zerops_discover
			// includeEnvs=true, scoped to the requested target. It always
			// returns values (not just keys) because "get" on a single service
			// is explicit intent to read them. If the agent wants many services
			// at once, zerops_discover is still the right tool — this action
			// exists so the agent's natural first attempt (get) succeeds
			// instead of bouncing through a decision tree of wrong actions.
			if !input.Project.Bool() && input.ServiceHostname == "" {
				return convertError(platform.NewPlatformError(
					platform.ErrInvalidParameter,
					"get requires serviceHostname or project=true",
					"Example: zerops_env action=get serviceHostname=\"db\" OR zerops_env action=get project=true. To list env vars for all services in one call, use zerops_discover includeEnvs=true.")), nil, nil
			}
			result, err := ops.Discover(ctx, client, projectID, input.ServiceHostname, true, true)
			if err != nil {
				return convertError(err), nil, nil
			}
			return jsonResult(result), nil, nil
		case "set":
			setResult, err := ops.EnvSet(ctx, client, projectID, input.ServiceHostname, input.Project.Bool(), input.Variables)
			if err != nil {
				return convertError(err), nil, nil
			}
			if setResult.Process != nil {
				setResult.Process, _ = pollManageProcess(ctx, client, setResult.Process, onProgress)
			}
			resp := envChangeResult{Process: setResult.Process, Stored: setResult.Stored}
			applyAutoRestart(ctx, client, projectID, input, selfHostname, &resp, onProgress)
			return jsonResult(resp), nil, nil
		case "delete":
			delResult, err := ops.EnvDelete(ctx, client, projectID, input.ServiceHostname, input.Project.Bool(), input.Variables)
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
				"Use get, set, delete, or generate-dotenv")), nil, nil
		default:
			// Invalid-action errors guided agents toward generate-dotenv in the
			// past, which fails from arbitrary working directories (see LOG.txt
			// post-mortem cascade). Point them at the action they probably
			// meant (get) and at zerops_discover for bulk reads.
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter, "Invalid action '"+input.Action+"'",
				"Valid actions: get, set, delete, generate-dotenv. To read env vars for a service use get (or zerops_discover includeEnvs=true for all services at once). generate-dotenv is only for writing a local .env file from a local zerops.yaml.")), nil, nil
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
	if input.SkipRestart.Bool() {
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

	if input.Project.Bool() {
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
