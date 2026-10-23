package ops

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/platform"
)

// A branch that spans several app parts holds one lease per part's pool
// (DP-17), and each part's dev servers reach the same branch's sibling
// workspaces by container hostname (docs/spec-workflows.md §8 DP-21). The
// repository's declaration names, in siblingEnv, the variable a part's servers
// read and the pool it reaches. A dev server is the supervisor's child, never
// zcp's, so zcp hands the value to the supervisor as its env overlay
// (`zcp-devsup ctl env`, docs/spec-mate.md §2.8a.2), which puts it into the
// exec env of every server it starts. zcp sends the full overlay — empty
// included, which also drops whatever a previous holder of the container left
// — before it starts or reloads a lease's servers: a dev-server start or
// restart, and an acquire's supervisor step (a reset took the supervisor's
// state with it, so nothing carries over). A pool the branch holds no lease
// on is left out, so the variable keeps the container's own value (the part's
// stage). `ctl env` is a capability: an older supervisor's ctl fails on the
// verb, so zcp sends it only when `ctl version` lists env.

// LeaseSiblingEnv is the env overlay of a lease's dev servers: each variable
// the declaration's siblingEnv names, set to the container hostname of the
// branch's lease on the pool it names, or left to the container's own env
// when the branch holds no lease on that pool.
type LeaseSiblingEnv struct {
	// Set is the overlay: variable → container hostname.
	Set map[string]string `json:"set,omitempty"`
	// LeftToContainerEnv are the variables whose pool the branch holds no
	// lease on, sorted.
	LeftToContainerEnv []string `json:"leftToContainerEnv,omitempty"`
	// pools is the declaration's siblingEnv (variable → pool), branch the
	// lease's.
	pools  map[string]string
	branch string
}

// leaseSiblingEnv is lease's overlay from the declaration's siblingEnv and
// zcp's records of the lease's group (LeaseGroup) — never the orchestrator.
// A variable naming the lease's own pool gets its own container.
func leaseSiblingEnv(stateDir string, lease LeaseTargetInfo, siblingEnv map[string]string) (LeaseSiblingEnv, error) {
	w := LeaseSiblingEnv{Set: map[string]string{}, pools: siblingEnv, branch: cmp.Or(lease.Branch, leaseDefaultBranch)}
	if len(siblingEnv) == 0 {
		return w, nil
	}
	group, err := LeaseGroup(stateDir, lease.Alias)
	if err != nil {
		return LeaseSiblingEnv{}, fmt.Errorf("read the group of lease %s: %w", lease.Alias, err)
	}
	hosts := make(map[string]string, len(group))
	for _, m := range group {
		hosts[m.Pool] = m.ContainerHostname
	}
	for _, name := range slices.Sorted(maps.Keys(siblingEnv)) {
		if host, ok := hosts[siblingEnv[name]]; ok {
			w.Set[name] = host
		} else {
			w.LeftToContainerEnv = append(w.LeftToContainerEnv, name)
		}
	}
	return w, nil
}

// ctlArgs is the `zcp-devsup ctl` argv that replaces the supervisor's overlay
// with exactly Set: every pair, or --clear when it is empty.
func (w LeaseSiblingEnv) ctlArgs() []string {
	if len(w.Set) == 0 {
		return []string{devsup.CapabilityEnv, "--clear"}
	}
	args := []string{devsup.CapabilityEnv}
	for _, name := range slices.Sorted(maps.Keys(w.Set)) {
		args = append(args, "--set", name+"="+w.Set[name])
	}
	return args
}

// names lists the variables the overlay sets.
func (w LeaseSiblingEnv) names() string {
	return strings.Join(slices.Sorted(maps.Keys(w.Set)), ", ")
}

