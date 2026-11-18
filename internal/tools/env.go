package tools

import (
	"context"
	"fmt"
	"slices"
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
	// Mate names the Mate's own vault, the third target beside
	// ServiceHostname and Project (docs/spec-mate.md §5.8a).
	Mate FlexBool `json:"mate,omitempty"`
	// Sensitive is optional: nil lets the set pick per key by name.
	Sensitive *FlexBool `json:"sensitive,omitempty"`
	// Key, Label and Why are request's: the name asked for, what it is in
	// people's words, and one line of what it is for. Never a value — the
	// person types that into Mate.
	Key   string `json:"key,omitempty"`
	Label string `json:"label,omitempty"`
	Why   string `json:"why,omitempty"`
	// Reason is why's name before it: a session that still holds the old
	// schema asks on its first try.
	Reason string `json:"reason,omitempty"`
	// Replace asks for a key the vault already holds, with ReplaceWhy the
	// line the person reads on what is wrong with the saved value. Again
	// asks once more after the person declined an earlier ask.
	Replace    FlexBool `json:"replace,omitempty"`
	ReplaceWhy string   `json:"replaceWhy,omitempty"`
	Again      FlexBool `json:"again,omitempty"`
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
//
// orgAIKey adds the ai-key action, which only a Mate has (WithOrgAIKey);
// asks adds request, which only a Mate's client draws (WithAskCard), and with
// it mate, the Mate's own vault.
func envInputSchema(orgAIKey, asks bool) *jsonschema.Schema {
	actions := []any{"get", "set", "delete", "generate-dotenv"}
	target := "Service to operate on; required for get/set/delete/request unless project=true. generate-dotenv: deprecated fallback for setup."
	actionDescription := "get: keys + ${host_var} refs — reference a value as $VAR by name, never paste it. set: upsert KEY=VALUE pairs. delete: remove keys. generate-dotenv: writes a resolved .env from a local zerops.yaml."
	if asks {
		target = "Service to operate on; required for get/set/delete/request unless project=true or mate=true. generate-dotenv: deprecated fallback for setup."
		actions = []any{"get", "set", "delete", "request", "generate-dotenv"}
		actionDescription = "get: keys + ${host_var} refs — reference a value as $VAR by name, never paste it. set: upsert KEY=VALUE pairs. delete: remove keys. request: ask the person, when the feature using it is being built, for a value only they have; it goes to the vault, never the chat. generate-dotenv: writes a resolved .env from a local zerops.yaml."
	}
	if orgAIKey {
		actions = append(actions, "ai-key")
		actionDescription += " ai-key: the organization's AI key into the Shared vault, unseen (NEBIUS_API_KEY sensitive, NEBIUS_BASE_URL), for an AI feature in the app; skipRestart as set."
	}
	properties := map[string]*jsonschema.Schema{
		"action": {
			Type:        "string",
			Enum:        actions,
			Description: actionDescription,
		},
		"serviceHostname": {
			Type:        "string",
			Description: target,
		},
		"key": {
			Type:        "string",
			Description: "request: the key's name.",
		},
		"label": {
			Type:        "string",
			Description: "request: its name for people (Stripe key).",
		},
		"reason": {Type: "string"},
		"why": {
			Type:        "string",
			Description: "request: what for, where to find it. Never a value.",
		},
		"replace": flexBoolSchema("request: ask for a held key whose saved value is wrong; needs replaceWhy."),
		"replaceWhy": {
			Type:        "string",
			Description: "replace: what is wrong with the saved value; the person reads it.",
		},
		"again": flexBoolSchema("request: ask anew after a message said declined."),
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
		"sensitive":   flexBoolSchema("set/request: true = sensitive (masked; Mate's vault shows it), false = plain. Omitted: by name — SECRET|TOKEN|KEY|PASSWORD|PASS|DSN|PRIVATE|CREDENTIAL → sensitive, else plain."),
		"skipRestart": flexBoolSchema("set/delete: skip restarting the services that read the key (readers). Pass true only when you deploy right after."),
	}
	if asks {
		properties["mate"] = flexBoolSchema("true: your own vault, the Mate's, instead of a service's or the Shared — values only you read (a test sign-in, a key for your own tools); no app reads it. Use one as `zcp with KEY -- <command>`.")
	}
	return objectSchema(properties, "action")
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
	Scope              ops.EnvRequestScope `json:"scope,omitempty"`
	ServiceHostname    string              `json:"serviceHostname,omitempty"`
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
func RegisterEnv(srv *mcp.Server, client platform.Client, projectID, selfHostname string, opts ...EnvOption) {
	var options envOptions
	for _, opt := range opts {
		opt(&options)
	}
	vaults := "a service's own (serviceHostname) or the Shared (project=true)"
	actions := "get (keys), set (upsert), delete, generate-dotenv (local .env)"
	if options.asks {
		vaults = "a service's own (serviceHostname), the Shared (project=true) or yours alone (mate=true: a test sign-in, a key for your own tools; no app reads it)"
		actions = "get (keys), set (upsert), delete, request (a value only the person has; never ask in chat), generate-dotenv (local .env)"
	}
	if options.aiKey != nil {
		actions += ", ai-key (the organization's AI key into the Shared vault, unseen)"
	}
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_env",
		Description: "Manage the vault: " + vaults + ". Actions: " + actions + ". An app reads only what its run.envVariables references: `NAME: ${KEY}` (own, else Shared), `${host_KEY}`. set: sensitive per key, expands <@...>, 'stored' verifies. set/delete restart the key's readers unless skipRestart=true",
		InputSchema: envInputSchema(options.aiKey != nil, options.asks),
		Annotations: &mcp.ToolAnnotations{
			Title:           "Manage environment variables",
			DestructiveHint: boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input EnvInput) (*mcp.CallToolResult, any, error) {
		onProgress := buildProgressCallback(ctx, req)

		// Outside a Mate there is no ai-key action: it is an invalid one.
		if input.Action == "ai-key" && options.aiKey != nil {
			return setOrgAIKey(ctx, client, projectID, selfHostname, options.aiKey, input, onProgress), nil, nil
		}
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
			input, ownVault, err := resolveEnvTarget(input, selfHostname, options.asks)
			if err != nil {
				return convertError(err), nil, nil
			}
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
			got := projectEnvGetResponse(result, input.Project.Bool())
			if ownVault {
				got.Envs = withoutHiddenNames(got.Envs)
			}
			return jsonResult(got), nil, nil
		case "set":
			input, ownVault, err := resolveEnvTarget(input, selfHostname, options.asks)
			if err != nil {
				return convertError(err), nil, nil
			}
			setResult, err := ops.EnvSet(ctx, client, projectID, input.ServiceHostname, input.Project.Bool(), input.Variables, input.Sensitive.Ptr())
			if err != nil {
				return convertError(err), nil, nil
			}
			var setTimedOut bool
			if setResult.Process != nil {
				setResult.Process, setTimedOut = pollManageProcess(ctx, client, setResult.Process, onProgress)
			}
			// `stored[]` names each key written, whether it is masked and
			// whether it replaced one — never a value (ops.StoredEnv marshals
			// none). A plain value echoed back put a secret written plain into
			// the conversation (Franta, 2026-10-28: a session secret set with
			// sensitive=false beside a plain URL); an unredacted echo once put
			// the raw PAT there (prod.txt T3).
			resp := envChangeResult{Process: setResult.Process, Stored: setResult.Stored, Scope: setResult.Scope, ServiceHostname: setResult.ServiceHostname, TimedOut: setTimedOut}
			resp.ShadowWarnings, resp.ShadowUnverified = detectSetShadows(ctx, client, projectID, input, selfHostname, setResult.Stored)
			setKeys := make([]string, 0, len(setResult.Stored))
			for _, st := range setResult.Stored {
				setKeys = append(setKeys, st.Key)
			}
			if ownVault {
				resp.NextActions = ownVaultChangeWords(setKeys, false)
				return jsonResult(resp), nil, nil
			}
			applyAutoRestart(ctx, client, projectID, input, selfHostname, setKeys, false, &resp, onProgress)
			return jsonResult(resp), nil, nil
		case "delete":
			input, ownVault, err := resolveEnvTarget(input, selfHostname, options.asks)
			if err != nil {
				return convertError(err), nil, nil
			}
			delResult, err := ops.EnvDelete(ctx, client, projectID, input.ServiceHostname, input.Project.Bool(), input.Variables)
			if err != nil {
				return convertError(err), nil, nil
			}
			var delTimedOut bool
			if delResult.Process != nil {
				delResult.Process, delTimedOut = pollManageProcess(ctx, client, delResult.Process, onProgress)
			}
			resp := envChangeResult{Process: delResult.Process, TimedOut: delTimedOut}
			if ownVault {
				resp.NextActions = ownVaultChangeWords(input.Variables, true)
				return jsonResult(resp), nil, nil
			}
			applyAutoRestart(ctx, client, projectID, input, selfHostname, input.Variables, true, &resp, onProgress)
			return jsonResult(resp), nil, nil
		case envActionRequest:
			// A request writes nothing: Mate draws a field for the person,
			// whose value goes straight to the vault — it never enters the
			// conversation, this result, or any log. Without a Mate's client
			// nobody draws it, so nothing is asked and nothing recorded.
			if !options.asks {
				return noAskCard(input.Key), nil, nil
			}
			input, ownVault, err := resolveEnvTarget(input, selfHostname, options.asks)
			if err != nil {
				return convertError(err), nil, nil
			}
			return requestVaultValue(ctx, client, projectID, selfHostname, options, input, ownVault), nil, nil
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

// applyAutoRestart restarts the services that read the changed keys so the
// new value takes effect. Under the strict env model a process reads only
// the entries of its run.envVariables, so a reader is a runtime whose
// deployed entries reference the key — `${KEY}` for its own value or the
// Shared one, through a chain of its entries, or `${host_KEY}` for a
// service's key (ops.ProjectEnvScopes). A key no entry references still
// reaches a process under its own name (byNameReaders): those are named for
// the agent to restart if it needs to, never restarted — a restart stops its
// dev servers — and nextActions says how to reference it. Populates resp with
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
	unreadHint := joinSentences(nothingReadsHint(plan.unreadKeys, input), byNameHint(plan.byName))
	switch {
	case plan.poolOwner:
		unreadHint = poolEnvHint(keys, input.ServiceHostname)
	case plan.boxOwner:
		unreadHint = boxEnvHint(keys, input.ServiceHostname)
	}

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

	// failed restarts were refused; unfollowed ones were started, but whether
	// they finished is unknown. Neither is counted as restarted.
	failed, unfollowed := 0, 0
	for _, t := range plan.targets {
		proc, err := client.RestartService(ctx, t.id)
		if err != nil {
			failed++
			resp.RestartWarnings = append(resp.RestartWarnings,
				fmt.Sprintf("%s: restart failed%s: %v", t.hostname, platform.ClassOf(err).Note(), err))
			continue
		}
		if proc != nil && proc.ID != "" {
			polled, pollErr := ops.PollProcess(ctx, client, proc.ID, onProgress)
			if pollErr != nil {
				// The restart was asked for, but whether it finished is
				// unknown: never counted as restarted, so "live" is not claimed.
				unfollowed++
				resp.RestartedProcesses = append(resp.RestartedProcesses, proc)
				resp.RestartWarnings = append(resp.RestartWarnings,
					fmt.Sprintf("%s: restart process %s was started but could not be followed to its end%s: %v", t.hostname, proc.ID, platform.ClassOf(pollErr).Note(), pollErr))
				continue
			}
			resp.RestartedProcesses = append(resp.RestartedProcesses, polled)
		}
		resp.RestartedServices = append(resp.RestartedServices, t.hostname)
	}

	var outcome string
	switch {
	case failed == len(plan.targets):
		outcome = "Restart failed on every reader — see restartWarnings."
	case failed+unfollowed > 0:
		outcome = restartTally(len(resp.RestartedServices), failed, unfollowed)
	case len(resp.ShadowWarnings) > 0:
		outcome = fmt.Sprintf("Restarted %s, but %d set key(s) are SHADOWED by a higher env layer and are NOT what the container reads — see shadowWarnings.", strings.Join(resp.RestartedServices, ", "), len(resp.ShadowWarnings))
	case len(resp.ShadowUnverified) > 0:
		// A higher-layer read failed for some service — we cannot confirm the
		// set isn't silently shadowed there, so do NOT claim "values are live" (E4).
		outcome = fmt.Sprintf("Restarted %s — env live where verified, but shadow status is UNVERIFIED, the env layers could not be read: %s.", strings.Join(resp.RestartedServices, ", "), strings.Join(resp.ShadowUnverified, "; "))
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

// restartTally is the outcome of restarts not all of which were seen to
// finish: how many restarted, failed, and started without being followed.
func restartTally(restarted, failed, unfollowed int) string {
	var parts []string
	if restarted > 0 {
		parts = append(parts, fmt.Sprintf("Restarted %d reader(s)", restarted))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed to restart", failed))
	}
	if unfollowed > 0 {
		parts = append(parts, fmt.Sprintf("%d started restarting but could not be followed to the end", unfollowed))
	}
	return strings.Join(parts, ", ") + " — see restartWarnings."
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
	// poolOwner is true when the changed keys are a Swarm pool's own: its
	// leases read them in their ssh sessions as they are (DP), never
	// through a restart or a deploy.
	poolOwner bool
	// boxOwner is true when the changed keys are a Zerops Box's own: it
	// applies them at once, or on a reload for its API key, never through a
	// restart (PS-8).
	boxOwner bool
	// unlisted is true when the project's services could not be listed, so
	// no reader is known.
	unlisted bool
	// selfReads is true when the service running this session reads a key:
	// it is never restarted.
	selfReads bool
	// idle are the readers that are not live: each reads the value when it
	// starts.
	idle []string
	// byName are the live runtimes that have a key no entry references by its
	// own name: never restarted by themselves, named for the agent to choose.
	byName []string
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
		plan.unlisted = true
		plan.warnings = append(plan.warnings, fmt.Sprintf("could not list services to find the readers: %v — restart the services that read the key manually", err))
		return plan
	}
	owner := ""
	if !input.Project.Bool() {
		owner = input.ServiceHostname
	}
	for _, svc := range services {
		if owner == "" || svc.Name != owner {
			continue
		}
		typeName := svc.ServiceStackTypeInfo.ServiceStackTypeVersionName
		plan.poolOwner = topology.IsPoolType(typeName)
		plan.boxOwner = topology.IsSandboxType(typeName)
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
			// No entry references it, yet the platform still hands an unreferenced Shared
			// value to every service, and a service's own to that service, by its name. In
			// the Mate's own project a restart stops the agent's dev servers, so these are
			// named for the agent to choose from, never restarted by themselves.
			for _, name := range byNameReaders(scopes, owner, key, platformReadKeys[key] || plan.poolOwner || plan.boxOwner) {
				if isAutoRestartEligible(byName[name], selfHostname) && !slices.Contains(plan.byName, name) {
					plan.byName = append(plan.byName, name)
				}
			}
		}
		for _, name := range readers {
			svc := byName[name]
			switch {
			case selfHostname != "" && name == selfHostname:
				plan.selfReads = true
				plan.warnings = append(plan.warnings, fmt.Sprintf("%s reads %s but runs this session — not restarted; it picks the value up at its next restart or deploy", name, key))
			case !isAutoRestartEligible(svc, selfHostname):
				if !svc.IsLive() {
					plan.idle = append(plan.idle, name)
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

// byNameReaders are the services a key no entry references still reaches
// under its own name (spec-zerops-env-lifecycle §3: project→service
// inheritance is not gated): every service for a Shared key, its owner for a
// service's own, unless an entry or the service's own value of that name
// shadows it. None for a key the platform reads itself, or a pool's or a
// Box's own.
func byNameReaders(scopes *ops.ProjectEnvScopes, owner, key string, none bool) []string {
	if none {
		return nil
	}
	var readers []string
	for _, svc := range scopes.Services {
		if owner != "" && svc.Hostname != owner {
			continue
		}
		if _, entry := svc.Entries[key]; entry || (owner == "" && svc.Own[key]) {
			continue
		}
		readers = append(readers, svc.Hostname)
	}
	return readers
}

// byNameHint names the running services that have an unreferenced key by its
// own name: nothing restarted them, and the agent chooses.
func byNameHint(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return fmt.Sprintf("%s still %s it by its own name (an unreferenced value reaches the process anyway) and keeps the old value until it restarts: nothing restarted it, since a restart stops its dev server — restart the ones that need it (zerops_manage action=\"restart\").",
		strings.Join(names, ", "), map[bool]string{true: "have", false: "has"}[len(names) > 1])
}

// nothingReadsHint is the fix for keys no service reads: reference them in
// run.envVariables and deploy. A service's own key is read as `${KEY}` by
// that service, as `${host_KEY}` by any other.
func nothingReadsHint(keys []string, input EnvInput) string {
	var platformRead []string
	keys, platformRead = splitPlatformRead(keys)
	var hint string
	if len(platformRead) > 0 {
		hint = fmt.Sprintf("%s is read by the platform itself, not by a process: a change applies to the next SSH connection, with no restart or deploy.", strings.Join(platformRead, ", "))
	}
	return joinSentences(hint, appReadHint(keys, input))
}

// platformReadKeys are the env keys the platform reads itself, live, whatever
// a service's run.envVariables say (knowledge/themes/operations.md: an
// sshIsolation change applies to the next SSH connection).
var platformReadKeys = map[string]bool{"sshIsolation": true}

// splitPlatformRead parts keys into those a process reads and those the
// platform reads itself.
func splitPlatformRead(keys []string) (appRead, platformRead []string) {
	for _, k := range keys {
		if platformReadKeys[k] {
			platformRead = append(platformRead, k)
			continue
		}
		appRead = append(appRead, k)
	}
	return appRead, platformRead
}

// appReadHint is the fix for keys a process reads but none does yet.
func appReadHint(keys []string, input EnvInput) string {
	if len(keys) == 0 {
		return ""
	}
	names := strings.Join(keys, ", ")
	if input.Project.Bool() {
		return fmt.Sprintf("No zerops.yaml entry references %s yet — reference it in the run.envVariables of each service that needs it (e.g. `NAME: ${%s}`), then deploy.", names, keys[0])
	}
	host := input.ServiceHostname
	return fmt.Sprintf("No zerops.yaml entry references %s yet — reference it in %s's run.envVariables (e.g. `NAME: ${%s}`), or from another service as `${%s_%s}`, then deploy.",
		names, host, keys[0], strings.ReplaceAll(host, "-", "_"), keys[0])
}

// resolveEnvTarget settles the vault a get, set, delete or request names.
// mate=true is the Mate's own vault — the user variables of the service zcp
// runs in (docs/spec-mate.md §5.8a) — offered only where asks are (inMate),
// and refused beside another target. The container's own hostname names the
// same vault, so ownVault says so however it was named: nothing restarts for
// its keys, no app reads them, and a name it never holds is refused here,
// before anything is written.
func resolveEnvTarget(input EnvInput, selfHostname string, inMate bool) (resolved EnvInput, ownVault bool, err error) {
	hasOwn := inMate && selfHostname != ""
	if input.Mate.Bool() {
		switch {
		case !hasOwn:
			return input, false, platform.NewPlatformError(platform.ErrInvalidUsage,
				"mate=true names a Mate's own vault, and zcp serves no Mate here",
				"Name the vault with serviceHostname, or project=true for the Shared one.")
		case input.Project.Bool():
			return input, false, platform.NewPlatformError(platform.ErrInvalidUsage,
				"mate=true and project=true name two vaults",
				"Pass one: mate=true for your own vault, project=true for the Shared one.")
		case input.ServiceHostname != "" && input.ServiceHostname != selfHostname:
			return input, false, platform.NewPlatformError(platform.ErrInvalidUsage,
				fmt.Sprintf("mate=true and serviceHostname=%q name two vaults", input.ServiceHostname),
				"Pass one: mate=true for your own vault, serviceHostname for that service's.")
		}
		input.ServiceHostname = selfHostname
	}
	if !hasOwn || input.Project.Bool() || input.ServiceHostname != selfHostname {
		return input, false, nil
	}
	return input, true, refuseHiddenName(input.Variables)
}

// withoutHiddenNames leaves out of a get of the Mate's own vault the names
// Mate and zcp keep for themselves, which no vault lists.
func withoutHiddenNames(envs []map[string]any) []map[string]any {
	return slices.DeleteFunc(envs, func(env map[string]any) bool {
		key, _ := env["key"].(string)
		return ops.IsVaultHidden(key)
	})
}

// refuseHiddenName refuses the variables of a set or a delete in the Mate's
// own vault that carry a name Mate and zcp keep for themselves: the vault
// never lists it, so the person could not see what was stored, and a removed
// one is a key Mate or zcp runs on. Named by its key only — never what
// follows `=`. A request refuses its own key (ops.EnvRequest).
func refuseHiddenName(variables []string) error {
	for _, variable := range variables {
		key, _, _ := strings.Cut(variable, "=")
		if ops.IsVaultHidden(key) {
			return platform.NewPlatformError(platform.ErrInvalidParameter,
				fmt.Sprintf("%q is a name Mate and zcp keep for themselves: your own vault never lists it, and zerops_env neither writes nor removes it there", key),
				"Use another name, without the ZCP_, MATE_ or GITEA_ prefix and not GIT_TOKEN.")
		}
	}
	return nil
}

// ownVaultUseWords say how a value of the Mate's own vault is used: it is in
// the container's live env store soon after the write, and `zcp with` hands
// it to one command, since the session's own processes keep the env they
// started with. arrives opens the sentence, saying from when.
func ownVaultUseWords(keys []string, arrives string) string {
	return fmt.Sprintf("%s, with no restart: run the command that needs it as `zcp with %s -- <command>`. A process started earlier, this session's shell too, keeps the env it started with. Never print it.",
		arrives, strings.Join(keys, ","))
}

// ownVaultChangeWords is the next action for a set or a delete in the
// Mate's own vault: nothing reads it but the Mate, so nothing restarted.
func ownVaultChangeWords(keys []string, deleting bool) string {
	names := strings.Join(keys, ", ")
	if deleting {
		return names + ": removed from your own vault; nothing was restarted. It leaves this container about two seconds after the write; a process started earlier keeps the env it started with."
	}
	return names + ": stored in your own vault, which only you read — no app does, and nothing was restarted. " +
		ownVaultUseWords(keys, "It is in this container about two seconds after the write")
}

// poolEnvHint is the next action for a pool's own keys: its service env
// reaches every container's ssh session as it is, so a lease clones and
// pushes with GIT_TOKEN at once; processes the container started at boot (the
// dev-server supervisor and its servers) keep the env they started with.
func poolEnvHint(keys []string, pool string) string {
	if len(keys) == 0 {
		return ""
	}
	return fmt.Sprintf("%s is %s's service env, which reaches its containers' ssh sessions as it is: its leases read it now (a lease clones and pushes with GIT_TOKEN), with no restart or deploy. A pool is never restarted for an env change — that would restart containers others hold. Processes a container started at boot — the dev-server supervisor and the servers it runs — keep the env they started with.",
		strings.Join(keys, ", "), pool)
}

// boxAPIKeyEnv is the Zerops Box variable holding its workspace API key.
const boxAPIKeyEnv = "ZBOX_WORKSPACE_API_KEY"

// boxEnvHint is the next action for a change to a Zerops Box's own variables
// (spec-workflows.md §8 PS-8). A Box is never restarted for one: a restart
// stops its VM, and every running sandbox comes back PARKED without its
// memory. Its ordinary variables (sandbox secrets, integration credentials)
// apply at once; ZBOX_WORKSPACE_API_KEY is read by the orchestrator only when
// it starts, so a reload — which keeps the sandboxes running — applies it.
func boxEnvHint(keys []string, box string) string {
	var others []string
	apiKey := false
	for _, k := range keys {
		if k == boxAPIKeyEnv {
			apiKey = true
			continue
		}
		others = append(others, k)
	}
	var parts []string
	if len(others) > 0 {
		parts = append(parts, fmt.Sprintf("%s is %s's own variable, which a Zerops Box applies at once — no restart or reload: new sandboxes and integrations use the new value, and a sandbox given the old one as a secret becomes stale.",
			strings.Join(others, ", "), box))
	}
	if apiKey {
		parts = append(parts, fmt.Sprintf("%s applies when %s reloads: zerops_manage action=reload serviceHostname=%q — a reload keeps its running sandboxes. Until then only the old key works. A Box is never restarted for an env change: a restart or stop brings every running sandbox back PARKED without its memory.",
			boxAPIKeyEnv, box, box))
	}
	return strings.Join(parts, " ")
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
		return nil, []string{fmt.Sprintf("every service (their list could not be read%s: %v)", platform.ClassOf(err).Note(), err)}
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
			unverified = append(unverified, fmt.Sprintf("%s%s: %v", svc.Name, platform.ClassOf(err).Note(), err))
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
// is restarted: live, a user runtime, not the service running this session —
// never a managed service, a Swarm pool or a Zerops Box.
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
	// A Swarm pool is never restarted: that restarts containers others hold,
	// and its service env reaches their ssh sessions as it is (DP).
	if topology.IsPoolType(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName) {
		return false
	}
	// A Zerops Box is never restarted: its restart stops the VM and every
	// running sandbox loses its memory, while its own variables apply at
	// once or on a reload (PS-8).
	if topology.IsSandboxType(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName) {
		return false
	}
	return true
}
