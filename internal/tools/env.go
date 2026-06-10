package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
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
	Setup           string   `json:"setup,omitempty"`
	Preview         FlexBool `json:"preview,omitempty"`
	Force           FlexBool `json:"force,omitempty"`
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
			Description: "Hostname of the service to operate on. Required for get/set/delete unless project=true. For generate-dotenv: deprecated — prefer the setup parameter (see below). Still accepted as a fallback when setup is empty; emits a deprecation warning in the result.",
		},
		"setup": {
			Type:        "string",
			Description: "generate-dotenv: name of the zerops.yaml setup block to render. Recipe / multi-setup yaml uses setup names like 'dev', 'prod', 'worker' that are not always service hostnames. Empty + single-block yaml: auto-pick. Empty + multi-block yaml: refuses with the available names. Empty + zero-block yaml: falls back to serviceHostname.",
		},
		"preview": flexBoolSchema("generate-dotenv: dry-run. Builds the plan and returns the diff vs current .env without writing. Use to inspect what would change before committing."),
		"force":   flexBoolSchema("generate-dotenv: bypass the refuse-on-unowned-edits safety gate. By default ZCP refuses to write when the existing .env has keys not produced by any source (project envVariables, zerops.yaml run.envVariables, .env.local) — those are user-direct edits at risk of being discarded. Set force=true after confirming the unowned keys are safe to drop, or move them to .env.local first."),
		"project": flexBoolSchema("Set to true to operate on project-level env vars instead of service-level. Valid for get/set/delete."),
		"variables": {
			Type:        "array",
			Items:       &jsonschema.Schema{Type: "string"},
			Description: "List of env vars. set: KEY=VALUE strings (literal values). delete: KEY names only. Ignored by get and generate-dotenv.",
		},
		"skipRestart": flexBoolSchema("set/delete: skip the automatic service restart after the env change. Default false (auto-restart affected services so the new value takes effect). Pass true only if you will redeploy immediately afterwards and the restart would be wasted."),
	}, "action")
}

// EnvGetResponse is the focused wire shape for `zerops_env action=get`.
// Replaces the prior leaky reuse of `ops.DiscoverResult` which forced
// agent consumers of env-get to skim past project info / service list /
// adoption fields irrelevant to env consumption (plan
// plans/discover-adoption-state-enum-2026-05-27.md §"Wire-leak fix").
//
// The focused shape also makes it structurally impossible for new
// DiscoverResult enrichment (adoptionState being the prime example)
// to leak into env-get output — env-get explicitly projects only the
// env-relevant fields from the underlying Discover call.
//
// Scope rules:
//   - serviceHostname=<X>: Service populated; Envs is the service's env
//     vars; Project nil.
//   - project=true: Service nil; Envs is the project-level env vars;
//     Project carries identity (id/name/status) WITHOUT duplicating envs.
//
// Warnings preserve env-fetch diagnostics that ops.Discover emits when
// per-service or project-level env reads fail partially — dropping
// them would silently hide those failures from agents.
type EnvGetResponse struct {
	Service  *EnvGetServiceInfo `json:"service,omitempty"`
	Envs     []map[string]any   `json:"envs"`
	Project  *EnvGetProjectInfo `json:"project,omitempty"`
	Warnings []string           `json:"warnings,omitempty"`
}

// EnvGetServiceInfo is the service-identification subset env-get
// returns. Omits AdoptionState / IsInfrastructure / MountPath /
// Subdomain / Containers / Resources / Ports / Refs — those are
// discover concerns, not env-read concerns.
type EnvGetServiceInfo struct {
	Hostname  string `json:"hostname"`
	ServiceID string `json:"serviceId"`
	Type      string `json:"type"`
	Status    string `json:"status"`
}

