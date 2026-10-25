package ops

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// A branch that spans several app parts holds one lease per part's pool
// (DP-17), and each part's dev servers reach the same branch's sibling
// workspaces by container hostname (docs/spec-workflows.md §8 DP-22). The
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
//
// A released container goes to the pool's next acquirer under the same
// hostname, so a sibling still wired to it would reach someone else's
// workspace. Every change zcp makes to a group's membership therefore first
// hands every other lease of the group the overlay recomputed without (or
// with) the lease (DP-26): an acquire that adds a lease, after its checkout,
// and a release that takes one away, before the orchestrator is asked — a
// release that then fails hands it back with the lease still in. A lease zcp
// cannot rewire is a note naming it and the call that sends the overlay
// again, never a refusal: another lease's health must not strand a container.
// A lease that ends outside zcp is not chased; status names the wiring that
// still points at its container (wiring: stale) and changes nothing.

// LeaseSiblingEnv is the env overlay of a lease's dev servers: each variable
// the declaration's siblingEnv names, set to the container hostname of the
// branch's lease on the pool it names, and each variable its
// siblingPreviewEnv names, set to that lease's preview URL. A variable is left
// to the container's own env when the branch holds no lease on its pool, or
// when zcp cannot build that lease's preview URL.
type LeaseSiblingEnv struct {
	// Set is the overlay: variable → container hostname or preview URL.
	Set map[string]string `json:"set,omitempty"`
	// LeftToContainerEnv are the variables the overlay does not set, sorted.
	LeftToContainerEnv []string `json:"leftToContainerEnv,omitempty"`
	// pools is every variable the declaration names → the pool it reaches;
	// preview marks siblingPreviewEnv's; why says, for a preview variable
	// left out although the branch leases its pool, why zcp could not build
	// the URL; branch is the lease's.
	pools   map[string]string
	preview map[string]bool
	why     map[string]string
	branch  string
}

// siblingWiring is what a declaration asks a lease's servers to be handed:
// container hostnames (siblingEnv), preview URLs (siblingPreviewEnv), and the
// preview port base those URLs are on.
type siblingWiring struct {
	hosts, previews map[string]string
	previewBase     int
}

// siblingWiringOf is d's sibling wiring; none for no declaration.
func siblingWiringOf(d *devsup.Declaration) siblingWiring {
	if d == nil {
		return siblingWiring{}
	}
	return siblingWiring{hosts: d.SiblingEnv, previews: d.SiblingPreviewEnv, previewBase: d.PreviewPortBase}
}

// previewURLFunc is the public address of a pool's preview port as
// zerops_dev_server status reports a lease's (https, the pool's subdomain on
// that port), "" when the pool serves none there.
type previewURLFunc func(pool string, port int) string

// leasePreviewURLs builds preview addresses from the pools' REST-authoritative
// details, whether or not a pool's subdomain is on yet: the address does not
// change when it goes on, and a lease's dev-server start turns it on (DP-19).
// nil without an API client.
func leasePreviewURLs(ctx context.Context, client platform.Client, projectID string) previewURLFunc {
	if client == nil {
		return nil
	}
	return func(pool string, port int) string {
		return leasePreviewURL(ctx, client, projectID, pool, port, false)
	}
}

// previewURLs is leasePreviewURLs over d's API client.
func (d LeaseDeps) previewURLs(ctx context.Context) previewURLFunc {
	return leasePreviewURLs(ctx, d.Client, d.ProjectID)
}

// leaseSiblingEnv is lease's overlay from the declaration's sibling wiring and
// zcp's records of the lease's group (LeaseGroup) — never the orchestrator. A
// variable naming the lease's own pool gets its own container or preview URL.
func leaseSiblingEnv(stateDir string, lease LeaseTargetInfo, w siblingWiring, previewURL previewURLFunc) (LeaseSiblingEnv, error) {
	return leaseSiblingEnvWithout(stateDir, lease, w, previewURL, nil)
}

