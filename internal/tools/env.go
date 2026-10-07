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
	// Sensitive is optional: nil lets the set pick per key by name.
	Sensitive *FlexBool `json:"sensitive,omitempty"`
	// Key and Reason are request's: the name asked for and one sentence for
	// the person. Never a value — the person types that into Mate.
	Key    string `json:"key,omitempty"`
	Reason string `json:"reason,omitempty"`
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
			Enum:        []any{"get", "set", "delete", "request", "generate-dotenv"},
			Description: "get: keys + ${host_var} refs — reference a value as $VAR by name, never paste it. set: upsert KEY=VALUE pairs. delete: remove keys. request: ask the person for a value only they have (key, reason); it goes to the vault, never the chat. generate-dotenv: writes a resolved .env from a local zerops.yaml.",
		},
		"serviceHostname": {
			Type:        "string",
			Description: "Service to operate on; required for get/set/delete/request unless project=true. generate-dotenv: deprecated fallback for setup.",
		},
		"key": {
			Type:        "string",
			Description: "request: the env key asked for (name only).",
		},
		"reason": {
			Type:        "string",
			Description: "request: one sentence for the person — what it is for, where to find it. Never a value.",
		},
		"setup": {
			Type:        "string",
			Description: "generate-dotenv: name of the zerops.yaml setup block to render. Recipe / multi-setup yaml uses setup names like 'dev', 'prod', 'worker' that are not always service hostnames. Empty + single-block yaml: auto-pick. Empty + multi-block yaml: refuses with the available names. Empty + zero-block yaml: falls back to serviceHostname.",
		},
		"preview": flexBoolSchema("generate-dotenv: dry-run. Builds the plan and returns the diff vs current .env without writing. Use to inspect what would change before committing."),
		"force":   flexBoolSchema("generate-dotenv: write even when the existing .env has keys no source produces (user edits that would be dropped). Confirm they are safe to drop, or move them to .env.local first."),
		"project": flexBoolSchema("true: the project's Shared vault (project env) instead of a service's own."),
		"variables": {
			Type:        "array",
			Items:       &jsonschema.Schema{Type: "string"},
			Description: "List of env vars. set: KEY=VALUE strings (literal values). delete: KEY names only. Ignored by get and generate-dotenv.",
		},
		"sensitive":   flexBoolSchema("set/request: true = sensitive (write-only, masked on every read), false = plain. Omitted: by name — SECRET|TOKEN|KEY|PASSWORD|PASS|DSN|PRIVATE|CREDENTIAL → sensitive, else plain."),
		"skipRestart": flexBoolSchema("set/delete: skip restarting the services that read the key (readers). Pass true only when you deploy right after."),
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
//     var keys; Refs carries the ${host_var} wiring references for a
//     managed service; Project nil.
//   - project=true: Service nil; Envs is the project-level env var keys;
//     Refs empty (project vars have no ${host_var} form); Project carries
//     identity (id/name/status) WITHOUT duplicating envs.
//
// get returns KEYS, not values (the operator references $VAR by name);
// Refs is the curated wiring menu for a managed dependency so the agent
// can reference it without inventing the syntax.
//
// Warnings preserve env-fetch diagnostics that ops.Discover emits when
// per-service or project-level env reads fail partially — dropping
// them would silently hide those failures from agents.
type EnvGetResponse struct {
	Service  *EnvGetServiceInfo `json:"service,omitempty"`
	Envs     []map[string]any   `json:"envs"`
	Refs     []string           `json:"refs,omitempty"`
	Project  *EnvGetProjectInfo `json:"project,omitempty"`
	Warnings []string           `json:"warnings,omitempty"`
}

// EnvGetServiceInfo is the service-identification subset env-get
// returns. Omits AdoptionState / IsInfrastructure / MountPath /
// Subdomain / Containers / Resources / Ports — those are discover
// concerns, not env-read concerns. (Refs is surfaced at the
// EnvGetResponse top level, parallel to Envs.)
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
		// Refs is the curated ${host_var} wiring menu ops.Discover computes
		// for a managed service; nil for runtimes (no managed refs).
		resp.Refs = s.Refs
	}
	return resp
}

