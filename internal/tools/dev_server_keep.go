package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"regexp"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/workflow"
)

// The dev servers zcp keeps (docs/spec-workflows.md §8 O4). A dev container's
// run.start is a no-op keepalive and the agent starts the real dev server with
// zerops_dev_server; that process dies with its container. So zcp keeps what
// the agent last started on each dev container (workflow.KeptDevServer) and
// starts it again, the same way — same command, same working directory, so
// live reload is what it was — once the container has restarted or been
// redeployed: right after one of zcp's own deploys, and otherwise from the
// keeper, `zcp dev-server keep`, a unit on the zcp container. Stop forgets it.
// A server that dies in the container it was started in (a crash, a kill by
// hand) is left down: that is the agent's to see and fix, not to hide.

// keeperStateDirRe bounds the state dir the keeper unit's command line
// carries: the unit's command is rendered for systemd, which would expand
// `$`, `%` and `\` and split on spaces.
var keeperStateDirRe = regexp.MustCompile(`^/[A-Za-z0-9._/-]+$`)

// devServerKeeperUnit names the zcp container's keeper unit for stateDir
// (zerops@devservers-<hash>): one keeper per state directory, so a session
// working from another directory is kept by its own keeper rather than by a
// unit created for someone else's.
func devServerKeeperUnit(stateDir string) string {
	sum := sha256.Sum256([]byte(stateDir))
	return "devservers-" + hex.EncodeToString(sum[:4])
}

// devServerKeeperCommand is the keeper unit's command line. The state dir is
// passed explicitly: a unit's working directory is not the agent's.
func devServerKeeperCommand(stateDir string) string {
	return "zcp dev-server keep --state-dir " + stateDir
}

// keptDevServerNote is appended to a successful start/restart's message.
const keptDevServerNote = " zcp keeps it: when this container restarts or is redeployed, zcp starts it again with the same command, until you stop it."

// reasonKeeperRestoring is a deploy's report while the keeper is still
// starting the kept server in the deployed container.
const reasonKeeperRestoring = "keeper_restoring"

// EnsureDevServerKeeper makes sure the keeper unit runs for stateDir whenever a
// dev server is kept there — at every zcp MCP start, so a keeper lost with its
// unit comes back before anything needs it. A no-op without units (local) or
// without kept servers.
func EnsureDevServerKeeper(ctx context.Context, units ops.UnitRegistrar, stateDir string) error {
	if units == nil || stateDir == "" {
		return nil
	}
	kept, err := workflow.KeptDevServers(stateDir)
	if err != nil || len(kept) == 0 {
		return err
	}
	return ensureDevServerKeeperUnit(ctx, units, stateDir)
}

// ensureDevServerKeeperUnit registers the keeper unit for stateDir.
func ensureDevServerKeeperUnit(ctx context.Context, units ops.UnitRegistrar, stateDir string) error {
	if !keeperStateDirRe.MatchString(stateDir) {
		return fmt.Errorf("state directory %q cannot go on a unit's command line (letters, digits, dot, dash, underscore and slash only)", stateDir)
	}
	return units.EnsureUnit(ctx, devServerKeeperUnit(stateDir), devServerKeeperCommand(stateDir))
}

