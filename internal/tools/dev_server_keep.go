package tools

import (
	"context"
	"fmt"
	"io"
	"strings"
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

// devServerKeeperUnit is the zcp container's unit running the keeper
// (zerops@devservers).
const devServerKeeperUnit = "devservers"

// devServerKeeperCommand is the keeper unit's command line. The state dir is
// passed explicitly: a unit's working directory is not the agent's.
func devServerKeeperCommand(stateDir string) string {
	return "zcp dev-server keep --state-dir " + stateDir
}

// keptDevServerNote is appended to a successful start/restart's message.
const keptDevServerNote = " zcp keeps it: when this container restarts or is redeployed, zcp starts it again with the same command, until you stop it."

// keepStartedDevServer records a successful start or restart as the dev server
// zcp keeps on its hostname, and makes sure the keeper runs. It returns the
// warnings the caller surfaces — never an error: the dev server already runs.
func keepStartedDevServer(ctx context.Context, ssh ops.SSHDeployer, units ops.UnitRegistrar, stateDir string, p ops.DevServerParams) []string {
	if stateDir == "" {
		return nil
	}
	// The container life the server runs in, read once more on a miss. Still
	// unreadable (unlikely: ssh just worked) leaves it empty, and the keeper's
	// first pass adopts the life it finds as the baseline.
	container, err := ops.ContainerIdentity(ctx, ssh, p.Hostname)
	if err != nil {
		container, _ = ops.ContainerIdentity(ctx, ssh, p.Hostname)
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
		return []string{fmt.Sprintf("zcp could not record the dev server to keep it (%v): after a restart or redeploy of %s, start it again yourself", err, p.Hostname)}
	}
	if units == nil {
		return nil
	}
	if strings.ContainsAny(stateDir, " \t\n'\"") {
		return []string{fmt.Sprintf("the dev-server keeper cannot run from a state directory with spaces or quotes (%q): %s's dev server comes back after zcp's own deploys only", stateDir, p.Hostname)}
	}
	if err := units.EnsureUnit(ctx, devServerKeeperUnit, devServerKeeperCommand(stateDir)); err != nil {
		return []string{fmt.Sprintf("zcp could not start its dev-server keeper (%v): %s's dev server comes back after zcp's own deploys, but not after a restart zcp does not make", err, p.Hostname)}
	}
	return nil
}

// restoreKeptDevServer brings back the dev server kept on hostname when its
// container is no longer the one it was started in — restarted or redeployed.
// Nil when nothing is kept, when the container is unreachable, when it is the
// same container life (nothing to bring back), or when another caller already
// claimed this life. Otherwise the start's result, running or not; the outcome
// is recorded with the kept server either way, and no further attempt is made
// until the container cycles again.
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
	rec, claimed, err := workflow.ClaimKeptDevServer(stateDir, hostname, container)
	if err != nil || !claimed {
		return nil
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
	restore := workflow.DevServerRestore{At: time.Now().UTC().Format(time.RFC3339)}
	if err != nil {
		result = &ops.DevServerResult{
			Action:   "start",
			Hostname: hostname,
			Reason:   "invalid_kept_start",
			Message:  fmt.Sprintf("zcp could not start the dev server it keeps on %s again: %v", hostname, err),
		}
	}
	restore.Running = result.Running
	restore.Reason = result.Reason
	_ = workflow.RecordDevServerRestore(stateDir, hostname, container, restore)
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
// rather than nothing, which would send the agent to start a second copy.
func restoreKeptDevServerAfterDeploy(ctx context.Context, ssh ops.SSHDeployer, stateDir, hostname string, before *workflow.KeptDevServer) *ops.DevServerResult {
	if stateDir == "" || ssh == nil || before == nil {
		return nil
	}
	for attempt := range deployRestoreAttempts {
		rec, err := workflow.KeptDevServerFor(stateDir, hostname)
		if err != nil || rec == nil {
			return nil
		}
		if rec.Container != before.Container && rec.Container != "" {
			if rec.LastRestore != nil {
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
		case <-time.After(deployRestoreWait):
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
	ds := restoreKeptDevServerAfterDeploy(ctx, ssh, stateDir, hostname, before)
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
	case ds.Reason == ops.ReasonHealthProbeTimeout:
		return fmt.Sprintf("zcp started the dev server it keeps on %s again; it did not answer within the wait and may still be starting (a first compile) — check zerops_dev_server action=status hostname=%q, then run zerops_verify.", hostname, hostname)
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
// container restarted or was redeployed since it was started is started again.
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
