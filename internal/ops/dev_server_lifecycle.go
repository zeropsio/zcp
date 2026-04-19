package ops

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

const (
	// portFreePollMS is the poll cadence of waitForPortFree. ss probes
	// over ssh take ~50-150ms each on a healthy container; 200ms gives
	// margin without burning probe budget.
	portFreePollMS = 200
	// portFreeWaitTotalMS bounds the post-stop port-free poll. Calibrated
	// against measured SO_REUSEADDR linger on the Zerops dev container
	// (~100-300ms typical, ~1.2s pathological worst-case in a v21 trace).
	portFreeWaitTotalMS = 1500
	// portFreeWaitEscalationMS bounds the post-SIGKILL re-poll. After a
	// hard kill the listener should be released within one OS reaper
	// cycle; 800ms covers that without inflating the stop call.
	portFreeWaitEscalationMS = 800
	// reasonPortStillBound is the structured Reason value emitted when
	// a port refuses to release even after SIGKILL escalation. Tests and
	// callers pattern-match on this string.
	reasonPortStillBound = "port_still_bound"
)

// stopDevServer kills the dev-server process and frees the port.
// Uses pkill on a caller-supplied match string and fuser on the port if
// provided. Both commands tolerate "no matching process" as success.
//
// v8.96 quality fix: when a port is supplied, the stop call does NOT
// return until the port is actually free (or the wait budget exhausts).
// Prior behavior returned immediately after sending kill signals, which
// raced with the OS reaper — a subsequent start would then hit "address
// already in use" and the agent would improvise pgrep+pkill+sleep
// workarounds, sometimes shipping the workaround as a "Zerops gotcha"
// in the published README. Polling after the kill removes the race so
// the next start sees a guaranteed-free port and the agent never
// invents a workaround.
func stopDevServer(ctx context.Context, ssh SSHDeployer, p DevServerParams) (*DevServerResult, error) {
	match := strings.TrimSpace(p.ProcessMatch)
	if match == "" && strings.TrimSpace(p.Command) != "" {
		// Derive a reasonable default match from the command's first token.
		match = firstShellToken(p.Command)
	}

	var parts []string
	if match != "" {
		// --ignore-ancestors (procps ≥3.3.15) prevents pkill from killing
		// its own sh -c invocation and the SSH session wrapping it — the
		// shape that produced 6 exit-255 events in v21. The pgrep fallback
		// filters $$ / $PPID so older procps / busybox pkill (no
		// --ignore-ancestors support) still avoid the self-kill.
		quoted := shellQuote(match)
		parts = append(parts, fmt.Sprintf(
			"(pkill --ignore-ancestors -f %s 2>/dev/null "+
				"|| pgrep -f %s 2>/dev/null | grep -v -e \"^$$\" -e \"^$PPID\" | xargs -r kill 2>/dev/null) "+
				"|| true",
			quoted, quoted,
		))
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
		// Exit 255 from ssh is the distinctive signature of pkill killing
		// its own sh -c shell child / the SSH session wrapping it. The
		// stop succeeded (the process tree is gone); surface a structured
		// success instead of propagating a raw error the agent must
		// reinterpret. Verified across 7 v20/v21 dev_server stop runs:
		// every time pkill successfully killed a dev-server process tree
		// that included its own shell, SSH returned exit 255.
		if isSSHSelfKill(err) {
			result.Reason = "ssh_self_killed"
			result.Message = fmt.Sprintf(
				"Dev server stopped on %s (matched %q). SSH session dropped because pkill matched its own shell child — this is expected when the dev command's process tree overlaps the sh/ssh session.",
				p.Hostname, match,
			)
			// Fall through to the post-kill port-free wait — the kill
			// landed, but the OS may still hold the listener for a
			// fraction of a second.
		} else {
			return nil, fmt.Errorf("dev_server stop: %w", err)
		}
	}

	// Post-kill verification: when the caller named a port, wait for it
	// to actually be free before returning success. SO_REUSEADDR sockets
	// can linger ~100-300ms after the listener PID dies; on a busy
	// container we have measured up to ~1.2s. The poll budget below
	// covers the realistic worst case while still failing loudly if the
	// kill didn't take.
	if p.Port > 0 && p.Port <= 65535 {
		freed, lingerMS, _ := waitForPortFree(ctx, ssh, p.Hostname, p.Port, portFreeWaitTotalMS, portFreePollMS)
		if !freed {
			// Escalate: hard SIGKILL anything still on the port, then
			// give it one more poll cycle. If it STILL won't release,
			// surface a structured failure so the caller / agent can
			// decide whether to retry — never silently claim success.
			killCmd := fmt.Sprintf("fuser -k -KILL %d/tcp 2>/dev/null || true", p.Port)
			_, _ = ssh.ExecSSH(ctx, p.Hostname, killCmd)
			var lastPIDs string
			freed, lingerMS, lastPIDs = waitForPortFree(ctx, ssh, p.Hostname, p.Port, portFreeWaitEscalationMS, portFreePollMS)
			if !freed {
				result.Reason = reasonPortStillBound
				detail := ""
				if strings.TrimSpace(lastPIDs) != "" {
					detail = " (PIDs still holding the port: " + strings.TrimSpace(lastPIDs) + ")"
				}
				result.Message = fmt.Sprintf(
					"Dev server stop on %s sent kill signals (matched %q, fuser -k on port %d, then SIGKILL escalation), but port %d is still bound after %dms%s. Investigate with `ssh %s \"ss -tnlp | grep :%d\"` before re-starting; do NOT add a manual pkill workaround to the recipe — port-stop is the platform's responsibility, not the recipe's.",
					p.Hostname, match, p.Port, p.Port,
					portFreeWaitTotalMS+portFreeWaitEscalationMS, detail, p.Hostname, p.Port,
				)
				return result, nil
			}
		}
		result.Message = fmt.Sprintf(
			"Dev server stopped on %s (matched %q). Port %d is free (verified after %dms).",
			p.Hostname, match, p.Port, lingerMS,
		)
		return result, nil
	}

	if result.Reason == "ssh_self_killed" {
		// Caller didn't supply a port; the legacy ssh-self-killed path
		// stays responsible for its own narrative when we can't verify.
		return result, nil
	}
	result.Message = fmt.Sprintf("Dev server stopped on %s (matched %q).", p.Hostname, match)
	return result, nil
}