// EnvGetProjectInfo is the project-identification subset env-get
// returns for project=true. Omits the project Envs list — those are
// surfaced at top-level `envs` in EnvGetResponse so the location of
// "the asked-for vars" is canonical regardless of scope.
type EnvGetProjectInfo struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// projectEnvGetResponse projects an ops.DiscoverResult into the
// focused EnvGetResponse. Two scope branches:
//
//   - serviceScope (project=false): Service[0] from DiscoverResult
//     carries identity + envs; project envs are NOT included
//     (caller deliberately scoped to one service).
//   - projectScope (project=true): top-level project envs migrate
//     to EnvGetResponse.Envs; Service stays nil; Project carries
//     identity only.
//
// Warnings carry through verbatim (env-fetch diagnostics from
// ops.Discover).
func projectEnvGetResponse(result *ops.DiscoverResult, projectScope bool) *EnvGetResponse {
	if result == nil {
		return &EnvGetResponse{}
	}
	resp := &EnvGetResponse{
		Warnings: result.Warnings,
	}
	if projectScope {
		resp.Project = &EnvGetProjectInfo{
			ID:     result.Project.ID,
			Name:   result.Project.Name,
			Status: result.Project.Status,
		}
		resp.Envs = result.Project.Envs
		return resp
	}
	if len(result.Services) > 0 {
		s := result.Services[0]
		resp.Service = &EnvGetServiceInfo{
			Hostname:  s.Hostname,
			ServiceID: s.ServiceID,
			Type:      s.Type,
			Status:    s.Status,
		}
		resp.Envs = s.Envs
	}
	return resp
}