// sentence says what alias's dev servers start with, "" when the
// declaration names no sibling variable.
func (w LeaseSiblingEnv) sentence(alias string) string {
	if len(w.pools) == 0 {
		return ""
	}
	var set, left []string
	for _, name := range slices.Sorted(maps.Keys(w.Set)) {
		set = append(set, fmt.Sprintf("%s=%s (branch %s's lease on %s)", name, w.Set[name], w.branch, w.pools[name]))
	}
	for _, name := range w.LeftToContainerEnv {
		left = append(left, fmt.Sprintf("%s keeps the container's own value (branch %s holds no lease on %s)", name, w.branch, w.pools[name]))
	}
	if len(set) == 0 {
		return "Dev servers on " + alias + ": " + strings.Join(left, ", ") + "."
	}
	out := "Dev servers on " + alias + " get " + strings.Join(set, ", ")
	if len(left) > 0 {
		out += "; " + strings.Join(left, ", ")
	}
	return out + "."
}

// olderSupervisorRecovery is how a lease's pool gets a supervisor that takes
// the overlay.
func olderSupervisorRecovery(lease LeaseTargetInfo) string {
	return fmt.Sprintf("Point the zcp-devsup line in run.prepareCommands of pool %s's zerops.yaml at a release whose `zcp-devsup ctl version` lists env (the one this zcp ships), deploy the pool, release this lease (zerops_lease action=release lease=%q), roll the pool out (zerops_lease action=rollout pool=%q), then acquire again.",
		lease.Pool, lease.Alias, lease.Pool)
}

// olderSupervisorText names a supervisor without the env capability and the
// variables it cannot set.
func olderSupervisorText(lease LeaseTargetInfo, v SupervisorVersion, w LeaseSiblingEnv) string {
	return fmt.Sprintf("lease %s (a container of pool %s) runs zcp-devsup %s, whose `ctl version` does not list env, so it cannot set %s for its dev servers",
		lease.Alias, lease.Pool, cmp.Or(v.Version, "of unknown version"), w.names())
}

// overlayRestartBudget bounds the wait for the restarts a changed overlay
// sets off, as a start's own wait is bounded.
const overlayRestartBudget = time.Duration(maxDevServerWait) * time.Second

// leaseWiring is what wiring a lease's servers before a start did.
type leaseWiring struct {
	env LeaseSiblingEnv
	// restarted names the servers the supervisor restarted under the new
	// overlay during the call; pending those whose restart the wait did not
	// see.
	restarted map[string]bool
	pending   []string
}

// wireLeaseServers hands a lease's supervisor its overlay before a start or
// reload of its servers. The overlay comes from the declaration zcp read; a
// supervisor whose `ctl version` does not list env never gets it — a start
// whose overlay is not empty is refused there with the way to a newer one,
// an empty one starts as before. A changed overlay has the supervisor restart
// its running servers after its answer, one at a time, so zcp reads its
// status until each has been restarted: a start of a server still running
// under the old overlay would probe the process about to be restarted.
func wireLeaseServers(ctx context.Context, ssh SSHDeployer, p DevServerParams, ds *DevServers) (leaseWiring, error) {
	env, err := leaseSiblingEnv(p.StateDir, *p.lease, ds.Declared.SiblingEnv)
	if err != nil {
		return leaseWiring{}, err
	}
	w := leaseWiring{env: env}
	if !ds.Supervisor.Supports(devsup.CapabilityEnv) {
		if len(env.Set) > 0 {
			return leaseWiring{}, platform.NewPlatformError(platform.ErrPreflightFailed,
				"Not starting dev servers on "+p.Hostname+": "+olderSupervisorText(*p.lease, ds.Supervisor, env)+" — they would read the container's own values instead.",
				olderSupervisorRecovery(*p.lease))
		}
		return w, nil
	}
	before := ds.State
	st, err := supervisorCtl(ctx, ssh, p.Hostname, env.ctlArgs()...)
	if err != nil {
		return leaseWiring{}, err
	}
	ds.observe(st)
	if maps.Equal(before.Env, env.Set) {
		return w, nil
	}
	st, w.pending, err = awaitOverlayRestarts(ctx, ssh, p.Hostname, before, overlayRestartBudget, devServersSettlePoll)
	if err != nil {
		return leaseWiring{}, err
	}
	ds.observe(st)
	w.restarted = map[string]bool{}
	for name, s := range st.Servers {
		if was, ok := before.Servers[name]; ok && s.Restarts > was.Restarts {
			w.restarted[name] = true
		}
	}
	return w, nil
}

