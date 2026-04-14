package ops

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// startDevServer launches the dev-server command on the target container
// in a fully detached process and waits for the health endpoint to pass.
//
// The implementation is split into three bounded phases:
//
//  1. SPAWN  — a single ExecSSHBackground call with an 8s budget that
//     clears the log file, cd's into the working directory, launches
//     the user command under `setsid` with redirected stdio, and echoes
//     an ack marker before `exit 0`. The background ssh variant uses
//     `-T -n` so the client closes stdin immediately and never allocates
//     a pty; setsid moves the child into its own session/pgroup so
//     sshd can close the channel the instant the outer shell exits.
//     A spawn that exceeds 8s is returned as a structured result with
//     Reason="spawn_timeout" and a clear next-action message — never as
//     a raw error — so the agent can diagnose without a follow-up call.
//
//  2. PROBE  — a single foreground ExecSSH call whose context deadline
//     is waitSeconds + slack. The remote side runs `seq 1..N` with
//     curl -w '%{http_code}' and exits on first 2xx, returning
//     "OK <code> <millis>". On failure it echoes "FAIL <code>" and
//     exits 1. One round-trip regardless of how many polls happen.
//
//  3. TAIL   — a short-budget ExecSSH call to read the last N lines of
//     the log file, so every result (success or failure) carries enough
//     context for the agent to diagnose without another tool call.
//
// The v17 run hung for 300s on the previous `nohup CMD > LOG 2>&1 <
// /dev/null & disown` pattern. Root causes: the remote shell did not
// fully detach from the ssh channel (possibly stale pty/stdin handling,
// possibly `disown` no-oping under non-interactive bash job control).
// The new shape removes every ambiguity: no pty on the client, no stdin
// on the client, setsid on the remote, explicit `exit 0` at the end.
// A bounded per-step timeout ensures any future regression costs 8s,
// not 300s.
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

	result := &DevServerResult{
		Action:     "start",
		Hostname:   p.Hostname,
		Port:       p.Port,
		HealthPath: healthPath,
		LogFile:    logFile,
	}

	spawnOut, spawnErr := spawnDevProcess(ctx, ssh, p.Hostname, p.Command, workDir, logFile)
	spawnAckSeen := strings.Contains(string(spawnOut), spawnAckMarker)

	if spawnErr != nil {
		result.LogTail = fetchLogTailBounded(ctx, ssh, p.Hostname, logFile, defaultLogTailLines)
		result.Running = false
		if platform.IsSpawnTimeout(spawnErr) {
			result.Reason = "spawn_timeout"
			result.Message = fmt.Sprintf(
				"Dev server on %s did not detach within %s — the remote shell held the ssh channel open. "+
					"This usually means the dev-command hangs before backgrounding (broken package install, missing binary) "+
					"OR the container's shell does not honor setsid+redirect detach. "+
					"Read logTail for the last output, then try: `ssh %s \"cd %s && %s\"` interactively to see what the command does on its own.",
				p.Hostname, spawnTimeout, p.Hostname, workDir, p.Command,
			)
		} else {
			result.Reason = "spawn_error"
			result.Message = fmt.Sprintf(
				"Dev server on %s failed to spawn: %s. "+
					"The ssh call returned non-zero before the remote shell reached the detach step. "+
					"Typical causes: authentication (key not loaded), wrong hostname, container not running, or the workDir %q does not exist. "+
					"Verify with `ssh %s \"pwd && id\"` and retry.",
				p.Hostname, spawnErr.Error(), workDir, p.Hostname,
			)
		}
		return result, nil
	}

	// PHASE 2 — health probe.
	probeOut, elapsedMs, probeDeadline, probeErr := runHealthProbe(ctx, ssh, p.Hostname, p.Port, healthPath, wait)

	// PHASE 3 — log tail. Always fetch, success or failure.
	result.LogTail = fetchLogTailBounded(ctx, ssh, p.Hostname, logFile, defaultLogTailLines)

	probeLine := strings.TrimSpace(string(probeOut))
	if probeErr == nil && strings.HasPrefix(probeLine, "OK ") {
		applyProbeSuccess(result, probeLine, elapsedMs, p, healthPath, spawnAckSeen)
		return result, nil
	}

	applyProbeFailure(result, probeLine, probeDeadline, p, healthPath, wait)
	return result, nil
}