// envChangeResult wraps the underlying set/delete result with the readers of
// each changed key — the services whose deployed run.envVariables reference
// it — and the readers that were restarted so the new value takes effect.
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
	Readers            map[string][]string `json:"readers,omitempty"`
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
		Description: "Manage the vault: a service's own (serviceHostname) or the Shared (project=true). Actions: get (keys), set (upsert), delete, request (a value only the person has; never ask in chat), generate-dotenv (local .env). An app reads only what its run.envVariables references: `NAME: ${KEY}` (own, else Shared), `${host_KEY}`. set: sensitive per key, expands <@...>, 'stored' verifies. set/delete restart the key's readers unless skipRestart=true",
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
			// includeEnvs=true, scoped to the requested target. It returns env
			// var KEYS (+ ${host_var} refs for a managed service), NOT values:
			// the operator references a value as $VAR by name, so the literal
			// never enters context. To inspect a specific value for diagnosis,
			// zerops_discover includeEnvValues=true reads them (managed-service
			// credential fields and sensitive values are masked even there).
			// This action exists so the agent's natural first attempt (get)
			// succeeds instead of bouncing through a decision tree of wrong
			// actions.
			if !input.Project.Bool() && input.ServiceHostname == "" {
				return convertError(platform.NewPlatformError(
					platform.ErrInvalidParameter,
					"get requires serviceHostname or project=true",
					"Example: zerops_env action=get serviceHostname=\"db\" OR zerops_env action=get project=true. To list env vars for all services in one call, use zerops_discover includeEnvs=true.")), nil, nil
			}
			// project=true is the project-scope read — it ignores any
			// serviceHostname (matching set/delete precedence). Passing both a
			// hostname (scoped Discover, includeProjectEnvs=false) AND
			// project=true previously projected the never-populated project
			// envs and silently returned envs:null (P0-4). Clear the hostname
			// so project=true takes the unscoped path that DOES attach project
			// envs.
			discoverHost := input.ServiceHostname
			if input.Project.Bool() {
				discoverHost = ""
			}
			// includeEnvValues=false: get returns KEYS + refs, not values, so
			// there is no credential value to leak (the operator references
			// $VAR by name). includeProjectEnvs=false keeps a service-scoped
			// get from broadening to project envs; project-level reads go
			// through project=true → unscoped Discover, a separate intent.
			result, err := ops.Discover(ctx, client, projectID, discoverHost, true, false, false)
			if err != nil {
				return convertError(err), nil, nil
			}
			return jsonResult(projectEnvGetResponse(result, input.Project.Bool())), nil, nil
		case "set":
			setResult, err := ops.EnvSet(ctx, client, projectID, input.ServiceHostname, input.Project.Bool(), input.Variables, input.Sensitive.Ptr())
			if err != nil {
				return convertError(err), nil, nil
			}
			var setTimedOut bool
			if setResult.Process != nil {
				setResult.Process, setTimedOut = pollManageProcess(ctx, client, setResult.Process, onProgress)
			}
			// Redact credential-class and sensitive values in the echo:
			// `stored[]` verifies WHAT landed, never the secret itself — an
			// unredacted set response put the raw PAT into the chat transcript
			// (the raw zerops_env rotation bypass, prod.txt T3). Same single
			// owner (RedactEnvValue) as the get/discover renderers.
			storedEcho := make([]ops.StoredEnv, len(setResult.Stored))
			copy(storedEcho, setResult.Stored)
			for i := range storedEcho {
				// A user set is never a managed service's own credential field,
				// so "" serviceType is correct — the ZCP-owned and sensitive
				// classes mask.
				if masked, redacted := ops.RedactEnvValue(storedEcho[i].Key, storedEcho[i].Value, "", storedEcho[i].Sensitive); redacted {
					storedEcho[i].Value = masked
				}
			}
			resp := envChangeResult{Process: setResult.Process, Stored: storedEcho, TimedOut: setTimedOut}
			resp.ShadowWarnings, resp.ShadowUnverified = detectSetShadows(ctx, client, projectID, input, selfHostname, setResult.Stored)
			setKeys := make([]string, 0, len(setResult.Stored))
			for _, st := range setResult.Stored {
				setKeys = append(setKeys, st.Key)
			}
			applyAutoRestart(ctx, client, projectID, input, selfHostname, setKeys, false, &resp, onProgress)
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
			applyAutoRestart(ctx, client, projectID, input, selfHostname, input.Variables, true, &resp, onProgress)
			return jsonResult(resp), nil, nil
		case "request":
			// A request writes nothing: Mate draws a field for the person,
			// whose value goes straight to the vault — it never enters the
			// conversation, this result, or any log.
			req, err := ops.EnvRequest(ctx, client, projectID, input.ServiceHostname, input.Project.Bool(), input.Key, input.Sensitive.Ptr())
			if err != nil {
				return convertError(err), nil, nil
			}
			return jsonResult(envRequestAnswer(req)), nil, nil
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
				"Use get, set, delete, request, or generate-dotenv")), nil, nil
		default:
			// Invalid-action errors guided agents toward generate-dotenv in the
			// past, which fails from arbitrary working directories (see LOG.txt
			// post-mortem cascade). Point them at the action they probably
			// meant (get) and at zerops_discover for bulk reads.
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter, "Invalid action '"+input.Action+"'",
				"Valid actions: get, set, delete, request, generate-dotenv. To read env vars for a service use get (or zerops_discover includeEnvs=true for all services at once). generate-dotenv is only for writing a local .env file from a local zerops.yaml.")), nil, nil
		}
	})
}

