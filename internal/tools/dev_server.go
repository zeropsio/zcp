package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// DevServerInput is the input type for zerops_dev_server.
type DevServerInput struct {
	Action       string `json:"action"                 jsonschema:"Action to perform: start, stop, status, logs, restart. start spawns the dev-server command in the background and waits for the health endpoint to return 2xx. stop kills matching processes and frees the port. status probes the health endpoint without spawning anything. logs tails the dev-server log file. restart is stop+start."`
	Hostname     string `json:"hostname"               jsonschema:"Target dev-container hostname (e.g. apidev, appdev, workerdev). Must exist in the current project."`
	Command      string `json:"command,omitempty"      jsonschema:"Shell command that starts the dev server. Required for start and restart. Example: 'npm run start:dev', 'vite --host 0.0.0.0', 'PORT=3000 npm run dev'. Env assignments and pipes are supported. Unused by stop/status/logs."`
	Port         int    `json:"port,omitempty"         jsonschema:"HTTP port the dev server listens on. Required for start/restart/status. Optional for stop (if set, fuser -k the port). Example: 3000 for NestJS, 5173 for Vite."`
	HealthPath   string `json:"healthPath,omitempty"   jsonschema:"HTTP path to probe for readiness. Defaults to '/' if omitted. Example: '/api/health', '/health', '/'. Must return 2xx or 3xx to count as ready."`
	LogFile      string `json:"logFile,omitempty"      jsonschema:"Absolute path to the log file on the target container. Defaults to /tmp/zcp-dev-server.log. Used by start/restart (written to), logs (read from). Reusing the same default across calls keeps the log stable between start and later tail operations."`
	WaitSeconds  int    `json:"waitSeconds,omitempty"  jsonschema:"How long to wait for the health probe to succeed on start/restart. Default 15, max 45. Short enough to fit comfortably inside the 120s bash timeout with margin for two retries."`
	LogLines     int    `json:"logLines,omitempty"     jsonschema:"Number of log lines to tail for the logs action. Default 40, max 500."`
	WorkDir      string `json:"workDir,omitempty"      jsonschema:"Absolute working directory on the target container. Defaults to /var/www. The dev-server command runs with this as cwd."`
	ProcessMatch string `json:"processMatch,omitempty" jsonschema:"pkill -f pattern for the stop action. If omitted, stop derives a match from the first token of command. Example: 'nest', 'vite', 'npm run'."`
}

// RegisterDevServer registers the zerops_dev_server tool. The tool is
// only useful when zcp has an SSH deployer wired in — local (non-
// container) installs skip registration.
func RegisterDevServer(srv *mcp.Server, client platform.Client, projectID string, ssh ops.SSHDeployer) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "zerops_dev_server",
		Description: "Start, stop, probe, tail, or restart a long-running development server on a Zerops dev container. " +
			"Replaces the hand-rolled `ssh host \"cmd &\"` + sleep + curl pattern that historically hit Bash's 120s timeout because the SSH channel stayed open on `&`-backgrounded commands. " +
			"The tool detaches the process correctly (nohup + `< /dev/null` + disown), polls the health endpoint server-side in a single SSH round-trip, and returns structured {running, startMillis, healthStatus, logTail} so the agent can diagnose failures without a follow-up call. " +
			"Prefer this tool over raw Bash + ssh for every dev-server lifecycle operation in the recipe workflow.",
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
		})
		if err != nil {
			return convertError(err), nil, nil
		}
		return jsonResult(result), nil, nil
	})
}
