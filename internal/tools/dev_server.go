package tools

import (
	"context"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
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
	Action       string   `json:"action"`
	Hostname     string   `json:"hostname"`
	Command      string   `json:"command,omitempty"`
	Port         int      `json:"port,omitempty"`
	HealthPath   string   `json:"healthPath,omitempty"`
	LogFile      string   `json:"logFile,omitempty"`
	WaitSeconds  int      `json:"waitSeconds,omitempty"`
	LogLines     int      `json:"logLines,omitempty"`
	WorkDir      string   `json:"workDir,omitempty"`
	ProcessMatch string   `json:"processMatch,omitempty"`
	NoHTTPProbe  FlexBool `json:"noHttpProbe,omitempty"`
}

// devServerInputSchema is the explicit InputSchema for zerops_dev_server.
// Field descriptions live here rather than on struct tags so FlexBool
// fields can declare the `oneOf: [boolean, string]` schema needed by the
// stringified-boolean agents.
func devServerInputSchema() *jsonschema.Schema {
	return objectSchema(map[string]*jsonschema.Schema{
		"action": {
			Type:        "string",
			Description: "Action to perform: start, stop, status, logs, restart. start spawns the dev-server command in the background and waits for the health endpoint to return 2xx. stop kills matching processes and frees the port. status probes the health endpoint without spawning anything. logs tails the dev-server log file. restart is stop+start.",
		},
		"hostname": {
			Type:        "string",
			Description: "Target dev-container hostname (e.g. apidev, appdev, workerdev). Must exist in the current project.",
		},
		"command": {
			Type:        "string",
			Description: "Command that starts the dev server. Required for start and restart. Example: 'npm run start:dev', 'vite --host 0.0.0.0'. For env-var prefixes use the `env KEY=VAL cmd` form (e.g. 'env PORT=3000 npm run dev') — a bare `KEY=VAL cmd` prefix is rejected (the command is parsed as the program name). Unused by stop/status/logs.",
		},
		"port": {
			Type:        "integer",
			Description: "HTTP port the dev server listens on. Required for start/restart unless noHttpProbe=true, and always required for status (the status check probes this port). Optional for stop (if set, fuser -k the port). Example: 3000 for NestJS, 5173 for Vite.",
		},
		"healthPath": {
			Type:        "string",
			Description: "HTTP path to probe for readiness. Defaults to '/' if omitted. Example: '/api/health', '/health', '/'. Must return 2xx or 3xx to count as ready. Ignored when noHttpProbe=true.",
		},
		"logFile": {
			Type:        "string",
			Description: "Absolute path to the log file on the target container. Defaults to /tmp/zcp-dev-server.log. Used by start/restart (written to), logs (read from). Reusing the same default across calls keeps the log stable between start and later tail operations.",
		},
		"waitSeconds": {
			Type:        "integer",
			Description: "How long to wait for the health probe to succeed on start/restart. Default 15, max 45. Short enough to fit comfortably inside the 120s bash timeout with margin for two retries. Ignored when noHttpProbe=true.",
		},
		"logLines": {
			Type:        "integer",
			Description: "Number of log lines to tail for the logs action. Default 40, max 500.",
		},
		"workDir": {
			Type:        "string",
			Description: "Absolute working directory on the target container. Defaults to /var/www. The dev-server command runs with this as cwd.",
		},
		"processMatch": {
			Type:        "string",
			Description: "pkill -f pattern for the stop action. If omitted, stop derives a match from the first token of command. Example: 'nest', 'vite', 'npm run'.",
		},
		"noHttpProbe": flexBoolSchema("Skip the HTTP health probe after spawning. Set true for worker services that have no HTTP port — NATS/Kafka consumers, disk-queue runners, cron-style processes. With noHttpProbe=true, 'port' becomes optional (pass 0 or omit), and the tool decides 'running' from a 3-second post-spawn process-liveness check (kill -0 on the spawned pid). This tool CANNOT verify a worker is actually consuming messages in no-probe mode — always follow up with zerops_logs to confirm the subscription loop is alive. Without this flag, start/restart require a valid port and run the HTTP probe phase."),
	}, "action", "hostname")
}