// leaseSiblingEnvWithout is leaseSiblingEnv once the leases in leaving
// (aliases) have left the group. A preview URL is the address of
// previewPortBase + the sibling container's stable number, which its record
// keeps from its checkout, at its pool's subdomain (previewURL): the port
// follows the container, so it moves when the part joins again on another
// one, and is resolved here every time, never copied.
func leaseSiblingEnvWithout(stateDir string, lease LeaseTargetInfo, w siblingWiring, previewURL previewURLFunc, leaving map[string]bool) (LeaseSiblingEnv, error) {
	env := LeaseSiblingEnv{Set: map[string]string{}, pools: map[string]string{}, preview: map[string]bool{}, why: map[string]string{},
		branch: cmp.Or(lease.Branch, leaseDefaultBranch)}
	if len(w.hosts) == 0 && len(w.previews) == 0 {
		return env, nil
	}
	group, err := leaseGroupWithout(stateDir, lease.Alias, leaving)
	if err != nil {
		return LeaseSiblingEnv{}, fmt.Errorf("read the group of lease %s: %w", lease.Alias, err)
	}
	members := make(map[string]LeaseTargetInfo, len(group))
	for _, m := range group {
		members[m.Pool] = m
	}
	for name, pool := range w.hosts {
		env.pools[name] = pool
		if m, ok := members[pool]; ok {
			env.Set[name] = m.ContainerHostname
		}
	}
	for name, pool := range w.previews {
		env.pools[name], env.preview[name] = pool, true
		m, ok := members[pool]
		if !ok {
			continue
		}
		if url, why := siblingPreviewURL(m, w.previewBase, previewURL); url != "" {
			env.Set[name] = url
		} else {
			env.why[name] = why
		}
	}
	for _, name := range slices.Sorted(maps.Keys(env.pools)) {
		if _, set := env.Set[name]; !set {
			env.LeftToContainerEnv = append(env.LeftToContainerEnv, name)
		}
	}
	return env, nil
}

