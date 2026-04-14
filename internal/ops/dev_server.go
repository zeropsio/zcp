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
	// failure classification the agent can dispatch on.
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
		if p.Port <= 0 || p.Port > 65535 {
			return platform.NewPlatformError(platform.ErrInvalidParameter,
				fmt.Sprintf("Invalid port %d", p.Port),
				"Pass the HTTP port the dev server listens on (e.g. 3000 for NestJS, 5173 for Vite).")
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
	for _, svc := range services {
		// ServiceStack.Name IS the hostname on Zerops — see platform.types.go.
		if svc.Name == hostname {
			return nil
		}
	}
	return platform.NewPlatformError(platform.ErrServiceNotFound,
		fmt.Sprintf("No service with hostname %q in this project", hostname),
		"Pass the hostname of a dev container that exists in the current project (e.g. apidev, appdev, workerdev).")
}

// startDevServer spawns the dev-server command in the background on the
// target container and polls the health endpoint until it returns 2xx
// or the wait window expires. The SSH shape is the key piece:
//
//	ssh HOST 'cd WORK && nohup CMD > LOG 2>&1 < /dev/null & disown'
//
// `< /dev/null` closes the child's stdin, `nohup` ignores SIGHUP from
// channel close, and `& disown` detaches the job from the SSH session's
// job table. Without all three, the ssh parent holds the channel open
// until the child exits or the bash timeout (120s) fires — the
// v11/v13/v14/v15/v16 dev-server pain. With all three, ssh returns in
// ~200ms whether or not the dev server actually starts.
//
// The follow-up curl via a SECOND ssh call then verifies readiness and
// gives us the startMs metric.
func startDevServer(ctx context.Context, ssh SSHDeployer, p DevServerParams) (*DevServerResult, error) {
	logFile := p.LogFile
	if logFile == "" {
		logFile = defaultLogFilePattern
	}
	workDir := p.WorkDir
	if workDir == "" {
		workDir = "/var/www"
	}
	wait := p.WaitSeconds
	if wait <= 0 {
		wait = defaultDevServerWait
	} else if wait > maxDevServerWait {
		wait = maxDevServerWait
	}
	healthPath := p.HealthPath
	if healthPath == "" {
		healthPath = "/"
	}

	// Clear the log file so tail-on-failure shows THIS startup, not the
	// previous one.
	_, _ = ssh.ExecSSH(ctx, p.Hostname, fmt.Sprintf("rm -f %s; true", shellQuote(logFile)))

	// Spawn the command fully detached. The shellQuote wrapper on the
	// user-provided command allows arbitrary shell (pipes, &&, env
	// overrides) to pass through, which the agent needs for things
	// like `PORT=3000 npm run start:dev` — we intentionally do NOT
	// treat Command as a single argv entry.
	spawn := fmt.Sprintf(
		"cd %s && nohup sh -c %s > %s 2>&1 < /dev/null & disown",
		shellQuote(workDir),
		shellQuote(p.Command),
		shellQuote(logFile),
	)
	if _, err := ssh.ExecSSH(ctx, p.Hostname, spawn); err != nil {
		return nil, fmt.Errorf("dev_server start: spawn: %w", err)
	}

	start := time.Now()
	// Poll the health endpoint via a SINGLE ssh invocation that tries up
	// to N times server-side. Fewer round-trips than one ssh per probe.
	// The server-side loop exits on first 2xx, returning the HTTP code
	// and elapsed millis.
	maxAttempts := (wait*1000)/pollIntervalMillis + 1
	probe := fmt.Sprintf(
		`set +e; start_ns=$(date +%%s%%N); for i in $(seq 1 %d); do `+
			`code=$(curl -s -o /dev/null -w '%%{http_code}' --max-time 2 http://localhost:%d%s 2>/dev/null); `+
			`if [ -n "$code" ] && [ "$code" -ge 200 ] 2>/dev/null && [ "$code" -lt 400 ] 2>/dev/null; then `+
			`end_ns=$(date +%%s%%N); echo "OK $code $(( (end_ns - start_ns) / 1000000 ))"; exit 0; fi; `+
			`sleep %s; done; `+
			`echo "FAIL $code"; exit 1`,
		maxAttempts, p.Port, healthPath, pollIntervalAsShellDuration(),
	)
	probeOut, probeErr := ssh.ExecSSH(ctx, p.Hostname, probe)
	elapsedMs := time.Since(start).Milliseconds()

	logTail := fetchLogTail(ctx, ssh, p.Hostname, logFile, defaultLogTailLines)

	result := &DevServerResult{
		Action:     "start",
		Hostname:   p.Hostname,
		Port:       p.Port,
		HealthPath: healthPath,
		LogFile:    logFile,
		LogTail:    logTail,
	}

	probeLine := strings.TrimSpace(string(probeOut))
	if probeErr == nil && strings.HasPrefix(probeLine, "OK ") {
		var httpCode int
		var remoteMs int64
		fields := strings.Fields(probeLine)
		if len(fields) >= 3 {
			// Ignore Sscanf errors — a malformed probe response is
			// self-reported below via the result shape; there is no
			// recovery action that would change the downstream
			// behavior on a parse failure.
			_, _ = fmt.Sscanf(fields[1], "%d", &httpCode)
			_, _ = fmt.Sscanf(fields[2], "%d", &remoteMs)
		}
		result.Running = true
		result.HealthStatus = httpCode
		// Prefer remote-measured elapsed when we have it — it's more
		// accurate than the local side which includes ssh round-trip.
		if remoteMs > 0 {
			result.StartMillis = remoteMs
		} else {
			result.StartMillis = elapsedMs
		}
		result.Message = fmt.Sprintf("Dev server on %s started and responded %d at http://localhost:%d%s in %dms.",
			p.Hostname, httpCode, p.Port, healthPath, result.StartMillis)
		return result, nil
	}

	// Failure path — read the reason from the probe output.
	result.Running = false
	switch {
	case probeLine == "":
		result.Reason = "health_probe_no_output"
	case strings.HasPrefix(probeLine, "FAIL "):
		fields := strings.Fields(probeLine)
		if len(fields) >= 2 && fields[1] != "" && fields[1] != "000" {
			result.Reason = "health_probe_http_" + fields[1]
		} else {
			result.Reason = "health_probe_connection_refused"
		}
	default:
		result.Reason = "health_probe_unknown: " + probeLine
	}
	result.Message = fmt.Sprintf(
		"Dev server on %s did not pass health probe at http://localhost:%d%s within %ds (%s). See logTail for the failing startup — if it references a missing dependency, run `npm install` over SSH first. If it references a bound port, call dev_server action=stop to free it.",
		p.Hostname, p.Port, healthPath, wait, result.Reason,
	)
	return result, nil
}

