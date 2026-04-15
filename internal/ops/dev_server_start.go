package ops

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
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
			result.Reason = reasonSpawnTimeout
			result.Message = fmt.Sprintf(
				"Dev server on %s did not detach within %s — the remote shell held the ssh channel open. "+
					"This usually means the dev-command hangs before backgrounding (broken package install, missing binary) "+
					"OR the container's shell does not honor setsid+redirect detach. "+
					"Read logTail for the last output, then try: `ssh %s \"cd %s && %s\"` interactively to see what the command does on its own.",
				p.Hostname, spawnTimeout, p.Hostname, workDir, p.Command,
			)
		} else {
			result.Reason = reasonSpawnError
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

	// No-probe branch: worker services without an HTTP surface. After a
	// successful spawn we pause briefly so the process has time to either
	// start consuming or exit during init (bad import, missing dependency,
	// broker auth failure, panic, uncaught exception — any failure mode
	// that makes the process exit). Then we check process liveness via
	// `kill -0` on the pid recorded in the spawn pidfile — a framework-
	// agnostic POSIX signal, not a pattern match on log content. A live
	// process → Running=true with a Message that tells the agent to
	// verify consumption via zerops_logs (this tool cannot check a worker
	// is actually processing work). A dead process → Running=false with
	// Reason="post_spawn_exit".
	if p.NoHTTPProbe {
		result.Port = 0
		result.HealthPath = ""
		settle := currentNoProbeSettle()
		sleepCtx(ctx, settle)
		alive, livenessErr := checkProcessAlive(ctx, ssh, p.Hostname, pidFileFor(logFile))
		result.LogTail = fetchLogTailBounded(ctx, ssh, p.Hostname, logFile, defaultLogTailLines)
		if livenessErr != nil {
			result.Running = false
			result.Reason = reasonLivenessCheckError
			result.Message = fmt.Sprintf(
				"Dev server on %s: spawn succeeded but the liveness check against the pidfile failed: %s. "+
					"The ssh call itself errored — typically a connectivity blip or the target became unreachable. "+
					"Read logTail to see whether the process started at all, then retry.",
				p.Hostname, livenessErr.Error(),
			)
			return result, nil
		}
		if !alive {
			result.Running = false
			result.Reason = reasonPostSpawnExit
			result.Message = fmt.Sprintf(
				"Dev server on %s spawned but the process is no longer alive %s after spawn (kill -0 on the pidfile pid failed, or the pidfile was never written). "+
					"The process exited during init. Read logTail for the stack / error that caused the exit and fix the root cause; do NOT retry the spawn until the cause is addressed.",
				p.Hostname, settle,
			)
			return result, nil
		}
		result.Running = true
		result.Message = fmt.Sprintf(
			"Dev server on %s started in no-probe mode (skipping HTTP probe — service has no HTTP surface). "+
				"After a %s settle interval the spawned process is still alive (kill -0 on the pidfile pid returned success). "+
				"This tool CANNOT verify the worker is actually consuming messages / processing work — follow up with `zerops_logs serviceHostname=%q` to confirm the subscription / job loop is making progress.",
			p.Hostname, settle, p.Hostname,
		)
		if !spawnAckSeen {
			result.Message += " (Note: spawn ack marker absent — the outer shell's .profile may be swallowing stdout.)"
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

// defaultNoProbeSettle bounds the pause between a successful spawn and
// the post-spawn liveness check in no-probe mode. 3s is long enough for
// typical init-phase work to either complete successfully or cause the
// process to exit — a process that survives 3s of init is almost
// certainly past its boot-time failure window. Kept short because the
// tool itself cannot prove the process is doing useful work; longer
// settle intervals just make the tool less responsive without improving
// signal.
const defaultNoProbeSettle = 3 * time.Second

// livenessCheckTimeout bounds the ssh call that reads the pidfile and
// runs kill -0. One round-trip, no retries, so the budget needs only
// enough for an ssh handshake and a trivial shell command.
const livenessCheckTimeout = 5 * time.Second

// noProbeSettleNanos holds the current settle interval in nanoseconds.
// Backed by atomic.Int64 so test helpers can override without data-racing
// the parallel-test readers in startDevServer. Accessed via
// currentNoProbeSettle / storeNoProbeSettle; tests use the exported
// helper in dev_server_test.go.
var noProbeSettleNanos atomic.Int64

func init() {
	noProbeSettleNanos.Store(int64(defaultNoProbeSettle))
}

func currentNoProbeSettle() time.Duration {
	return time.Duration(noProbeSettleNanos.Load())
}

func storeNoProbeSettle(d time.Duration) time.Duration {
	return time.Duration(noProbeSettleNanos.Swap(int64(d)))
}

// sleepCtx sleeps for d or until ctx is cancelled, whichever comes first.
func sleepCtx(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}

// spawnDevProcess runs the background-ssh spawn script. Split out of
// startDevServer so the phase-1 shape (script construction, detach
// semantics, tight budget) lives in one spot the test can pin against.
//
// The remote script is written as a multi-line program for clarity:
//
//	set -e                 — fail fast if setup lines break
//	rm -f LOG PIDFILE      — clear log and pidfile so later reads see
//	                         only THIS startup's state.
//	cd WORK                — enter workDir BEFORE backgrounding
//	                         (cd after `&` would race the child)
//	setsid sh -c           — fork into a new session/pgroup and run the
//	  'echo $$ > PID;      — inner shell: write its OWN PID to the
//	   exec CMD'            pidfile (exec below preserves this PID for
//	                         the real command), then exec CMD which
//	                         replaces the shell in-place.
//	> LOG 2>&1 < /dev/null — stdio redirects bind to `setsid sh -c ...`,
//	                         so the shell (and the exec'd CMD) never
//	                         share ssh's fds.
//	&                      — background the whole pipeline.
//	echo ack pid=$!        — print ack marker to the outer ssh channel.
//	                         `$!` here is the outer setsid parent; the
//	                         authoritative long-lived PID is the one
//	                         inside PIDFILE (written before exec).
//	exit 0                 — force outer shell exit so sshd closes the
//	                         channel immediately.
//
// The pidfile is the structural hook for post-spawn liveness checks in
// no-probe mode: we read PIDFILE and run `kill -0 <pid>` to decide
// whether the process is still alive, instead of pattern-matching on
// log content for runtime-specific crash strings.
func spawnDevProcess(ctx context.Context, ssh SSHDeployer, hostname, command, workDir, logFile string) ([]byte, error) {
	pidFile := pidFileFor(logFile)
	// Inner shell script written $$>PIDFILE; exec CMD. Single-quoted so
	// $$ and CMD are evaluated by the inner shell, not the outer one.
	inner := fmt.Sprintf("echo $$ > %s; exec %s", shellQuote(pidFile), command)
	script := fmt.Sprintf(
		"set -e; "+
			"rm -f %s %s 2>/dev/null || true; "+
			"cd %s; "+
			"setsid sh -c %s > %s 2>&1 < /dev/null & "+
			"echo \"%s pid=$!\"; "+
			"exit 0",
		shellQuote(logFile),
		shellQuote(pidFile),
		shellQuote(workDir),
		shellQuote(inner),
		shellQuote(logFile),
		spawnAckMarker,
	)
	return ssh.ExecSSHBackground(ctx, hostname, script, spawnTimeout)
}

// pidFileFor derives a pidfile path from the log file path by appending
// ".pid". Keeps the pair co-located so a single `rm` call clears both
// at spawn time.
func pidFileFor(logFile string) string {
	return logFile + ".pid"
}

// checkProcessAlive reads the pidfile on the remote host and runs
// `kill -0 <pid>` to decide whether the spawned process is still
// running. Returns alive=true if the process exists, alive=false if
// the pidfile is missing, empty, malformed, or the pid is no longer
// alive. The error return is reserved for transport failures (ssh
// call itself errored) — those propagate to the caller so the no-probe
// branch can surface them as spawn diagnostics.
//
// This is the framework-agnostic replacement for log-tail string
// matching: liveness is a universal POSIX signal, not a per-runtime
// error string. A process that exited on init (bad import, missing
// dependency, broker auth failure, panic, uncaught exception, etc.)
// is dead by kill(2) semantics regardless of which runtime produced
// it, and a process that's alive is alive regardless of what warnings
// it printed to the log.
func checkProcessAlive(ctx context.Context, ssh SSHDeployer, hostname, pidFile string) (bool, error) {
	probeCtx, cancel := context.WithTimeout(ctx, livenessCheckTimeout)
	defer cancel()
	cmd := fmt.Sprintf(
		`pid=$(cat %s 2>/dev/null); `+
			`if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then echo alive; else echo dead; fi`,
		shellQuote(pidFile),
	)
	out, err := ssh.ExecSSH(probeCtx, hostname, cmd)
	if err != nil {
		return false, err
	}
	return strings.Contains(string(out), "alive"), nil
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