// apply reports the wiring in a start's result: the overlay when the
// declaration names sibling variables, and the servers whose restart under it
// zcp did not see.
func (w leaseWiring) apply(result *DevServerResult) {
	if len(w.env.pools) > 0 {
		env := w.env
		result.SiblingEnv = &env
	}
	if s := w.env.sentence(result.Hostname); s != "" {
		result.Message += " " + s
	}
	if len(w.pending) > 0 {
		result.Message += fmt.Sprintf(" The supervisor had not yet restarted %s under the new overlay. %s",
			strings.Join(w.pending, ", "), RecheckSentence(result.Hostname, "", w.pending))
	}
}

// liveServer reports whether a server runs, or is starting, and is meant to.
func liveServer(s devsup.ServerState) bool {
	return s.Desired == devsup.DesiredRunning && (s.Status == devsup.StatusRunning || s.Status == devsup.StatusStarting)
}

// awaitOverlayRestarts reads the supervisor's status, after a `ctl env` that
// changed its overlay, until every server live before it has been restarted
// (its restart count grew) or no longer runs, or budget has passed. It answers
// the last state and, sorted, the servers it did not see restarted; a failed
// read ends the wait with its error.
func awaitOverlayRestarts(ctx context.Context, ssh SSHDeployer, hostname string, before devsup.State, budget, every time.Duration) (devsup.State, []string, error) {
	waiting := map[string]int{}
	for name, s := range before.Servers {
		if liveServer(s) {
			waiting[name] = s.Restarts
		}
	}
	deadline := time.Now().Add(budget)
	for {
		st, err := supervisorCtl(ctx, ssh, hostname, devServerActionStatus)
		if err != nil {
			return devsup.State{}, nil, err
		}
		var pending []string
		for name, restarts := range waiting {
			if now, ok := st.Servers[name]; ok && liveServer(now) && now.Restarts <= restarts {
				pending = append(pending, name)
			}
		}
		slices.Sort(pending)
		if len(pending) == 0 || !time.Now().Add(every).Before(deadline) {
			return st, pending, nil
		}
		select {
		case <-ctx.Done():
			return st, pending, nil
		case <-time.After(every):
		}
	}
}

// checkoutOverlay is the overlay an acquire's supervisor step sends after the
// checkout: env, the `ctl env` argv (nil: none), and a note when the
// supervisor cannot take a non-empty one. A pool whose image carries no
// zcp-devsup gets nothing (the supervisor step names it); nothing here fails
// the acquire.
func checkoutOverlay(stateDir, alias string, read leaseCheckoutRead) (LeaseSiblingEnv, []string, string) {
	var siblingEnv map[string]string
	if read.declaration != nil {
		siblingEnv = read.declaration.SiblingEnv
	}
	rec, err := loadLeaseRecord(stateDir, alias)
	if err != nil {
		return LeaseSiblingEnv{}, nil, fmt.Sprintf("zcp could not read its record of %s, so it set no sibling overlay for its dev servers (%v)", alias, err)
	}
	lease := rec.targetInfo()
	env, err := leaseSiblingEnv(stateDir, lease, siblingEnv)
	if err != nil {
		return LeaseSiblingEnv{}, nil, fmt.Sprintf("zcp set no sibling overlay for the dev servers: %v", err)
	}
	switch {
	case read.supervisor.Supports(devsup.CapabilityEnv):
		return env, env.ctlArgs(), ""
	case len(env.Set) > 0 && read.supervisor.Version != "":
		return env, nil, olderSupervisorText(lease, read.supervisor, env) + "; they read the container's own values. " + olderSupervisorRecovery(lease)
	}
	return env, nil, ""
}

// overlayRefusedNote is what an acquire says when the supervisor refused the
// overlay its step sent.
func overlayRefusedNote(alias string, env LeaseSiblingEnv, err error) string {
	what := "the cleared sibling overlay"
	if len(env.Set) > 0 {
		what = "the sibling overlay (" + env.names() + ")"
	}
	return fmt.Sprintf("the dev-server supervisor refused %s (%v), so its servers start without it; zerops_dev_server action=start hostname=%q sends it again", what, err, alias)
}