// stopDevServer kills the dev-server process and frees the port.
// Uses pkill on a caller-supplied match string and fuser on the port if
// provided. Both commands tolerate "no matching process" as success.
func stopDevServer(ctx context.Context, ssh SSHDeployer, p DevServerParams) (*DevServerResult, error) {
	match := strings.TrimSpace(p.ProcessMatch)
	if match == "" && strings.TrimSpace(p.Command) != "" {
		// Derive a reasonable default match from the command's first token.
		match = firstShellToken(p.Command)
	}

	var parts []string
	if match != "" {
		parts = append(parts, fmt.Sprintf("pkill -f %s 2>/dev/null || true", shellQuote(match)))
	}
	if p.Port > 0 && p.Port <= 65535 {
		parts = append(parts, fmt.Sprintf("fuser -k %d/tcp 2>/dev/null || true", p.Port))
	}
	if len(parts) == 0 {
		return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
			"stop requires processMatch, command, or port",
			"Pass processMatch='nest' (pkill target), or command='npm run start:dev' (first-token match), or port=3000 (fuser -k).")
	}
	parts = append(parts, "echo stopped")
	cmd := strings.Join(parts, "; ")
	_, err := ssh.ExecSSH(ctx, p.Hostname, cmd)
	result := &DevServerResult{
		Action:   "stop",
		Hostname: p.Hostname,
		Port:     p.Port,
		Running:  false,
	}
	if err != nil {
		return nil, fmt.Errorf("dev_server stop: %w", err)
	}
	result.Message = fmt.Sprintf("Dev server stopped on %s (matched %q).", p.Hostname, match)
	return result, nil
}