// envRequestResult is the answer to action=request: Requested when the
// person was asked, AlreadySet when the key is in that vault already.
type envRequestResult struct {
	Requested   *ops.EnvRequestResult `json:"requested,omitempty"`
	AlreadySet  *ops.EnvRequestResult `json:"alreadySet,omitempty"`
	NextActions string                `json:"nextActions"`
}

func envRequestAnswer(req *ops.EnvRequestResult) envRequestResult {
	if req.AlreadySet {
		where := "the Shared vault"
		if req.Scope == ops.EnvRequestScopeService {
			where = req.ServiceHostname + "'s vault"
		}
		return envRequestResult{
			AlreadySet: req,
			NextActions: fmt.Sprintf("%s is already in %s; nothing was asked. Reference it by name (`NAME: ${%s}` in run.envVariables) — its value is never read.",
				req.Key, where, req.Key),
		}
	}
	return envRequestResult{
		Requested: req,
		NextActions: fmt.Sprintf("The person was asked for %s in Mate; it goes straight to the vault, never through the chat. You will hear in a zerops-update note when it is set. Do not ask for the value in the chat; continue with what does not need it, or end your turn.",
			req.Key),
	}
}

// applyAutoRestart restarts the services that read the changed keys so the
// new value takes effect. Under the strict env model a process reads only
// the entries of its run.envVariables, so a reader is a runtime whose
// deployed entries reference the key — `${KEY}` for its own value or the
// Shared one, through a chain of its entries, or `${host_KEY}` for a
// service's key (ops.ProjectEnvScopes). Nothing reads it → nothing restarts,
// and nextActions says how to make a service read it. Populates resp with
// the readers and the outcomes. Best-effort — a restart failure is a
// warning; the env change itself has already succeeded.
func applyAutoRestart(
	ctx context.Context,
	client platform.Client,
	projectID string,
	input EnvInput,
	selfHostname string,
	keys []string,
	deleting bool,
	resp *envChangeResult,
	onProgress ops.ProgressCallback,
) {
	plan := planRestart(ctx, client, projectID, input, selfHostname, keys, deleting)
	resp.Readers = plan.readers
	resp.RestartWarnings = append(resp.RestartWarnings, plan.warnings...)
	unreadHint := nothingReadsHint(plan.unreadKeys, input)

	if input.SkipRestart.Bool() {
		resp.RestartSkipped = true
		next := "skipRestart=true — the value is stored, but a running process keeps its boot env until it restarts."
		if len(plan.targets) > 0 {
			next += fmt.Sprintf(" Restart %s (zerops_manage action=restart) or deploy to pick it up.", strings.Join(targetNames(plan.targets), ", "))
		}
		resp.NextActions = joinSentences(next, unreadHint)
		return
	}

	if len(plan.targets) == 0 {
		resp.NextActions = unreadHint
		if resp.NextActions == "" {
			resp.NextActions = "No live service reads the changed key(s); nothing restarted. A reader picks the value up when it starts or deploys."
		}
		return
	}

	attempted := 0
	for _, t := range plan.targets {
		attempted++
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
	failed := attempted - len(resp.RestartedServices)

	var outcome string
	switch {
	case len(resp.RestartedServices) == 0:
		outcome = "Restart failed on every reader — see restartWarnings."
	case failed > 0:
		outcome = fmt.Sprintf("Restarted %d reader(s), %d failed — see restartWarnings.", len(resp.RestartedServices), failed)
	case len(resp.ShadowWarnings) > 0:
		outcome = fmt.Sprintf("Restarted %s, but %d set key(s) are SHADOWED by a higher env layer and are NOT what the container reads — see shadowWarnings.", strings.Join(resp.RestartedServices, ", "), len(resp.ShadowWarnings))
	case len(resp.ShadowUnverified) > 0:
		// A higher-layer read failed for some service — we cannot confirm the
		// set isn't silently shadowed there, so do NOT claim "values are live" (E4).
		outcome = fmt.Sprintf("Restarted %s — env live where verified, but shadow status is UNVERIFIED for %d service(s) (env layer read failed; retry the env check after `zcli vpn up`).", strings.Join(resp.RestartedServices, ", "), len(resp.ShadowUnverified))
	case plan.uncertain:
		outcome = fmt.Sprintf("Restarted %s — see restartWarnings for the services whose env could not be read.", strings.Join(resp.RestartedServices, ", "))
	case deleting:
		outcome = fmt.Sprintf("Restarted %s — see restartWarnings: the deleted key no longer resolves there.", strings.Join(resp.RestartedServices, ", "))
	case unreadHint != "":
		outcome = fmt.Sprintf("Restarted %s — the readers have the new value.", strings.Join(resp.RestartedServices, ", "))
	default:
		outcome = fmt.Sprintf("Restarted %s — env values are live.", strings.Join(resp.RestartedServices, ", "))
	}
	resp.NextActions = joinSentences(outcome, unreadHint)
}

// restartPlan is what an env change restarts and why.
type restartPlan struct {
	// readers maps each changed key to the services whose deployed entries
	// read it (all of them, live or not, this session's own included).
	readers map[string][]string
	// targets are the readers a restart applies to: live, user runtimes,
	// never the service running this session — plus, when the env could not
	// be read, every such runtime that might read it.
	targets    []restartTarget
	unreadKeys []string
	warnings   []string
	uncertain  bool
}

// planRestart finds the readers of the changed keys from the project's
// deployed rows (ops.ReadProjectEnvScopes). keys are the keys a set stored
// or a delete removed; deleting adds a warning per reader, which now reads
// the literal `${KEY}`. When the project's env cannot be read at all, every
// eligible runtime in scope (all of them for a Shared key, the named service
// for its own) is restarted, as before readers were known.
func planRestart(ctx context.Context, client platform.Client, projectID string, input EnvInput, selfHostname string, keys []string, deleting bool) restartPlan {
	plan := restartPlan{readers: map[string][]string{}}
	services, err := ops.ListProjectServices(ctx, client, projectID)
	if err != nil {
		plan.warnings = append(plan.warnings, fmt.Sprintf("could not list services to find the readers: %v — restart the services that read the key manually", err))
		return plan
	}
	owner := ""
	if !input.Project.Bool() {
		owner = input.ServiceHostname
	}

	scopes, err := ops.ReadProjectEnvScopes(ctx, client, projectID, services)
	if err != nil {
		plan.uncertain = true
		plan.warnings = append(plan.warnings, fmt.Sprintf("could not read the project's env to find the readers (%v) — restarted every runtime that might read it", err))
		for _, svc := range services {
			if isAutoRestartEligible(svc, selfHostname) && (owner == "" || svc.Name == owner) {
				plan.targets = append(plan.targets, restartTarget{id: svc.ID, hostname: svc.Name})
			}
		}
		return plan
	}

	byName := make(map[string]platform.ServiceStack, len(services))
	for _, svc := range services {
		byName[svc.Name] = svc
	}
	targeted := map[string]bool{}
	addTarget := func(svc platform.ServiceStack) {
		if !targeted[svc.Name] {
			targeted[svc.Name] = true
			plan.targets = append(plan.targets, restartTarget{id: svc.ID, hostname: svc.Name})
		}
	}

	for _, key := range keys {
		readers := scopes.Readers(ops.VaultKey{Service: owner, Key: key})
		if readers == nil {
			readers = []string{}
		}
		plan.readers[key] = readers
		if len(readers) == 0 {
			plan.unreadKeys = append(plan.unreadKeys, key)
		}
		for _, name := range readers {
			svc := byName[name]
			switch {
			case selfHostname != "" && name == selfHostname:
				plan.warnings = append(plan.warnings, fmt.Sprintf("%s reads %s but runs this session — not restarted; it picks the value up at its next restart or deploy", name, key))
			case !isAutoRestartEligible(svc, selfHostname):
				if !svc.IsLive() {
					plan.warnings = append(plan.warnings, fmt.Sprintf("%s reads %s but is %s (not live) — it picks the value up when it starts", name, key, svc.Status))
				}
			default:
				addTarget(svc)
			}
			if deleting {
				plan.warnings = append(plan.warnings, fmt.Sprintf("%s still references %s: after the delete its entry reaches the app as the literal text ${%s} — remove or repoint the reference in %s's zerops.yaml run.envVariables and deploy", name, key, key, name))
			}
		}
	}

	for _, name := range scopes.Unread {
		svc := byName[name]
		if !isAutoRestartEligible(svc, selfHostname) {
			continue
		}
		plan.uncertain = true
		plan.warnings = append(plan.warnings, fmt.Sprintf("could not read %s's deployed env entries — restarted it in case it reads the key", name))
		addTarget(svc)
	}
	return plan
}

// nothingReadsHint is the fix for keys no service reads: reference them in
// run.envVariables and deploy. A service's own key is read as `${KEY}` by
// that service, as `${host_KEY}` by any other.
func nothingReadsHint(keys []string, input EnvInput) string {
	if len(keys) == 0 {
		return ""
	}
	names := strings.Join(keys, ", ")
	if input.Project.Bool() {
		return fmt.Sprintf("Nothing reads %s yet — reference it in the run.envVariables of each service that needs it (e.g. `NAME: ${%s}`), then deploy.", names, keys[0])
	}
	host := input.ServiceHostname
	return fmt.Sprintf("Nothing reads %s yet — reference it in %s's run.envVariables (e.g. `NAME: ${%s}`), or from another service as `${%s_%s}`, then deploy.",
		names, host, keys[0], strings.ReplaceAll(host, "-", "_"), keys[0])
}

func targetNames(targets []restartTarget) []string {
	out := make([]string, 0, len(targets))
	for _, t := range targets {
		out = append(out, t.hostname)
	}
	return out
}

func joinSentences(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, " ")
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
// guidance: the key and the two places — the project, and the yaml or the
// service that wins — never the winning value. zerops_env get returns keys,
// not values, and a warning reaches the agent and the person's screen the
// same way; the agent reads the value with zerops_discover
// includeEnvValues=true when it needs it. The fix differs by winning layer:
// yaml-baked is edit-yaml-and-redeploy (the key is owned by the yaml, spec
// §2); service userData is change-or-delete the service var.
func formatLayeredShadow(s ops.LayeredShadow) string {
	switch s.WinningLayer {
	case ops.EnvLayerYamlBaked:
		return fmt.Sprintf("%q set at project scope is shadowed on %s: its zerops.yaml run.envVariables bakes %s (yaml owns the key — spec §2). %s reads the yaml value, not the project one. Edit %s's zerops.yaml and redeploy to change it there.",
			s.Key, s.Hostname, s.Key, s.Hostname, s.Hostname)
	case ops.EnvLayerService:
		return fmt.Sprintf("%q set at project scope is shadowed on %s: a service-level env sets %s (service > project — spec §2). %s reads the service value. Change or delete the service-level %s on %s.",
			s.Key, s.Hostname, s.Key, s.Hostname, s.Key, s.Hostname)
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

// isAutoRestartEligible reports whether a service that reads a changed key
// is restarted: live, a user runtime, not the service running this session.
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
