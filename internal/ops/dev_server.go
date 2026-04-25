package ops

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// DevServerResult is the standardized response from dev_server actions.
// All actions return the same shape so the MCP schema stays single.
type DevServerResult struct {
	Action   string `json:"action"`
	Hostname string `json:"hostname"`
	Running  bool   `json:"running"`
	// Port is the health-check port the tool probed or would probe. Zero
	// when the action does not involve a port (stop, logs).
	Port int `json:"port,omitempty"`
	// HealthPath is the HTTP path that was probed (start, restart). Empty
	// when no probe was run.
	HealthPath string `json:"healthPath,omitempty"`
	// HealthStatus is the HTTP status code returned by the health probe,
	// or 0 when no probe was run or it never completed.
	HealthStatus int `json:"healthStatus,omitempty"`
	// StartMillis is the time between kicking off the process and the
	// health probe returning 2xx (start, restart). Zero on failure.
	StartMillis int64 `json:"startMillis,omitempty"`
	// LogTail is the last N lines of the dev-server log file. Always
	// populated when the log file exists on the target, whether the
	// action succeeded or not — gives the agent something to read on
	// failure without a follow-up call.
	LogTail string `json:"logTail,omitempty"`
	// LogFile is the absolute path to the log file on the target
	// container, so the agent can tail it further if needed.
	LogFile string `json:"logFile,omitempty"`
	// Message is a one-line human summary of what happened.
	Message string `json:"message"`
	// Reason is set when Running=false on a start/restart — concrete
	// failure classification the agent can dispatch on. Known values:
	//   spawn_timeout             — background spawn exceeded the 8s
	//                               budget (remote shell did not detach)
	//   spawn_error               — ssh returned non-zero before the
	//                               remote shell reached the detach step
	//   health_probe_timeout      — probe context deadline exceeded
	//   health_probe_no_output    — probe returned nothing (malformed)
	//   health_probe_connection_refused  — curl got no response
	//   health_probe_http_<code>  — server returned non-ready status
	//   health_probe_unknown: <…> — unclassified; includes raw probe line
	//   post_spawn_exit           — no-probe mode: spawn succeeded but
	//                               `kill -0 <pid>` on the pidfile pid
	//                               returned non-zero after the settle
	//                               interval (process exited, or pidfile
	//                               never written — both treated as dead)
	//   liveness_check_error      — no-probe mode: the ssh call that
	//                               ran the liveness check itself failed
	//                               (transport error) — we cannot prove
	//                               alive or dead, agent must investigate
	//   connection_refused        — status-action: no HTTP response
	//   http_<code>               — status-action: non-ready HTTP code
	Reason string `json:"reason,omitempty"`
}

// DevServerParams is the typed input for StartDevServer / StopDevServer /
// StatusDevServer / LogsDevServer / RestartDevServer. Fields not relevant
// to a given action are ignored (e.g. Command is unused by stop and logs).
type DevServerParams struct {
	Action       string
	Hostname     string
	Command      string
	Port         int
	HealthPath   string
	LogFile      string
	WaitSeconds  int
	LogLines     int
	WorkDir      string
	ProcessMatch string
	// NoHTTPProbe skips the post-spawn health probe. Set true for worker
	// services that have no HTTP port (NATS/Kafka consumers, disk-queue
	// runners, cron-style processes). With NoHTTPProbe=true, Port becomes
	// optional and the tool decides Running from the spawn ack marker plus
	// a short post-spawn log-tail crash scan (detectPostSpawnCrash), not
	// from an HTTP 2xx probe. Callers should follow up with zerops_logs to
	// confirm the worker is actually consuming — the tool cannot verify
	// liveness for a process without a readable endpoint.
	NoHTTPProbe bool
}

// DevServer action names — kept as constants so callers and tests
// reference the same identifiers.
const (
	devServerActionStart   = "start"
	devServerActionStop    = "stop"
	devServerActionStatus  = "status"
	devServerActionLogs    = "logs"
	devServerActionRestart = "restart"
)

// DevServerResult.Reason values. Declared as constants so assignments in
// ops code and assertions in tests reference the same literal — linter
// also enforces this via goconst.
const (
	reasonSpawnTimeout       = "spawn_timeout"
	reasonSpawnError         = "spawn_error"
	reasonPostSpawnExit      = "post_spawn_exit"
	reasonLivenessCheckError = "liveness_check_error"
)