// envChangeResult wraps the underlying set/delete result with the list of
// services that were auto-restarted so the new env value takes effect.
//
// ShadowWarnings carries cross-layer shadows detected on a project-scope set:
// keys whose stored project value is overridden by a higher layer (yaml-baked
// run.envVariables or service userData) on some service, so the set is NOT
// what that service reads (spec §2). When present, the success text stops
// claiming the values are "live".
type envChangeResult struct {
	Process            *platform.Process   `json:"process,omitempty"`
	Stored             []ops.StoredEnv     `json:"stored,omitempty"`
	TimedOut           bool                `json:"timedOut,omitempty"`
	RestartedServices  []string            `json:"restartedServices,omitempty"`
	RestartWarnings    []string            `json:"restartWarnings,omitempty"`
	ShadowWarnings     []string            `json:"shadowWarnings,omitempty"`
	ShadowUnverified   []string            `json:"shadowUnverified,omitempty"`
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
			// includeProjectEnvs=false: env get serviceHostname=X must NOT
			// leak project env VALUES (includeEnvValues=true is always set
			// here). Project-level reads go through project=true → unscoped
			// Discover, which is a separate intent.
			result, err := ops.Discover(ctx, client, projectID, input.ServiceHostname, true, true, false)
			if err != nil {
				return convertError(err), nil, nil
			}
			return jsonResult(projectEnvGetResponse(result, input.Project.Bool())), nil, nil
		case "set":
			setResult, err := ops.EnvSet(ctx, client, projectID, input.ServiceHostname, input.Project.Bool(), input.Variables)
			if err != nil {
				return convertError(err), nil, nil
			}
			var setTimedOut bool
			if setResult.Process != nil {
				setResult.Process, setTimedOut = pollManageProcess(ctx, client, setResult.Process, onProgress)
			}
			resp := envChangeResult{Process: setResult.Process, Stored: setResult.Stored, TimedOut: setTimedOut}
			resp.ShadowWarnings, resp.ShadowUnverified = detectSetShadows(ctx, client, projectID, input, selfHostname, setResult.Stored)
			applyAutoRestart(ctx, client, projectID, input, selfHostname, &resp, onProgress)
			return jsonResult(resp), nil, nil
		case "delete":
			delResult, err := ops.EnvDelete(ctx, client, projectID, input.ServiceHostname, input.Project.Bool(), input.Variables)
			if err != nil {
				return convertError(err), nil, nil
			}
			var delTimedOut bool
			if delResult.Process != nil {
				delResult.Process, delTimedOut = pollManageProcess(ctx, client, delResult.Process, onProgress)
			}
			resp := envChangeResult{Process: delResult.Process, TimedOut: delTimedOut}
			applyAutoRestart(ctx, client, projectID, input, selfHostname, &resp, onProgress)
			return jsonResult(resp), nil, nil
		case "generate-dotenv":
			// Setup parameter takes precedence; serviceHostname falls
			// through with a deprecation warning so existing callers
			// keep working through the migration window.
			selectedSetup := input.Setup
			var deprecationWarning string
			if selectedSetup == "" && input.ServiceHostname != "" {
				selectedSetup = input.ServiceHostname
				deprecationWarning = "serviceHostname for generate-dotenv is deprecated; pass setup=<name> instead. Recipe / multi-setup zerops.yaml uses setup names that are not always service hostnames."
			}
			result, err := ops.EnvGenerateDotenv(ctx, client, projectID, selectedSetup, "", ops.EnvGenerateDotenvOptions{
				Preview: input.Preview.Bool(),
				Force:   input.Force.Bool(),
			})
			if err != nil {
				return convertError(err), nil, nil
			}
			if deprecationWarning != "" {
				result.Warnings = append(result.Warnings, deprecationWarning)
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
		resp.NextActions = "skipRestart=true — the value lands in the env store (~5-10s) but the RUNNING process keeps its boot env until it restarts. Restart manually (zerops_manage action=restart) or deploy to pick it up."
		return
	}

	targets, warn := resolveRestartTargets(ctx, client, projectID, input, selfHostname)
	if warn != "" {
		resp.RestartWarnings = append(resp.RestartWarnings, warn)
	}
	if len(targets) == 0 {
		// No ACTIVE runtime services to restart — the env value is stored and
		// will be injected at the next service start/deploy.
		resp.NextActions = "No live services needed restart. The new env value will be injected when a service starts or deploys."
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
	case len(resp.ShadowWarnings) > 0:
		resp.NextActions = fmt.Sprintf("Restarted %s, but %d set key(s) are SHADOWED by a higher env layer and are NOT what the container reads — see shadowWarnings.", strings.Join(resp.RestartedServices, ", "), len(resp.ShadowWarnings))
	case len(resp.ShadowUnverified) > 0:
		// A higher-layer read failed for some service — we cannot confirm the
		// set isn't silently shadowed there, so do NOT claim "values are live" (E4).
		resp.NextActions = fmt.Sprintf("Restarted %s — env live where verified, but shadow status is UNVERIFIED for %d service(s) (env layer read failed; retry the env check after `zcli vpn up`).", strings.Join(resp.RestartedServices, ", "), len(resp.ShadowUnverified))
	default:
		resp.NextActions = fmt.Sprintf("Restarted %s — env values are live.", strings.Join(resp.RestartedServices, ", "))
	}
}

// detectSetShadows reports cross-layer shadows for a project-scope set: keys
// whose just-set project value a service's higher layer (yaml-baked
// run.envVariables or service userData) overrides with a different value, so
// the set silently has no effect on that service (spec §2 precedence).
//
// Service-scope sets are never silently shadowed — a yaml-owned key 400s in
// ops.EnvSet, and service userData outranks project — so this returns nil for
// them. Scoped to the same services auto-restart targets (active, non-managed,
// non-self): exactly the services the "values are live" claim covers. Best-
// effort: read failures yield no warning rather than failing the set.
// Returns (warnings, unverified): warnings are confirmed shadows; unverified
// lists services whose higher-layer read FAILED (transient/API), so whether
// they shadow the just-set value is unknown — the caller must then NOT claim
// "env values are live" with false confidence (E4).
func detectSetShadows(ctx context.Context, client platform.Client, projectID string, input EnvInput, selfHostname string, stored []ops.StoredEnv) (warnings, unverified []string) {
	if !input.Project.Bool() || len(stored) == 0 {
		return nil, nil
	}
	services, err := ops.ListProjectServices(ctx, client, projectID)
	if err != nil {
		return nil, nil
	}
	lower := make([]ops.EffectiveEnvVar, 0, len(stored))
	for _, s := range stored {
		lower = append(lower, ops.EffectiveEnvVar{Key: s.Key, Value: s.Value, Layer: ops.EnvLayerProject})
	}
	for _, svc := range services {
		if !isAutoRestartEligible(svc, selfHostname) {
			continue
		}
		higher, err := ops.ServiceHigherLayers(ctx, client, svc)
		if err != nil {
			// Precondition failure — can't verify this service's shadows.
			unverified = append(unverified, svc.Name)
			continue
		}
		// A failed layer read (Unavailable) means an incomplete view: the
		// unread layer might shadow the just-set value, so we cannot confirm
		// "no shadow". Record unverified rather than silently reporting clean.
		if higher.ServiceState.Unavailable() || higher.YamlBakedState.Unavailable() {
			unverified = append(unverified, svc.Name)
			continue
		}
		for _, sh := range ops.DetectLayeredShadows(svc.Name, lower, higher.Service, higher.YamlBaked) {
			warnings = append(warnings, formatLayeredShadow(sh))
		}
	}
	return warnings, unverified
}