// siblingPreviewURL is the preview URL of the lease m, or why zcp cannot build
// it: the declaration names no previewPortBase, m's record predates keeping
// its container's stable number, or its pool serves no HTTP address on that
// port.
func siblingPreviewURL(m LeaseTargetInfo, previewBase int, previewURL previewURLFunc) (string, string) {
	switch {
	case previewBase == 0:
		return "", "the declaration names no previewPortBase, so no lease has a preview port"
	case m.StableNumber == 0:
		return "", fmt.Sprintf("zcp's record of %s was written before it kept its container's stable number, which the preview port follows; a lease acquired from now on carries it", m.Alias)
	}
	port := previewBase + m.StableNumber
	if previewURL != nil {
		if url := previewURL(m.Pool, port); url != "" {
			return url, ""
		}
	}
	return "", fmt.Sprintf("pool %s serves no HTTP address on %s's preview port %d (a port of the pool's run.ports with httpSupport)", m.Pool, m.Alias, port)
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
		whose := fmt.Sprintf("branch %s's lease on %s", w.branch, w.pools[name])
		if w.preview[name] {
			whose = "the preview URL of " + whose
		}
		set = append(set, fmt.Sprintf("%s=%s (%s)", name, w.Set[name], whose))
	}
	for _, name := range w.LeftToContainerEnv {
		why := fmt.Sprintf("branch %s holds no lease on %s", w.branch, w.pools[name])
		if w.why[name] != "" {
			why = w.why[name]
		}
		left = append(left, fmt.Sprintf("%s keeps the container's own value (%s)", name, why))
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
	// changed: the overlay sent differs from the one the supervisor held.
	changed bool
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
// an empty one starts as before. The overlay is computed from zcp's records
// and sent under the group's lock (lockLeaseGroup), so no release of a sibling
// drops it in between (DP-26); the lock is held for the records' read and the
// one `ctl env` exchange (devsupCtlTimeout), never for the wait. A changed
// overlay has the supervisor restart its running servers after its answer,
// one at a time, so zcp reads its status until each has been restarted: a
// start of a server still running under the old overlay would probe the
// process about to be restarted.
func wireLeaseServers(ctx context.Context, ssh SSHDeployer, previewURL previewURLFunc, p DevServerParams, ds *DevServers) (leaseWiring, error) {
	lease := *p.lease
	var before devsup.State
	w, err := func() (leaseWiring, error) {
		unlock, err := lockLeaseTargetGroup(ctx, p.StateDir, lease)
		if err != nil {
			return leaseWiring{}, err
		}
		defer unlock()
		env, err := leaseSiblingEnv(p.StateDir, lease, siblingWiringOf(&ds.Declared), previewURL)
		if err != nil {
			return leaseWiring{}, err
		}
		if !ds.Supervisor.Supports(devsup.CapabilityEnv) {
			if len(env.Set) > 0 {
				return leaseWiring{}, platform.NewPlatformError(platform.ErrPreflightFailed,
					"Not starting dev servers on "+p.Hostname+": "+olderSupervisorText(lease, ds.Supervisor, env)+" — they would read the container's own values instead.",
					olderSupervisorRecovery(lease))
			}
			return leaseWiring{env: env}, nil
		}
		var w leaseWiring
		before, w, err = sendLeaseOverlay(ctx, ssh, p.Hostname, ds, env)
		return w, err
	}()
	if err != nil || !w.changed {
		return w, err
	}
	return awaitLeaseOverlay(ctx, ssh, p.Hostname, ds, before, w)
}

// applyLeaseOverlay hands hostname's supervisor env as its overlay. A changed
// overlay has the supervisor restart its running servers after its answer,
// one at a time, so zcp reads its status until each has been restarted
// (awaitOverlayRestarts); the wiring names those restarted and those still to
// be. ds takes the supervisor's last answer.
func applyLeaseOverlay(ctx context.Context, ssh SSHDeployer, hostname string, ds *DevServers, env LeaseSiblingEnv) (leaseWiring, error) {
	before, w, err := sendLeaseOverlay(ctx, ssh, hostname, ds, env)
	if err != nil || !w.changed {
		return w, err
	}
	return awaitLeaseOverlay(ctx, ssh, hostname, ds, before, w)
}

// sendLeaseOverlay sends env to hostname's supervisor as its overlay (`ctl
// env`, one exchange): before is its state as zcp last read it, and the wiring
// says whether the overlay changed. ds takes the supervisor's answer.
func sendLeaseOverlay(ctx context.Context, ssh SSHDeployer, hostname string, ds *DevServers, env LeaseSiblingEnv) (devsup.State, leaseWiring, error) {
	before := ds.State
	st, err := supervisorCtl(ctx, ssh, hostname, env.ctlArgs()...)
	if err != nil {
		return devsup.State{}, leaseWiring{}, err
	}
	ds.observe(st)
	return before, leaseWiring{env: env, changed: !maps.Equal(before.Env, env.Set)}, nil
}

// awaitLeaseOverlay waits, once sendLeaseOverlay changed hostname's overlay
// (w), for the restarts it set off (awaitOverlayRestarts) and names those
// restarted and those still to be; before is the state the send started from.
// ds takes the supervisor's last answer.
func awaitLeaseOverlay(ctx context.Context, ssh SSHDeployer, hostname string, ds *DevServers, before devsup.State, w leaseWiring) (leaseWiring, error) {
	st, pending, err := awaitOverlayRestarts(ctx, ssh, hostname, before, overlayRestartBudget, devServersSettlePoll)
	if err != nil {
		return leaseWiring{}, err
	}
	ds.observe(st)
	w.pending = pending
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
func checkoutOverlay(stateDir, alias string, read leaseCheckoutRead, previewURL previewURLFunc) (LeaseSiblingEnv, []string, string) {
	rec, err := loadLeaseRecord(stateDir, alias)
	if err != nil {
		return LeaseSiblingEnv{}, nil, fmt.Sprintf("zcp could not read its record of %s, so it set no sibling overlay for its dev servers (%v)", alias, err)
	}
	lease := rec.targetInfo()
	env, err := leaseSiblingEnv(stateDir, lease, siblingWiringOf(read.declaration), previewURL)
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

// LeaseChange is what an acquire or a release did: the leases it returned,
// and every other lease of their branch it handed the recomputed sibling
// overlay (DP-26).
type LeaseChange struct {
	Leases  []LeaseView
	Rewired []LeaseRewire
}

// LeaseRewire is what handing one lease the sibling overlay recomputed for a
// change of its group did (DP-26): the overlay — present when its declaration
// names siblingEnv — the servers its supervisor restarted under it during the
// call, read back by the status reads that wait for them, those it had not yet
// restarted, and how long it took. A lease zcp could not rewire carries a
// note saying why and the call that sends the overlay again.
type LeaseRewire struct {
	Alias      string           `json:"alias"`
	SiblingEnv *LeaseSiblingEnv `json:"siblingEnv,omitempty"`
	Restarted  []string         `json:"restarted,omitempty"`
	Pending    []string         `json:"pending,omitempty"`
	Note       string           `json:"note,omitempty"`
	Millis     int64            `json:"ms"`
	// changed: the supervisor took an overlay other than the one it held.
	changed bool
}

// rewireLeases hands every lease of members the overlay recomputed from zcp's
// records without the leases in leaving (aliases; nil: as the records stand),
// all at once, each with its own read, send and wait (rewireLease). why says
// in the notes what the change is ("without hqpool-3"). Each member's
// container is read first (readLeaseOwnership), all at once: one that is no
// longer its lease's is sent nothing — ssh would reach whoever holds it now —
// and is a note naming the release that drops its record, and it leaves the
// overlay the others get, since its hostname now names someone else's
// workspace; one the orchestrator does not answer about is sent nothing and
// is a note, while it stays in the others' overlay (it may still be this
// lease's). A lease whose image carries no zcp-devsup runs no dev server and
// is left out. Nothing here fails the caller.
func rewireLeases(ctx context.Context, d LeaseDeps, members []leaseRecord, leaving map[string]bool, why string) []LeaseRewire {
	type outcome struct {
		r    LeaseRewire
		kept bool
	}
	outcomes := make([]outcome, len(members))
	idx := make([]int, len(members))
	for i := range idx {
		idx[i] = i
	}
	verdicts := make([]leaseVerdict, len(members))
	runMembers(idx, func(i int) { verdicts[i] = readLeaseOwnership(ctx, d, members[i]) })
	without := maps.Clone(leaving)
	if without == nil {
		without = map[string]bool{}
	}
	for i, v := range verdicts {
		if v.own.lost() {
			without[members[i].alias()] = true
		}
	}
	runMembers(idx, func(i int) {
		rec, v, alias := members[i], verdicts[i], members[i].alias()
		switch {
		case v.own.lost():
			outcomes[i].r, outcomes[i].kept = LeaseRewire{Alias: alias, Note: lostLeaseNote(alias, v.why, "sent it no overlay "+why)}, true
		case v.own == leaseUnverified:
			outcomes[i].r, outcomes[i].kept = LeaseRewire{Alias: alias, Note: fmt.Sprintf("zcp sent %s no overlay %s: %s; %s sends it once its pool's orchestrator answers", alias, why, v.why, resendCall(alias, nil))}, true
		default:
			outcomes[i].r, outcomes[i].kept = rewireLease(ctx, d.SSH, d.StateDir, d.previewURLs(ctx), rec, without, why)
		}
	})
	var out []LeaseRewire
	for _, o := range outcomes {
		if o.kept {
			out = append(out, o.r)
		}
	}
	return out
}

// rewireLease hands one lease the overlay its group will have once the leases
// in leaving have left: read its dev servers, compute the overlay from its
// declaration and zcp's records, and send it when it differs from the one its
// supervisor holds (applyLeaseOverlay, which waits for the restarts). A
// declaration that cannot be read keeps only the variables still naming a
// container of the group, so no server is left wired to one that left. kept is
// false for a lease without zcp-devsup.
func rewireLease(ctx context.Context, ssh SSHDeployer, stateDir string, previewURL previewURLFunc, rec leaseRecord, leaving map[string]bool, why string) (r LeaseRewire, kept bool) {
	start := time.Now()
	alias := rec.alias()
	r = LeaseRewire{Alias: alias}
	defer func() { r.Millis = time.Since(start).Milliseconds() }()
	if rec.Parked {
		// Nothing answers on a parked lease; its unpark sends it the overlay
		// the records then give (DP-28).
		r.Note = fmt.Sprintf("%s is parked, so zcp sent it no overlay %s; %s sends it the branch's overlay as it then stands", alias, why, LeaseUnparkCall(alias))
		return r, true
	}
	ds, err := ReadDevServers(ctx, ssh, alias, "")
	var noSup *NoSupervisorError
	var unresponsive *SupervisorUnresponsiveError
	switch {
	case errors.As(err, &noSup) && !noSup.Installed:
		return r, false
	case errors.As(err, &noSup):
		r.Note = fmt.Sprintf("no zcp-devsup runs on %s, so zcp sent it no overlay %s; the next zerops_dev_server start there sends the branch's", alias, why)
		return r, true
	case errors.As(err, &unresponsive):
		r.Note = fmt.Sprintf("the dev-server supervisor on %s did not answer, so zcp could not send it the overlay %s; %s sends it again once it answers", alias, why, resendCall(alias, nil))
		return r, true
	case err != nil:
		r.Note = fmt.Sprintf("zcp could not read the dev-server supervisor on %s (%s), so its servers keep the overlay they had instead of the one %s; %s sends it again once it answers", alias, sshFailureText(err), why, resendCall(alias, nil))
		return r, true
	}
	ds.Service = rec.PoolHostname
	live := liveServers(ds.State)
	var env LeaseSiblingEnv
	if ds.DeclarationErr != nil {
		records, err := readLeaseRecords(stateDir)
		if err != nil {
			r.Note = fmt.Sprintf("zcp could not read its records, so it sent %s no overlay %s (%v); %s sends it again", alias, why, err, resendCall(alias, live))
			return r, true
		}
		reach := groupContainers(stateDir, rec, records, leaving)
		env = LeaseSiblingEnv{Set: map[string]string{}}
		for name, value := range ds.State.Env {
			if reach.holds(value) {
				env.Set[name] = value
			}
		}
		r.Note = fmt.Sprintf("the %s on %s cannot be read (%v), so zcp only dropped from its overlay the variables naming a container, or the preview URL of a pool, no lease of the branch holds then", devsup.DeclarationPath, alias, ds.DeclarationErr)
	} else if env, err = leaseSiblingEnvWithout(stateDir, rec.targetInfo(), siblingWiringOf(&ds.Declared), previewURL, leaving); err != nil {
		r.Note = fmt.Sprintf("zcp sent %s no overlay %s: %v; %s sends it again", alias, why, err, resendCall(alias, live))
		return r, true
	}
	if len(env.pools) > 0 {
		e := env
		r.SiblingEnv = &e
	}
	if maps.Equal(ds.State.Env, env.Set) {
		return r, true
	}
	if !ds.Supervisor.Supports(devsup.CapabilityEnv) {
		r.Note = joinNotes(r.Note, olderSupervisorText(rec.targetInfo(), ds.Supervisor, env)+"; they read the container's own values. "+olderSupervisorRecovery(rec.targetInfo()))
		return r, true
	}
	w, err := applyLeaseOverlay(ctx, ssh, alias, ds, env)
	if err != nil {
		r.Note = joinNotes(r.Note, fmt.Sprintf("the dev-server supervisor on %s refused the overlay %s (%s), so its servers keep the one they had; %s sends it again", alias, why, sshFailureText(err), resendCall(alias, live)))
		return r, true
	}
	r.changed = w.changed
	r.Restarted = slices.Sorted(maps.Keys(w.restarted))
	r.Pending = w.pending
	if len(w.pending) > 0 {
		r.Note = joinNotes(r.Note, fmt.Sprintf("the supervisor on %s had not yet restarted %s under the new overlay. %s", alias, strings.Join(w.pending, ", "), RecheckSentence(alias, "", w.pending)))
	}
	return r, true
}

// resendCall is the call that hands alias's supervisor the branch's overlay
// again: a restart of one of its live servers — a start or restart sends the
// overlay first, and the supervisor restarts every server running under
// another one — or of whichever server, when zcp could not read which run.
func resendCall(alias string, live []string) string {
	if len(live) == 0 {
		return fmt.Sprintf("zerops_dev_server action=restart hostname=%q name=<one of its servers>", alias)
	}
	return fmt.Sprintf("zerops_dev_server action=restart hostname=%q name=%q", alias, live[0])
}

// liveServers names, sorted, the servers st runs or starts and means to.
func liveServers(st devsup.State) []string {
	var names []string
	for name, s := range st.Servers {
		if liveServer(s) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// siblingRecords is zcp's records of the other leases of rec's group (DP-17):
// none for a lease whose repository zcp knows neither way, or when the records
// cannot be read.
func siblingRecords(stateDir string, rec leaseRecord) []leaseRecord {
	repo := recordRepo(stateDir, rec)
	if repo == "" {
		return nil
	}
	group, err := leaseGroupRecords(stateDir, repo, recordBranch(rec))
	if err != nil {
		return nil
	}
	return slices.DeleteFunc(group, func(r leaseRecord) bool { return r.alias() == rec.alias() })
}

// handBackOverlay is a release's refusal once it failed after rec's siblings
// were rewired without it. A lease the orchestrator still reserves stays held,
// so every sibling the rewire changed gets the overlay with it back in — and
// the refusal says so, or which sibling it could not hand back. One whose
// container the orchestrator let go (the release went, its record could not
// be dropped), or that zcp cannot tell, is never handed back: the pool may
// give the container to its next holder; the siblings stay without it and the
// refusal names the call that wires them again. The caller's context may be
// what ended the release; the hand-back runs all the same.
func handBackOverlay(ctx context.Context, d LeaseDeps, rec leaseRecord, siblings []leaseRecord, rewired []LeaseRewire, cause error) error {
	alias := rec.alias()
	changed := map[string]LeaseRewire{}
	for _, r := range rewired {
		if r.changed {
			changed[r.Alias] = r
		}
	}
	back := slices.DeleteFunc(slices.Clone(siblings), func(r leaseRecord) bool { _, ok := changed[r.alias()]; return !ok })
	if len(back) == 0 {
		return cause
	}
	ctx = context.WithoutCancel(ctx)
	text := fmt.Sprintf("zcp had handed %s the sibling overlay without %s before asking for the release", strings.Join(aliasesOf(back), ", "), alias)
	switch v := readLeaseOwnership(ctx, d, rec); v.own {
	case leaseUnverified, leaseGone, leaseFree, leaseOther:
		why := alias + "'s container is no longer its lease's (" + v.why + ")"
		if v.own == leaseUnverified {
			why = fmt.Sprintf("zcp could not tell whether %s's container is still its lease's (%s)", alias, v.why)
		}
		calls := make([]string, 0, len(back))
		for _, r := range back {
			calls = append(calls, resendCall(r.alias(), changed[r.alias()].Restarted))
		}
		text += fmt.Sprintf("; %s, so they stay without it — once %s is held again, %s wires them to it", why, alias, strings.Join(calls, "; "))
	case leaseOurs:
		var ok, failed []string
		for _, r := range rewireLeases(ctx, d, back, nil, "with "+alias+" back in") {
			if r.Note == "" {
				ok = append(ok, r.Alias)
			} else {
				failed = append(failed, r.Alias+" ("+r.Note+")")
			}
		}
		switch {
		case len(failed) == 0:
			text += fmt.Sprintf(", and as %s stays held it handed it back with %s back in", alias, alias)
		case len(ok) > 0:
			text += fmt.Sprintf("; it handed it back with %s back in to %s, but not to %s", alias, strings.Join(ok, ", "), strings.Join(failed, "; "))
		default:
			text += fmt.Sprintf("; it could not hand it back with %s back in to %s", alias, strings.Join(failed, "; "))
		}
	}
	var pe *platform.PlatformError
	if !errors.As(cause, &pe) {
		return fmt.Errorf("%w (%s)", cause, text)
	}
	out := *pe
	out.Message += "; " + text
	return &out
}

func aliasesOf(records []leaseRecord) []string {
	out := make([]string, 0, len(records))
	for _, r := range records {
		out = append(out, r.alias())
	}
	return out
}

// LeaseWiringStale marks a lease whose dev servers run with an overlay naming
// a container no lease of its group holds (DP-26).
const LeaseWiringStale = "stale"

// leaseOverlay is a lease supervisor's overlay as one read found it: the
// variables it sets and, sorted, the servers running under it.
type leaseOverlay struct {
	env  map[string]string
	live []string
}

// overlayOf reads a lease's overlay from the supervisor part of status's read
// of its checkout (leasePreviewCommand): nil unless its `ctl version` lists env
// and its `ctl status` answered.
func overlayOf(part string) *leaseOverlay {
	sections, err := splitDevsupSections(part)
	if err != nil {
		return nil
	}
	version, ok := sections["version"]
	if !ok || version.code != devsupCodeOK {
		return nil
	}
	var v SupervisorVersion
	if json.Unmarshal([]byte(version.body), &v) != nil || !v.Supports(devsup.CapabilityEnv) {
		return nil
	}
	status, ok := sections["status"]
	if !ok || status.code != devsupCodeOK {
		return nil
	}
	var st devsup.State
	if json.Unmarshal([]byte(status.body), &st) != nil {
		return nil
	}
	return &leaseOverlay{env: st.Env, live: liveServers(st)}
}

// groupReach is what the leases of a group (DP-17) are reached by: their
// containers' hostnames, and their pools, which a preview URL names.
type groupReach struct {
	containers, pools map[string]bool
}

// holds reports whether an overlay value still names a lease of the group: a
// preview URL its pool's lease, any other value a container of the group.
func (g groupReach) holds(value string) bool {
	if pool, ok := previewURLPool(value); ok {
		return g.pools[pool]
	}
	return g.containers[value]
}

// previewURLPool is the pool a preview URL zcp hands out names — the first
// label of its host is `<pool>-<project prefix>-<port>`, and a pool's hostname
// holds no dash — false for a value that is no https URL.
func previewURLPool(value string) (string, bool) {
	rest, ok := strings.CutPrefix(value, httpsScheme+"://")
	if !ok {
		return "", false
	}
	host, _, _ := strings.Cut(rest, "/")
	label, _, _ := strings.Cut(host, ".")
	pool, _, found := strings.Cut(label, "-")
	if !found || pool == "" {
		return "", false
	}
	return pool, true
}

// groupContainers is what the leases of rec's group (DP-17) among records —
// rec's own always — are reached by, leaving out the aliases in gone.
func groupContainers(stateDir string, rec leaseRecord, records []leaseRecord, gone map[string]bool) groupReach {
	reach := groupReach{containers: map[string]bool{rec.ContainerHostname: true}, pools: map[string]bool{rec.PoolHostname: true}}
	repo := recordRepo(stateDir, rec)
	if repo == "" {
		return reach
	}
	branch := recordBranch(rec)
	for _, r := range records {
		if !gone[r.alias()] && recordBranch(r) == branch && topology.SameRepoURL(recordRepo(stateDir, r), repo) {
			reach.containers[r.ContainerHostname] = true
			reach.pools[r.PoolHostname] = true
		}
	}
	return reach
}

// staleWiring says, "" when nothing is stale, that rec's live servers run with
// an overlay naming a container, or the preview URL of a pool, that no lease
// of its group holds — its group among records, less the aliases in lost —
// each variable with what it names and whose it was, and the call that sends
// the branch's overlay again.
func staleWiring(stateDir string, rec leaseRecord, o leaseOverlay, records []leaseRecord, lost map[string]bool) string {
	if len(o.live) == 0 {
		return ""
	}
	reach := groupContainers(stateDir, rec, records, lost)
	owners := make(map[string]leaseRecord, len(records))
	for _, r := range records {
		owners[r.ContainerHostname] = r
	}
	var stale []string
	for _, name := range slices.Sorted(maps.Keys(o.env)) {
		value := o.env[name]
		if reach.holds(value) {
			continue
		}
		stale = append(stale, fmt.Sprintf("%s=%s (%s)", name, value, staleWhose(stateDir, rec, value, records, owners, lost)))
	}
	if len(stale) == 0 {
		return ""
	}
	return fmt.Sprintf("its dev servers (%s) run with %s, which no lease of branch %s holds now — the pool hands such a container, and its preview port, to its next holder, so they would reach someone else's workspace; %s sends the branch's overlay again",
		strings.Join(o.live, ", "), strings.Join(stale, ", "), recordBranch(rec), resendCall(rec.alias(), o.live))
}

// staleWhose says whose the container, or the preview, a stale overlay value
// names was: a lease of rec's group lost by this status's word, a lease of
// another branch or repository, or none zcp holds.
func staleWhose(stateDir string, rec leaseRecord, value string, records []leaseRecord, owners map[string]leaseRecord, lost map[string]bool) string {
	if pool, ok := previewURLPool(value); ok {
		for _, r := range records {
			if r.PoolHostname == pool && lost[r.alias()] && recordBranch(r) == recordBranch(rec) && topology.SameRepoURL(recordRepo(stateDir, r), recordRepo(stateDir, rec)) {
				return r.alias() + "'s preview URL, whose lease is lost"
			}
		}
		return fmt.Sprintf("a preview URL of pool %s, where branch %s holds no lease now", pool, recordBranch(rec))
	}
	owner, ok := owners[value]
	switch {
	case !ok:
		return "a container zcp holds no lease of"
	case lost[owner.alias()]:
		return owner.alias() + "'s container, whose lease is lost"
	case recordBranch(owner) != recordBranch(rec):
		return owner.alias() + "'s container, a lease of branch " + recordBranch(owner)
	}
	return owner.alias() + "'s container, a lease of another repository"
}

// markStaleWiring marks each lease of views whose read found its servers
// running with an overlay naming a container no lease of its group holds by
// this status's word — a group record the orchestrator gave as lost, or no
// record of the group at all — wiring: stale, its note saying which and the
// call that fixes it. It changes nothing anywhere (docs/spec-mate.md §0 rule
// 4): a lease that ended outside zcp is not chased.
func markStaleWiring(stateDir string, records []leaseRecord, views []LeaseView) {
	lost := map[string]bool{}
	for _, v := range views {
		if v.State == LeaseStateLost {
			lost[v.Alias] = true
		}
	}
	byAlias := make(map[string]leaseRecord, len(records))
	for _, r := range records {
		byAlias[r.alias()] = r
	}
	for i := range views {
		v := &views[i]
		rec, ok := byAlias[v.Alias]
		if v.overlay == nil || !ok {
			continue
		}
		if why := staleWiring(stateDir, rec, *v.overlay, records, lost); why != "" {
			v.Wiring = LeaseWiringStale
			v.Note = joinNotes(v.Note, "wiring stale: "+why+" (status changes nothing)")
		}
	}
}

// applyStaleWiring marks a dev-server status of a lease whose servers run with
// an overlay naming a container no lease of its group holds by zcp's records
// wiring: stale, saying which and the call that fixes it; it changes nothing.
func (r *DevServerResult) applyStaleWiring(stateDir string, lease LeaseTargetInfo, st devsup.State) {
	records, err := readLeaseRecords(stateDir)
	if err != nil {
		return
	}
	i := slices.IndexFunc(records, func(x leaseRecord) bool { return x.alias() == lease.Alias })
	if i < 0 {
		return
	}
	o := leaseOverlay{env: st.Env, live: liveServers(st)}
	if why := staleWiring(stateDir, records[i], o, records, nil); why != "" {
		r.Wiring = LeaseWiringStale
		r.Message += " Wiring stale: " + why + "."
	}
}