const (
	// defaultDevServerWait is how long the start probe waits for a
	// successful health check before giving up. 15s is enough for a
	// typical Node/TS hot-reload boot on Zerops.
	defaultDevServerWait = 15
	// maxDevServerWait is an upper bound that keeps pathological boot
	// loops from eating the whole bash-timeout window.
	maxDevServerWait = 45
	// defaultLogTailLines is how many trailing log lines to include in
	// every result. Small enough to fit in an MCP response, large
	// enough to diagnose a failed startup.
	defaultLogTailLines = 40
	// pollIntervalMillis is the poll cadence of the start-probe loop.
	pollIntervalMillis = 500
	// defaultLogFilePattern is the path used when the caller does not
	// specify a log file. Slash-safe and stable across agents.
	defaultLogFilePattern = "/tmp/zcp-dev-server.log"
	// spawnTimeout bounds the background-ssh call that kicks off the
	// dev-server process. A correct detach (setsid + stdio redirect +
	// `-T -n` ssh flags) returns in well under a second; 8s gives us
	// headroom for a slow handshake on a cold container without eating
	// any of the probe budget. The v17 run hung for 300s on the old
	// `nohup ... & disown` pattern — this ceiling is how we guarantee
	// that regression can never cost more than 8 seconds again.
	spawnTimeout = 8 * time.Second
	// probeTimeoutSlack is added on top of waitSeconds to bound the
	// foreground ssh call. The remote probe has its own `seq 1..N` loop
	// so slack only covers ssh dial + channel tear-down jitter.
	probeTimeoutSlack = 5 * time.Second
	// logTailTimeout bounds the log-tail ssh call on the failure path.
	// Fetching diagnostic logs must never dwarf the spawn budget.
	logTailTimeout = 5 * time.Second
	// spawnAckMarker is the stdout sentinel the outer remote shell prints
	// right before its `exit 0`. Its presence in the spawn output proves:
	// (1) the ssh channel reached the remote shell, (2) the `setsid ... &`
	// line executed without error, and (3) the outer shell ran through to
	// the final echo — which in turn means the backgrounded child's stdio
	// redirects took effect (otherwise the outer shell would have been
	// blocked by the child holding the ssh channel's fds open). A missing
	// marker is a strong signal that the spawn shape broke somewhere on
	// the remote side.
	spawnAckMarker = "zcp-dev-server-spawned"
)

var (
	// hostnameRe bounds the dev-server hostname to the same shape as
	// Zerops container hostnames. Prevents shell injection through
	// a crafted hostname that would end up inside `ssh HOST ...`.
	devHostnameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	// logPathRe bounds the log-file path to an absolute POSIX path
	// with no shell metacharacters. The tool always double-quotes
	// the path in shell commands, but we also pre-filter for safety.
	devLogPathRe = regexp.MustCompile(`^/[A-Za-z0-9._/-]{1,256}$`)
)

// ExecuteDevServer is the single entry point for all dev_server actions.
// Dispatching inside the ops package (instead of five separate exported
// functions) lets the tool handler stay a one-liner and keeps the SSH
// shape decisions centralized.
func ExecuteDevServer(
	ctx context.Context,
	ssh SSHDeployer,
	client platform.Client,
	projectID string,
	p DevServerParams,
) (*DevServerResult, error) {
	if err := validateDevServerParams(p); err != nil {
		return nil, err
	}
	if err := verifyDevServerTarget(ctx, client, projectID, p.Hostname); err != nil {
		return nil, err
	}
	switch strings.ToLower(p.Action) {
	case devServerActionStart:
		return startDevServer(ctx, ssh, p)
	case devServerActionStop:
		return stopDevServer(ctx, ssh, p)
	case devServerActionStatus:
		return statusDevServer(ctx, ssh, p)
	case devServerActionLogs:
		return logsDevServer(ctx, ssh, p)
	case devServerActionRestart:
		return restartDevServer(ctx, ssh, p)
	default:
		return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("Unknown dev_server action %q", p.Action),
			"Use one of: start, stop, status, logs, restart")
	}
}

func validateDevServerParams(p DevServerParams) error {
	if !devHostnameRe.MatchString(p.Hostname) {
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("Invalid hostname %q", p.Hostname),
			"Hostname must match Zerops container conventions: lowercase alphanumeric with dashes, starting with a letter, up to 63 chars.")
	}
	if p.LogFile != "" && !devLogPathRe.MatchString(p.LogFile) {
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("Invalid log file path %q", p.LogFile),
			"Log file must be an absolute POSIX path containing only letters, digits, dots, dashes, underscores, and slashes.")
	}
	if p.WorkDir != "" && !devLogPathRe.MatchString(p.WorkDir) {
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("Invalid working directory %q", p.WorkDir),
			"workDir must be an absolute POSIX path (e.g. /var/www).")
	}
	action := strings.ToLower(p.Action)
	if action == devServerActionStart || action == devServerActionRestart {
		if strings.TrimSpace(p.Command) == "" {
			return platform.NewPlatformError(platform.ErrInvalidParameter,
				"Missing dev server command",
				"Pass the exact shell command that starts the dev server, e.g. 'npm run start:dev' or 'vite --host 0.0.0.0'.")
		}
		if !p.NoHTTPProbe {
			if p.Port <= 0 || p.Port > 65535 {
				return platform.NewPlatformError(platform.ErrInvalidParameter,
					fmt.Sprintf("Invalid port %d", p.Port),
					"Pass the HTTP port the dev server listens on (e.g. 3000 for NestJS, 5173 for Vite). For services with no HTTP surface (NATS/queue workers, cron runners), set noHttpProbe=true to skip the health probe entirely.")
			}
		} else if p.Port < 0 || p.Port > 65535 {
			return platform.NewPlatformError(platform.ErrInvalidParameter,
				fmt.Sprintf("Invalid port %d", p.Port),
				"Port must be in 0..65535. Use 0 when noHttpProbe=true (the tool ignores it).")
		}
	}
	return nil
}

// verifyDevServerTarget confirms the hostname resolves to an actual
// service in the current project — catches typos early with a clear
// error instead of an opaque SSH failure.
func verifyDevServerTarget(ctx context.Context, client platform.Client, projectID, hostname string) error {
	if client == nil {
		// Unit tests can pass a nil client when only the SSH shape matters.
		return nil
	}
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return err
	}
	if _, lookupErr := FindService(services, hostname); lookupErr != nil {
		return platform.NewPlatformError(platform.ErrServiceNotFound,
			fmt.Sprintf("No service with hostname %q in this project", hostname),
			"Pass the hostname of a dev container that exists in the current project (e.g. apidev, appdev, workerdev).")
	}
	return nil
}