// keepStartedDevServer records a successful start or restart as the dev server
// zcp keeps on its hostname, and makes sure the keeper runs. container is the
// life prepareDevServerKeeping read before the start, "" when it read none. It
// reports whether the server is kept and the warnings the caller surfaces —
// never an error: the dev server already runs.
func keepStartedDevServer(ctx context.Context, ssh ops.SSHDeployer, units ops.UnitRegistrar, stateDir, container string, p ops.DevServerParams) (bool, []string) {
	// The container life the server runs in, read once more on a miss. Still
	// unreadable (unlikely: ssh just worked) means zcp could not tell a later
	// restart from a crash: it keeps nothing on this host — not this start,
	// and not an earlier one, which would come back in its place.
	if container == "" {
		var err error
		if container, err = ops.ContainerIdentity(ctx, ssh, p.Hostname); err != nil {
			container, err = ops.ContainerIdentity(ctx, ssh, p.Hostname)
		}
		if err != nil {
			_ = workflow.ForgetDevServer(stateDir, p.Hostname)
			return false, []string{fmt.Sprintf("zcp could not read %s's container to keep this dev server (%v): after a restart or redeploy, start it again yourself", p.Hostname, err)}
		}
	}
	rec := workflow.KeptDevServer{
		Hostname:    p.Hostname,
		Command:     p.Command,
		Port:        p.Port,
		HealthPath:  p.HealthPath,
		WorkDir:     p.WorkDir,
		LogFile:     p.LogFile,
		WaitSeconds: p.WaitSeconds,
		NoHTTPProbe: p.NoHTTPProbe,
		Container:   container,
		StartedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	if err := workflow.KeepDevServer(stateDir, rec); err != nil {
		return false, []string{fmt.Sprintf("zcp could not record the dev server to keep it (%v): after a restart or redeploy of %s, start it again yourself", err, p.Hostname)}
	}
	if units == nil {
		return true, nil
	}
	if err := ensureDevServerKeeperUnit(ctx, units, stateDir); err != nil {
		return true, []string{fmt.Sprintf("zcp could not start its dev-server keeper (%v): %s's dev server comes back after zcp's own deploys, but not after a restart zcp does not make", err, p.Hostname)}
	}
	return true, nil
}

// restoreKeptDevServer brings back the dev server kept on hostname when its
// container is no longer the one it was started in — restarted or redeployed —
// or retries a bring-back of this life that did not come up
// (workflow.ClaimKeptDevServer decides, at most once at a time). Nil when
// nothing is kept, the container is unreachable, or there is nothing to claim;
// nil too when the agent stopped it while this start ran — the spawned process
// is killed again. Otherwise the start's result, running or not, recorded with
// the kept server.
func restoreKeptDevServer(ctx context.Context, ssh ops.SSHDeployer, stateDir, hostname string) *ops.DevServerResult {
	if stateDir == "" || ssh == nil {
		return nil
	}
	if rec, err := workflow.KeptDevServerFor(stateDir, hostname); err != nil || rec == nil {
		return nil
	}
	container, err := ops.ContainerIdentity(ctx, ssh, hostname)
	if err != nil {
		return nil
	}
	rec, claimed, err := workflow.ClaimKeptDevServer(stateDir, hostname, container, time.Now())
	if err != nil || !claimed {
		return nil
	}
	record := func(result *ops.DevServerResult) {
		_ = workflow.RecordDevServerRestore(stateDir, hostname, container, workflow.DevServerRestore{
			At:      time.Now().UTC().Format(time.RFC3339),
			Running: result.Running,
			Reason:  result.Reason,
		})
	}

	// Something already listens on its port — started by hand, or a slow
	// first start that has come up since: never start a second copy.
	if rec.Port > 0 && !rec.NoHTTPProbe {
		if listening, lerr := ops.PortListening(ctx, ssh, hostname, rec.Port); lerr == nil && listening {
			result := &ops.DevServerResult{
				Action:   "start",
				Hostname: hostname,
				Port:     rec.Port,
				Running:  true,
				Message:  fmt.Sprintf("The dev server zcp keeps on %s (`%s`) already answers on port %d in this container; zcp left it as it is.", hostname, rec.Command, rec.Port),
			}
			record(result)
			return result
		}
	}

	result, err := ops.ExecuteDevServer(ctx, ssh, nil, "", ops.DevServerParams{
		Action:      "start",
		Hostname:    rec.Hostname,
		Command:     rec.Command,
		Port:        rec.Port,
		HealthPath:  rec.HealthPath,
		LogFile:     rec.LogFile,
		WaitSeconds: rec.WaitSeconds,
		WorkDir:     rec.WorkDir,
		NoHTTPProbe: rec.NoHTTPProbe,
	})
	if err != nil {
		result = &ops.DevServerResult{
			Action:   "start",
			Hostname: hostname,
			Reason:   "invalid_kept_start",
			Message:  fmt.Sprintf("zcp could not start the dev server it keeps on %s again: %v", hostname, err),
		}
	}
	// Stopped while this start ran: the stop forgot it before killing, so a
	// record that is gone means the process just spawned must go too.
	if now, _ := workflow.KeptDevServerFor(stateDir, hostname); now == nil {
		_ = ops.KillSpawnedDevServer(ctx, ssh, hostname, rec.LogFile)
		return nil
	}
	record(result)
	result.Message = fmt.Sprintf("Brought back the dev server zcp keeps on %s (`%s`) after its container restarted or was redeployed. %s", hostname, rec.Command, result.Message)
	return result
}

// deployRestoreAttempts / deployRestoreWait bound how long a deploy waits to
// reach its new container: the build's DEPLOYED can come a moment before the
// hostname stops answering from the old one.
const (
	deployRestoreAttempts = 5
	deployRestoreWait     = 2 * time.Second
)

// restoreKeptDevServerAfterDeploy is restoreKeptDevServer for a deploy that
// just replaced hostname's container; before is the kept record as it stood
// when the deploy began (nil: nothing was kept). The kept server went with the
// old container, so an answer from the life before names the old container
// still answering — wait and read again. A life that moved on meanwhile was
// claimed by the keeper's pass: report that bring-back once it is recorded,
// and while it still runs, say so — never nothing, which would send the agent
// to start a second copy.
func restoreKeptDevServerAfterDeploy(ctx context.Context, ssh ops.SSHDeployer, stateDir, hostname string, before *workflow.KeptDevServer, wait time.Duration) *ops.DevServerResult {
	if stateDir == "" || ssh == nil || before == nil {
		return nil
	}
	var rec *workflow.KeptDevServer
	for attempt := range deployRestoreAttempts {
		var err error
		rec, err = workflow.KeptDevServerFor(stateDir, hostname)
		if err != nil || rec == nil {
			return nil
		}
		if rec.Container != before.Container {
			if rec.LastRestore == nil {
				return nil // the agent took this life over itself
			}
			if rec.LastRestore.Reason != workflow.DevServerRestoring {
				return keeperRestoreResult(*rec)
			}
		} else if result := restoreKeptDevServer(ctx, ssh, stateDir, hostname); result != nil {
			return result
		}
		if attempt == deployRestoreAttempts-1 {
			break
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
	}
	if rec != nil && rec.Container != before.Container && rec.LastRestore != nil && rec.LastRestore.Reason == workflow.DevServerRestoring {
		return &ops.DevServerResult{
			Action:   "start",
			Hostname: hostname,
			Port:     rec.Port,
			Reason:   reasonKeeperRestoring,
			Message:  fmt.Sprintf("zcp's dev-server keeper is starting the dev server it keeps on %s (`%s`) in the new container right now.", hostname, rec.Command),
		}
	}
	return nil
}

// keeperRestoreResult states a bring-back the keeper made, from its record.
func keeperRestoreResult(rec workflow.KeptDevServer) *ops.DevServerResult {
	state := "it answers"
	if !rec.LastRestore.Running {
		state = "it did not come up (" + rec.LastRestore.Reason + ")"
	}
	return &ops.DevServerResult{
		Action:   "start",
		Hostname: rec.Hostname,
		Port:     rec.Port,
		Running:  rec.LastRestore.Running,
		Reason:   rec.LastRestore.Reason,
		Message:  fmt.Sprintf("zcp's dev-server keeper brought back the dev server it keeps on %s (`%s`) in the new container; %s.", rec.Hostname, rec.Command, state),
	}
}

// bringBackKeptDevServer runs after a successful deploy onto hostname. The
// deploy replaced the container, so a dev server zcp keeps there is gone: it is
// started again at once, reported as result.DevServer, and the next step
// follows from whether it came up. before is the kept record read when the
// deploy began. Returns whether it answers — a listener the public-access hook
// can count on. With nothing kept, result is untouched.
func bringBackKeptDevServer(ctx context.Context, ssh ops.SSHDeployer, stateDir, hostname string, before *workflow.KeptDevServer, result *ops.DeployResult) bool {
	ds := restoreKeptDevServerAfterDeploy(ctx, ssh, stateDir, hostname, before, deployRestoreWait)
	if ds == nil {
		return false
	}
	result.DevServer = ds
	result.NextActions = keptDevServerNextActions(hostname, ds)
	return ds.Running
}

// keptDevServerNextActions is the next step after a deploy brought back the
// dev server zcp keeps on hostname, by how the bring-back went.
func keptDevServerNextActions(hostname string, ds *ops.DevServerResult) string {
	switch {
	case ds.Running:
		return fmt.Sprintf("zcp started the dev server it keeps on %s again and it answers — run zerops_verify serviceHostname=%q.", hostname, hostname)
	case ds.Reason == ops.ReasonHealthProbeTimeout, ds.Reason == reasonKeeperRestoring:
		return fmt.Sprintf("zcp is starting the dev server it keeps on %s again; it did not answer yet and may still be starting (a first compile) — check zerops_dev_server action=status hostname=%q, then run zerops_verify.", hostname, hostname)
	default:
		return fmt.Sprintf("zcp started the dev server it keeps on %s again, but it did not come up: read devServer.reason and devServer.logTail, fix the cause, then zerops_dev_server action=restart hostname=%q.", hostname, hostname)
	}
}

// KeptDevServerRestore names one bring-back made by a keeper pass.
type KeptDevServerRestore struct {
	Hostname string
	Command  string
	Running  bool
	Reason   string
}

// KeepDevServers is one pass of the keeper: every kept dev server whose
// container restarted or was redeployed since it was started is started again,
// and one whose bring-back did not come up is retried while retries remain.
// Returns the bring-backs it made, for the keeper's log.
func KeepDevServers(ctx context.Context, ssh ops.SSHDeployer, stateDir string) []KeptDevServerRestore {
	kept, err := workflow.KeptDevServers(stateDir)
	if err != nil {
		return nil
	}
	var out []KeptDevServerRestore
	for _, rec := range kept {
		if ctx.Err() != nil {
			break
		}
		result := restoreKeptDevServer(ctx, ssh, stateDir, rec.Hostname)
		if result == nil {
			continue
		}
		out = append(out, KeptDevServerRestore{
			Hostname: rec.Hostname,
			Command:  rec.Command,
			Running:  result.Running,
			Reason:   result.Reason,
		})
	}
	return out
}

// RunDevServerKeeper is the keeper unit's life (`zcp dev-server keep`): a pass
// every interval until ctx ends, each bring-back logged to w. The first pass
// runs at once — the unit starts when the zcp container boots, and a project
// that was stopped comes back with every container at once.
func RunDevServerKeeper(ctx context.Context, ssh ops.SSHDeployer, stateDir string, interval time.Duration, w io.Writer) {
	for {
		for _, r := range KeepDevServers(ctx, ssh, stateDir) {
			state := "running"
			if !r.Running {
				state = "not up: " + r.Reason
			}
			fmt.Fprintf(w, "zcp dev-server keep: brought back %s (%s) after its container restarted or was redeployed — %s\n", r.Hostname, r.Command, state)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}