// waitForPortFree polls the target container's TCP listener table for
// `port` until either nothing is bound or the budget expires. Returns
// (freed, elapsedMS, lastListenerPIDs). lastListenerPIDs is whatever
// `ss -tnlp` reported on the final probe — used to populate the
// failure-path message so the agent can investigate without inventing
// a workaround.
//
// Probe shape: `ss -tnlp 'sport = :PORT'` exits 0 either way; the
// presence of a non-empty result line indicates a listener. We grep
// for `pid=` to narrow to the bound socket, then collect those tokens.
func waitForPortFree(ctx context.Context, ssh SSHDeployer, hostname string, port, totalMS, pollMS int) (bool, int, string) {
	if pollMS <= 0 {
		pollMS = portFreePollMS
	}
	probe := fmt.Sprintf(
		"ss -tnlp 'sport = :%d' 2>/dev/null | tail -n +2",
		port,
	)
	elapsed := 0
	var last string
	for elapsed <= totalMS {
		out, _ := ssh.ExecSSH(ctx, hostname, probe)
		last = strings.TrimSpace(string(out))
		if last == "" {
			return true, elapsed, ""
		}
		if elapsed == totalMS {
			break
		}
		sleep := pollMS
		if elapsed+sleep > totalMS {
			sleep = totalMS - elapsed
		}
		// Sleep on the remote side via ssh would double-charge the
		// network round-trip; sleep here instead. The poll cadence is
		// already tuned so cumulative ssh probe latency stays under
		// the budget.
		select {
		case <-ctx.Done():
			return false, elapsed, last
		case <-time.After(time.Duration(sleep) * time.Millisecond):
		}
		elapsed += sleep
	}
	return false, elapsed, last
}

// isSSHSelfKill returns true when the underlying error is SSH exit 255,
// the distinctive signature of pkill killing its own ssh session. A
// dev_server stop action that matches on a process name appearing in
// its own shell invocation triggers this; the process is gone, the SSH
// channel is just dropped.
func isSSHSelfKill(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "exit status 255")
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