// statusDevServer probes the health endpoint and returns Running based on
// the HTTP response, without spawning anything.
func statusDevServer(ctx context.Context, ssh SSHDeployer, p DevServerParams) (*DevServerResult, error) {
	if p.Port <= 0 || p.Port > 65535 {
		return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
			"status requires a port",
			"Pass the HTTP port the dev server listens on (e.g. 3000, 5173).")
	}
	healthPath := p.HealthPath
	if healthPath == "" {
		healthPath = "/"
	}
	probe := fmt.Sprintf(
		"curl -s -o /dev/null -w '%%{http_code}' --max-time 2 http://localhost:%d%s",
		p.Port, healthPath,
	)
	out, _ := ssh.ExecSSH(ctx, p.Hostname, probe)
	code := strings.TrimSpace(string(out))
	result := &DevServerResult{
		Action:     "status",
		Hostname:   p.Hostname,
		Port:       p.Port,
		HealthPath: healthPath,
	}
	if code == "" || code == "000" {
		result.Running = false
		result.Reason = "connection_refused"
		result.Message = fmt.Sprintf("No HTTP response on %s:%d — dev server is not listening.", p.Hostname, p.Port)
		return result, nil
	}
	var httpCode int
	_, _ = fmt.Sscanf(code, "%d", &httpCode)
	result.HealthStatus = httpCode
	result.Running = httpCode >= 200 && httpCode < 500
	if result.Running {
		result.Message = fmt.Sprintf("Dev server on %s:%d responding (HTTP %d).", p.Hostname, p.Port, httpCode)
	} else {
		result.Reason = fmt.Sprintf("http_%d", httpCode)
		result.Message = fmt.Sprintf("Dev server on %s:%d returned HTTP %d.", p.Hostname, p.Port, httpCode)
	}
	return result, nil
}

// logsDevServer tails the dev-server log file.
func logsDevServer(ctx context.Context, ssh SSHDeployer, p DevServerParams) (*DevServerResult, error) {
	logFile := p.LogFile
	if logFile == "" {
		logFile = defaultLogFilePattern
	}
	lines := p.LogLines
	if lines <= 0 {
		lines = defaultLogTailLines
	}
	if lines > 500 {
		lines = 500
	}
	tail := fetchLogTail(ctx, ssh, p.Hostname, logFile, lines)
	result := &DevServerResult{
		Action:   "logs",
		Hostname: p.Hostname,
		LogFile:  logFile,
		LogTail:  tail,
		Message:  fmt.Sprintf("Tailing last %d lines of %s on %s.", lines, logFile, p.Hostname),
	}
	return result, nil
}

// restartDevServer is stop+start composed, sharing the same params.
// The stop call tolerates "nothing to stop" so this is safe to call on
// a fresh container.
func restartDevServer(ctx context.Context, ssh SSHDeployer, p DevServerParams) (*DevServerResult, error) {
	stopParams := p
	stopParams.Action = devServerActionStop
	if _, err := stopDevServer(ctx, ssh, stopParams); err != nil {
		return nil, fmt.Errorf("dev_server restart: stop phase: %w", err)
	}
	startParams := p
	startParams.Action = devServerActionStart
	result, err := startDevServer(ctx, ssh, startParams)
	if result != nil {
		result.Action = devServerActionRestart
	}
	return result, err
}

// fetchLogTail reads the last N lines of the dev-server log file.
// Returns empty string when the file does not exist (fresh container,
// misconfigured log path) so the caller shows a blank tail rather than
// a confusing error — the dev_server action is not about log errors.
func fetchLogTail(ctx context.Context, ssh SSHDeployer, hostname, logFile string, lines int) string {
	if logFile == "" {
		return ""
	}
	cmd := fmt.Sprintf("tail -n %d %s 2>/dev/null || true", lines, shellQuote(logFile))
	out, _ := ssh.ExecSSH(ctx, hostname, cmd)
	return strings.TrimRight(string(out), "\n")
}

// firstShellToken returns the first whitespace-separated token of a
// shell command, stripped of any leading env assignments like
// PORT=3000 FOO=bar npm run start:dev -> npm.
func firstShellToken(cmd string) string {
	for tok := range strings.FieldsSeq(cmd) {
		if strings.Contains(tok, "=") && !strings.ContainsAny(tok, "/.") {
			continue
		}
		return tok
	}
	return ""
}

// pollIntervalAsShellDuration returns the pollIntervalMillis as a shell
// sleep argument. sh's sleep accepts fractional seconds on most targets
// (bash, busybox ≥1.30); we emit seconds with three decimal places so
// 500ms becomes "0.500".
func pollIntervalAsShellDuration() string {
	return fmt.Sprintf("%d.%03d", pollIntervalMillis/1000, pollIntervalMillis%1000)
}