// formatLayeredShadow renders a single cross-layer shadow as agent-actionable
// guidance. The winning value is redacted when it is a secret. The fix differs
// by winning layer: yaml-baked is edit-yaml-and-redeploy (the key is owned by
// the yaml, spec §2); service userData is change-or-delete the service var.
func formatLayeredShadow(s ops.LayeredShadow) string {
	val := s.WinningValue
	if s.WinningSensitive {
		val = "<redacted>"
	}
	switch s.WinningLayer {
	case ops.EnvLayerYamlBaked:
		return fmt.Sprintf("%q set at project scope is shadowed on %s: its zerops.yaml run.envVariables bakes %s=%s (yaml owns the key — spec §2). %s reads the yaml value, not the project one. Edit %s's zerops.yaml and redeploy to change it there.",
			s.Key, s.Hostname, s.Key, val, s.Hostname, s.Hostname)
	case ops.EnvLayerService:
		return fmt.Sprintf("%q set at project scope is shadowed on %s: a service-level env sets %s=%s (service > project — spec §2). %s reads the service value. Change or delete the service-level %s on %s.",
			s.Key, s.Hostname, s.Key, val, s.Hostname, s.Key, s.Hostname)
	case ops.EnvLayerProject:
		// Project is the lowest-precedence layer (spec §2) — it can never be the
		// WINNING/shadowing layer (DetectLayeredShadows only ever sets
		// WinningLayer to yaml-baked or service). Present to satisfy exhaustive;
		// renders the generic message if a future producer ever sets it.
		return fmt.Sprintf("%q set at project scope is shadowed on %s by a higher env layer (spec §2).", s.Key, s.Hostname)
	default:
		return fmt.Sprintf("%q set at project scope is shadowed on %s by a higher env layer (spec §2).", s.Key, s.Hostname)
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
// Returns a warning string if the target service is not found or not live
// (so the agent understands why no restart happened).
func resolveRestartTargets(
	ctx context.Context,
	client platform.Client,
	projectID string,
	input EnvInput,
	selfHostname string,
) ([]restartTarget, string) {
	services, err := ops.ListProjectServices(ctx, client, projectID)
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
		if !svc.IsLive() {
			return nil, fmt.Sprintf("%s is %s (not live) — env stored, will apply on next start", svc.Name, svc.Status)
		}
		return []restartTarget{{id: svc.ID, hostname: svc.Name}}, ""
	}
	return nil, fmt.Sprintf("service %q not found for auto-restart", input.ServiceHostname)
}

// isAutoRestartEligible reports whether a service should be restarted after a
// project-level env change.
func isAutoRestartEligible(svc platform.ServiceStack, selfHostname string) bool {
	if !svc.IsLive() {
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
	if topology.IsManagedService(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName) {
		return false
	}
	return true
}