// spawnDevProcess runs the background-ssh spawn script. Split out of
// startDevServer so the phase-1 shape (script construction, detach
// semantics, tight budget) lives in one spot the test can pin against.
//
// The remote script is written as a multi-line program for clarity:
//
//	set -e                 — fail fast if setup lines break
//	rm -f LOG || true      — clear the log so tail shows THIS startup
//	cd WORK                — enter workDir BEFORE backgrounding
//	                         (cd after `&` would race the child)
//	setsid sh -c CMD > ... — background CHILD with fully redirected stdio
//	                         AND new session/pgroup. Redirects bind to
//	                         `setsid sh -c CMD` (everything before `&`),
//	                         so bash's fork applies them BEFORE exec and
//	                         the child never shares ssh's fds.
//	echo ack pid=$!        — print ack marker + child pid. `$!` refers
//	                         to the most-recent bg job in the outer shell.
//	exit 0                 — force outer shell exit so sshd closes the
//	                         channel immediately.
func spawnDevProcess(ctx context.Context, ssh SSHDeployer, hostname, command, workDir, logFile string) ([]byte, error) {
	script := fmt.Sprintf(
		"set -e; "+
			"rm -f %s 2>/dev/null || true; "+
			"cd %s; "+
			"setsid sh -c %s > %s 2>&1 < /dev/null & "+
			"echo \"%s pid=$!\"; "+
			"exit 0",
		shellQuote(logFile),
		shellQuote(workDir),
		shellQuote(command),
		shellQuote(logFile),
		spawnAckMarker,
	)
	return ssh.ExecSSHBackground(ctx, hostname, script, spawnTimeout)
}

// runHealthProbe polls the health endpoint server-side in a single SSH
// call. Returns probe output, locally-measured elapsed, a deadline-hit
// flag, and the ssh error (convention: error last).
func runHealthProbe(parent context.Context, ssh SSHDeployer, hostname string, port int, healthPath string, wait int) ([]byte, int64, bool, error) {
	maxAttempts := (wait*1000)/pollIntervalMillis + 1
	probe := fmt.Sprintf(
		`set +e; start_ns=$(date +%%s%%N); for i in $(seq 1 %d); do `+
			`code=$(curl -s -o /dev/null -w '%%{http_code}' --max-time 2 http://localhost:%d%s 2>/dev/null); `+
			`if [ -n "$code" ] && [ "$code" -ge 200 ] 2>/dev/null && [ "$code" -lt 400 ] 2>/dev/null; then `+
			`end_ns=$(date +%%s%%N); echo "OK $code $(( (end_ns - start_ns) / 1000000 ))"; exit 0; fi; `+
			`sleep %s; done; `+
			`echo "FAIL $code"; exit 1`,
		maxAttempts, port, healthPath, pollIntervalAsShellDuration(),
	)

	probeCtx, cancel := context.WithTimeout(parent, time.Duration(wait)*time.Second+probeTimeoutSlack)
	defer cancel()
	start := time.Now()
	out, err := ssh.ExecSSH(probeCtx, hostname, probe)
	elapsed := time.Since(start).Milliseconds()
	return out, elapsed, probeCtx.Err() == context.DeadlineExceeded, err
}

// applyProbeSuccess fills in result fields for a successful probe.
func applyProbeSuccess(result *DevServerResult, probeLine string, elapsedMs int64, p DevServerParams, healthPath string, spawnAckSeen bool) {
	var httpCode int
	var remoteMs int64
	fields := strings.Fields(probeLine)
	if len(fields) >= 3 {
		// Ignore Sscanf errors — a malformed probe response is self-
		// reported via the result shape; there is no recovery action.
		_, _ = fmt.Sscanf(fields[1], "%d", &httpCode)
		_, _ = fmt.Sscanf(fields[2], "%d", &remoteMs)
	}
	result.Running = true
	result.HealthStatus = httpCode
	// Prefer remote-measured elapsed when available — more accurate than
	// the local side which includes ssh round-trip.
	if remoteMs > 0 {
		result.StartMillis = remoteMs
	} else {
		result.StartMillis = elapsedMs
	}
	msg := fmt.Sprintf("Dev server on %s started and responded %d at http://localhost:%d%s in %dms.",
		p.Hostname, httpCode, p.Port, healthPath, result.StartMillis)
	if !spawnAckSeen {
		// Diagnostic breadcrumb: probe succeeded so the dev server IS
		// running, but spawn output didn't include our ack marker.
		// Usually means a noisy .profile / .bashrc on the target ate
		// stdout before `echo` ran. Not an error — surface for review.
		msg += " (Note: spawn ack marker absent — the outer shell's .profile may be swallowing stdout.)"
	}
	result.Message = msg
}

// applyProbeFailure classifies probe failure into a Reason code.
func applyProbeFailure(result *DevServerResult, probeLine string, deadlineHit bool, p DevServerParams, healthPath string, wait int) {
	result.Running = false
	switch {
	case deadlineHit:
		result.Reason = "health_probe_timeout"
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
}
