package tools

import (
	"context"
	"errors"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// DevServerInput is the input type for zerops_dev_server.
//
// NoHTTPProbe is FlexBool so the MCP schema accepts both JSON booleans
// and stringified forms — matches every other boolean input across the
// tool surface. The reflection-based schema would otherwise publish
// plain `boolean` and reject the stringified form that some LLM agents
// send.
type DevServerInput struct {
	Action      string   `json:"action"`
	Hostname    string   `json:"hostname"`
	Name        string   `json:"name,omitempty"`
	Command     string   `json:"command,omitempty"`
	Port        int      `json:"port,omitempty"`
	HealthPath  string   `json:"healthPath,omitempty"`
	WaitSeconds int      `json:"waitSeconds,omitempty"`
	LogLines    int      `json:"logLines,omitempty"`
	WorkDir     string   `json:"workDir,omitempty"`
	NoHTTPProbe FlexBool `json:"noHttpProbe,omitempty"`
}

// devServerInputSchema is the explicit InputSchema for zerops_dev_server.
// Field descriptions live here rather than on struct tags so FlexBool
// fields can declare the `oneOf: [boolean, string]` schema needed by the
// stringified-boolean agents.
func devServerInputSchema() *jsonschema.Schema {
	return objectSchema(map[string]*jsonschema.Schema{
		"action": {
			Type:        "string",
			Description: "Action to perform: start, stop, status, logs, restart. start has the dev service's supervisor run the named server (declaring it in the repo's .zcp/dev-servers.json the first time) and waits for the health endpoint to return 2xx. stop ends the server's whole process group and frees its port. status reports the supervisor's state and probes the port — without name on a dev service with several servers, each of them. logs tails the server's own log. restart is stop+start.",
		},
		"hostname": {
			Type:        "string",
			Description: "Target dev-container hostname (e.g. apidev, appdev, workerdev). Must exist in the current project.",
		},
		"name": {
			Type:        "string",
			Description: "Name of the dev server in the repo's .zcp/dev-servers.json (e.g. web, api, worker) — lowercase letters, digits, dashes. Omit for the dev service's one server: it is named after the hostname. Name each server when the repo runs several on one dev service.",
		},
		"command": {
			Type:        "string",
			Description: "Shell command that runs the dev server (run as `/bin/sh -c` in workDir, e.g. 'npm run start:dev', 'PORT=3000 bun --watch index.ts'). Required the first time a server is started; later starts reuse the declared one. Passing a different command replaces the declared one. Unused by stop/status/logs.",
		},
		"port": {
			Type:        "integer",
			Description: "HTTP port the dev server listens on. Required the first time a server with HTTP is started unless noHttpProbe=true; later calls reuse the declared port. Example: 3000 for NestJS, 5173 for Vite.",
		},
		"healthPath": {
			Type:        "string",
			Description: "HTTP path to probe for readiness. A start declares it for the server, so later calls probe it without passing it again; defaults to '/' when none is declared. Example: '/api/health', '/health', '/'. Must return 2xx or 3xx to count as ready. Ignored when noHttpProbe=true.",
		},
		"waitSeconds": {
			Type:        "integer",
			Description: "How long to wait for the health probe to succeed on start/restart. Default 15, max 45. Ignored when noHttpProbe=true.",
		},
		"logLines": {
			Type:        "integer",
			Description: "Number of log lines to tail for the logs action. Default 40, max 500.",
		},
		"workDir": {
			Type:        "string",
			Description: "Absolute working directory inside the repo at /var/www (e.g. /var/www/apps/web). Defaults to /var/www. The dev-server command runs there. Set once, when the server is first declared; a declared server keeps the directory its declaration names.",
		},
		"noHttpProbe": flexBoolSchema("Skip the HTTP health probe. Set true for worker services that have no HTTP port — NATS/Kafka consumers, disk-queue runners, cron-style processes. With noHttpProbe=true, 'port' becomes optional and the tool decides 'running' from the supervisor's status after a 3-second settle. This tool CANNOT verify a worker is actually consuming messages in no-probe mode — always follow up with zerops_logs to confirm the subscription loop is alive."),
	}, "action", "hostname")
}

// DevServerDeps is what zerops_dev_server needs: the supervisor is driven
// over SSH, and moving a dev setup onto it redeploys the dev service once
// (dev_server_migrate.go).
//
// HTTPClient + StateDir are S7's public-access hook plumbing (docs/spec-
// workflows.md §8 O3 PA-1's second hook): after a successful start, a
// passing health probe proves a live listener exists, so ensurePublicAccess
// runs with that fact forced regardless of the runtime's static deferred-
// start classification. Both may be nil/empty (annotations-only registry
// snapshots, local-only installs) — the hook then no-ops.
type DevServerDeps struct {
	Client     platform.Client
	HTTPClient ops.HTTPDoer
	ProjectID  string
	SSH        ops.SSHDeployer
	StateDir   string
	AuthInfo   *auth.Info
	LogFetcher platform.LogFetcher
	RtInfo     runtime.Info
	// Devsup finds the zcp-devsup a dev setup moved onto the supervisor
	// installs.
	Devsup DevsupSource
	// CarriesParts is mate.EnvelopeCarriesParts: the result carries the
	// envelope with the dev service's devServers[] only when every mate
	// this zcp drives accepts it (docs/spec-mate.md §2.8a.3).
	CarriesParts bool
}

// RegisterDevServer registers the zerops_dev_server tool. The tool is
// only useful when zcp has an SSH deployer wired in — local (non-
// container) installs skip registration.
func RegisterDevServer(srv *mcp.Server, d DevServerDeps) {
	client, httpClient, projectID, ssh, stateDir := d.Client, d.HTTPClient, d.ProjectID, d.SSH, d.StateDir
	mcp.AddTool(srv, &mcp.Tool{
		Name: "zerops_dev_server",
		Description: "Start, stop, probe, tail, or restart a named development server on a Zerops dev container. " +
			"The dev servers run under the dev service's supervisor (zcp-devsup, the dev setup's run.start) and are declared in the repo's .zcp/dev-servers.json: " +
			"the first start of a server declares it (command, port, working directory), later starts need only its name. " +
			"Each server is its own process group with its own log; stop ends the whole group and frees the port, and the other servers keep running. " +
			"A declared server comes back by itself after its container restarts or is redeployed; one that crashes while starting is started once more after its siblings settle (status says so); any other crash stays down (status says crashed, with its exit) until you start it again. " +
			"On a dev service whose dev setup does not run the supervisor yet, the first start moves it there: zcp adds two lines to the dev setup in zerops.yaml, declares the server, and redeploys the dev service once (uncommitted work stays) — commit both files with your next change. " +
			"Results are structured {running, startMillis, healthStatus, url, logTail, reason} with a reason code on failure (crashed, port_in_use, supervisor_unresponsive, health_probe_*) so the agent can diagnose without a follow-up call. " +
			"For worker services with no HTTP port (NATS/queue consumers, cron runners), pass noHttpProbe=true. " +
			"Prefer this tool over raw Bash + ssh for every dev-server lifecycle operation.",
		InputSchema: devServerInputSchema(),
		Annotations: &mcp.ToolAnnotations{
			Title:           "Manage dev server lifecycle",
			IdempotentHint:  true,
			DestructiveHint: boolPtr(false),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input DevServerInput) (*mcp.CallToolResult, any, error) {
		params := devServerParams(input)
		params.Root = supervisorRoot(stateDir, input.Hostname)
		result, err := ops.ExecuteDevServer(ctx, ssh, client, projectID, params)
		var noSup *ops.NoSupervisorError
		var migration *devServerMigration
		if errors.As(err, &noSup) {
			migration, result, err = migrateToSupervisor(ctx, req, d, params)
		}
		if err != nil {
			// verifyDevServerTarget may return a precondition error (target
			// not RUNNING — deploy it first); a plain PlatformError carries
			// its own Suggestion through convertError.
			return convertError(err), nil, nil
		}

		resp := &devServerToolResult{DevServerResult: result}
		if migration != nil {
			resp.MigratedToSupervisor = true
			resp.MigrationTimes = &migration.times
			resp.Message = migration.sentence() + " " + resp.Message
			resp.Warnings = append(resp.Warnings, migration.warnings...)
		}
		// PA-1's second hook (docs/spec-workflows.md §8 O3): a start/restart
		// the supervisor reports running just proved a live listener — its
		// status is running only once the port accepts connections — so force
		// listener=true regardless of the runtime's static deferred-start
		// classification. The address it reports is the started server's own
		// port's, not the service's first HTTP port's. A server listening on
		// loopback only has no public address.
		if result.Running && isDevServerStartAction(input.Action) && result.Reason != ops.ReasonLoopbackOnly {
			scratch := &ops.DeployResult{}
			ensurePublicAccess(ctx, client, httpClient, projectID, stateDir, input.Hostname, scratch, true)
			if scratch.SubdomainURL != "" {
				resp.SubdomainURL = ops.DevServerSubdomainURL(ctx, client, projectID, result)
			}
			if pa := scratch.PublicAccess; pa != nil {
				pa.URL = resp.SubdomainURL
				resp.PublicAccess = pa
			}
			resp.Warnings = append(resp.Warnings, scratch.Warnings...)
		}
		if d.CarriesParts {
			if env := freshEnvelope(ctx, stateDir, client, projectID, d.RtInfo); env != nil {
				states := ops.DevServerStates(ctx, client, projectID, result)
				withServers := env.WithParts(map[string][]topology.DevServerState{input.Hostname: states})
				resp.Envelope = &withServers
			}
		}
		if input.Action == "logs" || result.Unreported {
			return jsonResult(devServerLogRead{devServerToolResult: resp}), nil, nil
		}
		return jsonResult(resp), nil, nil
	})
}

// devServerParams maps the tool input onto ops.DevServerParams.
func devServerParams(input DevServerInput) ops.DevServerParams {
	return ops.DevServerParams{
		Action:      input.Action,
		Hostname:    input.Hostname,
		Name:        input.Name,
		Command:     input.Command,
		Port:        input.Port,
		HealthPath:  input.HealthPath,
		WaitSeconds: input.WaitSeconds,
		LogLines:    input.LogLines,
		WorkDir:     input.WorkDir,
		NoHTTPProbe: input.NoHTTPProbe.Bool(),
	}
}

// isDevServerStartAction reports whether action is one that can bring up a
// listener — start or restart. status/stop/logs never do.
func isDevServerStartAction(action string) bool {
	switch strings.ToLower(action) {
	case actionStart, "restart":
		return true
	default:
		return false
	}
}

// devServerToolResult is the zerops_dev_server response envelope: every
// field of ops.DevServerResult plus the public-access enrichment the
// start-success hook adds (S7, docs/spec-workflows.md §8 O3/E9).
type devServerToolResult struct {
	*ops.DevServerResult
	// MigratedToSupervisor is true when this start moved the host onto the
	// supervisor and redeployed it (docs/spec-mate.md §2.8a.2); omitted
	// otherwise. The Mate acts on this field, never on the message sentence.
	MigratedToSupervisor bool `json:"migratedToSupervisor,omitempty"`
	// MigrationTimes is where that move spent its time; omitted otherwise.
	MigrationTimes *migrationTimes `json:"migrationTimes,omitempty"`
	// SubdomainURL is the L7 subdomain URL when the start-success hook
	// enabled (or found already-enabled) the route for this hostname.
	SubdomainURL string `json:"subdomainUrl,omitempty"`
	// PublicAccess is the PA-5 structured summary (docs/spec-workflows.md
	// §8 O3): {intent, subdomain, url, domains[]}, copied verbatim from the
	// hook's scratch *ops.DeployResult — nil when the hook never ran (a
	// non-start action, or a failed start).
	PublicAccess *ops.PublicAccessSummary `json:"publicAccess,omitempty"`
	// Warnings carries any non-fatal anomaly: a public-access hook failure,
	// or what moving the dev setup onto the supervisor could not finish.
	Warnings []string `json:"warnings,omitempty"`
	// Envelope is the post-call state envelope with the dev service's
	// devServers[] (docs/spec-mate.md §2.8a.3) — only from field-order
	// step (3), DevServerDeps.CarriesParts.
	Envelope *workflow.StateEnvelope `json:"envelope,omitempty"`
}

// devServerLogRead is the answer of a call that reports no one server's
// running — a log read, which checks nothing, or a status of several servers
// (ops.DevServerResult.Unreported): Running, always nil, shadows the embedded
// result's `running`, which a reader would take for "it stopped".
type devServerLogRead struct {
	*devServerToolResult
	Running *bool `json:"running,omitempty"`
}