// RegisterDevServer registers the zerops_dev_server tool. The tool is
// only useful when zcp has an SSH deployer wired in — local (non-
// container) installs skip registration.
//
// httpClient + stateDir are S7's public-access hook plumbing (docs/spec-
// workflows.md §8 O3 PA-1's second hook): after a successful start, a
// passing health probe proves a live listener exists, so ensurePublicAccess
// runs with that fact forced regardless of the runtime's static deferred-
// start classification. Both may be nil/empty (annotations-only registry
// snapshots, local-only installs) — the hook then no-ops (ensurePublicAccess
// needs a real stateDir to find ServiceMeta at all).
func RegisterDevServer(srv *mcp.Server, client platform.Client, httpClient ops.HTTPDoer, projectID string, ssh ops.SSHDeployer, stateDir string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "zerops_dev_server",
		Description: "Start, stop, probe, tail, or restart a long-running development server on a Zerops dev container. " +
			"Replaces the hand-rolled `ssh host \"cmd &\"` + sleep + curl pattern that historically hit Bash's 120s timeout because the SSH channel stayed open on `&`-backgrounded commands. " +
			"The tool launches the process via `ssh -T -n` + `setsid` with redirected stdio (all three are load-bearing), " +
			"bounds every phase with a tight budget — spawn 8s, probe waitSeconds+5s, tail 5s — so a regression costs seconds not minutes, " +
			"polls the health endpoint server-side in a single round-trip, and returns structured {running, startMillis, healthStatus, url, logTail, reason} " +
			"with a specific reason code on failure (spawn_timeout, spawn_error, health_probe_*) so the agent can diagnose without a follow-up call. " +
			"For worker services with no HTTP port (NATS/queue consumers, cron runners), pass noHttpProbe=true — the tool spawns through the same bounded-timeout path, skips the HTTP probe, and scans the post-spawn log tail for crash markers instead (missing module, broker auth failure, panic). " +
			"command runs via exec, NOT a shell — for env-var prefixes use `env KEY=VAL cmd` (the env binary handles the assignment before exec), NOT `KEY=VAL cmd` (parsed as program name). " +
			"Prefer this tool over raw Bash + ssh for every dev-server lifecycle operation.",
		InputSchema: devServerInputSchema(),
		Annotations: &mcp.ToolAnnotations{
			Title:           "Manage dev server lifecycle",
			IdempotentHint:  true,
			DestructiveHint: boolPtr(false),
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DevServerInput) (*mcp.CallToolResult, any, error) {
		result, err := ops.ExecuteDevServer(ctx, ssh, client, projectID, ops.DevServerParams{
			Action:       input.Action,
			Hostname:     input.Hostname,
			Command:      input.Command,
			Port:         input.Port,
			HealthPath:   input.HealthPath,
			LogFile:      input.LogFile,
			WaitSeconds:  input.WaitSeconds,
			LogLines:     input.LogLines,
			WorkDir:      input.WorkDir,
			ProcessMatch: input.ProcessMatch,
			NoHTTPProbe:  input.NoHTTPProbe.Bool(),
		})
		if err != nil {
			// verifyDevServerTarget may return a precondition error (target
			// not RUNNING — deploy it first); a plain PlatformError carries
			// its own Suggestion through convertError.
			return convertError(err), nil, nil
		}

		resp := &devServerToolResult{DevServerResult: result}
		// PA-1's second hook (docs/spec-workflows.md §8 O3): a successful
		// start/restart with a passing health probe just proved a live
		// listener exists, so force listener=true regardless of the
		// runtime's static deferred-start classification — a dev-mode
		// dynamic runtime has none until THIS call.
		if result.Running && isDevServerStartAction(input.Action) {
			scratch := &ops.DeployResult{}
			ensurePublicAccess(ctx, client, httpClient, projectID, stateDir, input.Hostname, scratch, true)
			resp.SubdomainURL = scratch.SubdomainURL
			resp.Warnings = scratch.Warnings
			if meta, _ := workflow.FindServiceMeta(stateDir, input.Hostname); meta != nil {
				resp.PublicAccess = string(meta.PublicAccessFor(input.Hostname).Intent)
			}
		}
		return jsonResult(resp), nil, nil
	})
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
// ops.DevServerResult itself can't carry these fields directly —
// internal/ops/** is outside this slice's write-set.
type devServerToolResult struct {
	*ops.DevServerResult
	// SubdomainURL is the L7 subdomain URL when the start-success hook
	// enabled (or found already-enabled) the route for this hostname.
	SubdomainURL string `json:"subdomainUrl,omitempty"`
	// PublicAccess is the persisted intent (auto/subdomain/domain/none)
	// after the hook ran — "" when no ServiceMeta exists for hostname.
	PublicAccess string `json:"publicAccess,omitempty"`
	// Warnings carries any non-fatal public-access anomaly from the hook
	// (e.g. an auto-enable failure) — ops.DevServerResult has no Warnings
	// field of its own.
	Warnings []string `json:"warnings,omitempty"`
}
