package ops

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

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

// fetchLogTailBounded is fetchLogTail with a short context deadline so
// the failure-path log tail never stalls the tool call.
func fetchLogTailBounded(parent context.Context, ssh SSHDeployer, hostname, logFile string, lines int) string {
	ctx, cancel := context.WithTimeout(parent, logTailTimeout)
	defer cancel()
	return fetchLogTail(ctx, ssh, hostname, logFile, lines)
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
