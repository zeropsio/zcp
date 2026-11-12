package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A wired pair's dev service reaches HQ only through the relay (`zcp git
// <host>`, ops.RelayedSSH), which answers HQ with the Mate's project key over
// the session: it holds no credential of its own. Its origin is the pair's
// repository at HQ's address — a copy of the HQ the Mate links to, stale the
// moment the Mate moves to another HQ. The repository pass moves it, but
// runs only when an agent bootstraps, adopts or starts develop; the Mate's
// own service does at its start (KeepMateDevGit).

// devGitDeps is what keeping the dev services' git reaches: the platform,
// HQ, the dev services over SSH, the container and its state.
type devGitDeps struct {
	client     platform.Client
	httpClient ops.HTTPDoer
	ssh        ops.SSHDeployer
	rt         runtime.Info
	stateDir   string
	// release releases a finished change's leases (followLeaseCompletion,
	// DP-34); nil releases none.
	release leaseBranchRelease
}

// devGitSSHWithin bounds each command the Mate's service runs on a dev
// service: one that does not answer must not hold the service, or the pair's
// lock, for an SSH session's own bound of minutes.
const devGitSSHWithin = 15 * time.Second

// devGitLookEvery is how often the Mate's service looks whether a start that
// did not settle is due again (hqAttemptDue): a read of the clock.
const devGitLookEvery = 10 * time.Second

// KeepMateDevGit keeps every wired pair's dev service on this Mate's HQ
// (keepMateDevGit) for as long as ctx lasts, and releases the leases of every
// branch whose change in HQ is merged or closed (followLeaseCompletion,
// docs/spec-workflows.md §8 DP-34) each time the Mate's server puts a `state`
// frame's changes in place (hq.WatchReplacedFile): the Mate's own service runs it
// beside the server it supervises. env is the container's live env store as
// it is at each call — the unit's own environment carries neither the key nor
// the project — and a container that is no Mate returns at once. Each line
// said goes to log.
func KeepMateDevGit(ctx context.Context, env func() func(string) string, log func(string)) {
	rt := runtime.DetectFrom(env())
	if !rt.InContainer || !rt.MateEnabled {
		return
	}
	id := hqIdentity(rt)
	stateChanged, err := id.State.Watch(ctx)
	if err != nil {
		log(fmt.Sprintf("the dev services' git is looked at only at the service's start and on retries: %v", err))
	}
	ticker := time.NewTicker(devGitLookEvery)
	defer ticker.Stop()
	keepMateDevGit(ctx, liveDevGitDeps(env, log), time.Now(), ticker.C, stateChanged, log)
}

// liveDevGitDeps is devGitDeps from the container's live env store: the
// platform client under the project's key, bound to the organisation that
// owns the project, built again only when the key changes. No key, or a
// client that cannot be built, skips the look; a build that failed is tried
// again at the next look, and said once per key.
func liveDevGitDeps(env func() func(string) string, log func(string)) func(context.Context) (devGitDeps, bool) {
	var (
		key, orgID, said string
		client           platform.Client
	)
	httpClient := &http.Client{Timeout: hqCallTimeout}
	sshDeployer := platform.NewSystemSSHDeployer()
	mounter := platform.NewSystemMounter()
	stateDir := filepath.Join(topology.CodeMountRoot, ".zcp", "state")
	return func(ctx context.Context) (devGitDeps, bool) {
		lookup := env()
		rt := runtime.DetectFrom(lookup)
		current := lookup(hq.EnvKey)
		if current == "" {
			return devGitDeps{}, false
		}
		if current != key {
			readCtx, cancel := context.WithTimeout(ctx, platform.DefaultAPITimeout)
			built, err := platform.NewProjectClient(readCtx, current, mate.ResolveAPIHost(lookup("ZCP_API_HOST")), rt.ProjectID)
			cancel()
			var bound string
			if err == nil {
				bound, err = built.OrgID(ctx)
			}
			if err != nil {
				if said != current {
					said = current
					log(fmt.Sprintf("the dev services' git waits for the platform client: %v", err))
				}
				return devGitDeps{}, false
			}
			key, client, orgID = current, built, bound
		}
		rt.OrgID = orgID
		// A lease's release runs git over its own ssh, never the look's
		// bounded one: its guard and its rewire have bounds of their own.
		release := leaseBranchReleaseFor(client, rt.ProjectID, stateDir, mounter, sshDeployer, rt)
		return devGitDeps{client: client, httpClient: httpClient, ssh: sshDeployer, rt: rt, stateDir: stateDir, release: release}, true
	}
}

// keepMateDevGit keeps every wired pair's dev service on this Mate's HQ for
// as long as ctx lasts and ticks come: the Mate's start (startDevGit) at
// started. A start that did not settle is taken again on a state replacement
// or on hqAttemptDue's backoff, counted in the ticks' own times. Finished
// changes are looked for (followLeaseCompletion) at the
// start — the file may already hold one — and at each wake changed sends,
// never at a tick. deps is asked for at each look; false skips the look. A
// line goes to log when the look of its kind before did not say it.
func keepMateDevGit(ctx context.Context, deps func(context.Context) (devGitDeps, bool), started time.Time, ticks <-chan time.Time, stateChanged <-chan struct{}, log func(string)) {
	var (
		settled bool
		retry   hqPairState
	)
	sayNew := func(said map[string]bool, lines []string) map[string]bool {
		saying := make(map[string]bool, len(lines))
		for _, line := range lines {
			if !said[line] {
				log(line)
			}
			saying[line] = true
		}
		return saying
	}
	var saidStart, saidCompletion map[string]bool
	start := func(d devGitDeps, at time.Time) {
		if settled || !hqAttemptDue(retry, at) {
			return
		}
		var lines []string
		lines, settled = startDevGit(ctx, d.httpClient, d.ssh, d.rt, d.stateDir)
		retry = nextHQAttempt(retry, at, "", false)
		saidStart = sayNew(saidStart, lines)
	}
	complete := func(d devGitDeps) {
		saidCompletion = sayNew(saidCompletion, followLeaseCompletion(ctx, d))
	}
	look := func(turn func(devGitDeps)) {
		d, ok := deps(ctx)
		if !ok {
			return
		}
		d.ssh = boundedSSH{SSHDeployer: d.ssh}
		turn(d)
	}
	look(func(d devGitDeps) {
		start(d, started)
		complete(d)
	})
	for {
		select {
		case <-ctx.Done():
			return
		case at, open := <-ticks:
			if !open {
				return
			}
			look(func(d devGitDeps) { start(d, at) })
		case <-stateChanged:
			look(func(d devGitDeps) {
				complete(d)
				if !settled || devGitStateChanged(d.rt, d.stateDir) {
					settled, retry = false, hqPairState{}
					start(d, time.Now())
				}
			})
		}
	}
}

// devGitStateChanged wakes a settled start only for an application or a
// wired repository's origin that differs from its recorded value.
func devGitStateChanged(rt runtime.Info, stateDir string) bool {
	doc, err := hqIdentity(rt).ReadMateState()
	if err != nil {
		return false
	}
	state, err := doc.Heard()
	if err != nil {
		return false
	}
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil {
		return false
	}
	for _, m := range metas {
		if !hqPairWired(m) {
			continue
		}
		if state.AppID == nil || *state.AppID != m.HQ.AppID {
			return true
		}
		origin, ok := hqOriginIn(state, m.HQ)
		if ok && (m.HQ.Origin == nil || *m.HQ.Origin != origin) {
			return true
		}
	}
	return false
}

// startDevGit is the Mate's start for every wired pair's dev service: a pair
// wired at an HQ the Mate no longer links to is moved to the current one
// (moveFromRetiredHQ), and origin is its repository in HQ, moved there from
// GitHub where an older zcp pointed it (hqPairOrigin) — a move develop's
// start tells the agent, since nothing said here reaches it. settled is false when a pair could not be brought there —
// HQ or the dev service not answering — so the caller tries again.
func startDevGit(
	ctx context.Context,
	httpClient ops.HTTPDoer,
	sshDeployer ops.SSHDeployer,
	rt runtime.Info,
	stateDir string,
) (lines []string, settled bool) {
	hqc, pairs, ok := wiredDevGitPairs(httpClient, sshDeployer, rt, stateDir)
	if !ok {
		return nil, true
	}
	settled = true
	var state hq.MateState
	var stateErr error
	read := false
	self := func() (hq.MateState, error) {
		if !read {
			read = true
			doc, err := hqIdentity(rt).ReadMateState()
			if err == nil {
				state, err = doc.Heard()
				if standing := doc.Standing(); standing != "" {
					lines = append(lines, standing)
				}
			}
			stateErr = mate.StateReadError(err)
		}
		return state, stateErr
	}
	for _, hostname := range pairs {
		line, done := withPairLocked(ctx, stateDir, hostname, func(m *workflow.ServiceMeta) (string, bool) {
			return startPairDevGit(ctx, sshDeployer, stateDir, hqc, self, m)
		})
		settled = settled && done
		if line != "" {
			lines = append(lines, hostname+": "+line)
		}
	}
	return lines, settled
}

// startPairDevGit is one wired pair's turn of startDevGit.
func startPairDevGit(
	ctx context.Context,
	sshDeployer ops.SSHDeployer,
	stateDir string,
	hqc hq.Client,
	self func() (hq.MateState, error),
	m *workflow.ServiceMeta,
) (string, bool) {
	var said []string
	settled := true
	say := func(line string) {
		if line != "" {
			said = append(said, line)
		}
	}
	line, moved := moveFromRetiredHQ(ctx, sshDeployer, stateDir, hqc, m)
	say(line)
	settled = settled && moved
	// Where origin points is the pass's to keep for a pair set up to push; a
	// pair a refused push marked is wired again by the pass instead.
	if m.GitPushState != topology.GitPushConfigured {
		return strings.Join(said, "; "), settled
	}
	if state, err := self(); err == nil {
		recordHQOrigin(stateDir, m, state)
		line := hqPairOrigin(ctx, sshDeployer, stateDir, hqc, m)
		say(line)
		settled = settled && !strings.Contains(line, "could not")
	} else {
		say(fmt.Sprintf("the Mate's state could not be read: %v", err))
		settled = false
	}
	return strings.Join(said, "; "), settled
}

// wiredDevGitPairs is this Mate's HQ and the hostnames of its wired pairs,
// sorted; false when there is no Mate container to keep them in.
func wiredDevGitPairs(httpClient ops.HTTPDoer, sshDeployer ops.SSHDeployer, rt runtime.Info, stateDir string) (hq.Client, []string, bool) {
	if !rt.InContainer || !rt.MateEnabled || sshDeployer == nil {
		return hq.Client{}, nil, false
	}
	hqc, linked := openHQ(httpClient, rt)
	if !linked {
		return hq.Client{}, nil, false
	}
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil {
		return hq.Client{}, nil, false
	}
	var pairs []string
	for _, m := range metas {
		if hqPairWired(m) {
			pairs = append(pairs, m.Hostname)
		}
	}
	sort.Strings(pairs)
	return hqc, pairs, true
}

// withPairLocked runs turn on hostname's record as it is once the pair's
// lock is held. A pair another process runs git on is left to it: turn does
// not run, and the pair is not settled.
func withPairLocked(ctx context.Context, stateDir, hostname string, turn func(*workflow.ServiceMeta) (string, bool)) (string, bool) {
	release, err := workflow.LockPair(ctx, stateDir, hostname, 0)
	if err != nil {
		return "", false
	}
	defer release()
	m, err := workflow.FindServiceMeta(stateDir, hostname)
	if err != nil || !hqPairWired(m) {
		return "", true
	}
	return turn(m)
}

// moveFromRetiredHQ moves a wired pair whose record names its repository on
// another HQ than the one the Mate links to — wired before the Mate moved —
// to the repository's address on the current one: the checkout's origin
// while it still names the retired repository, git's helper, and the record.
// moved is false when the move could not be made; "" and true when there is
// nothing to move. A pair keyed by its dev pool has no checkout of its own —
// the pool's hostname reaches a random container of it, possibly someone's
// lease (docs/spec-workflows.md §8 PS-3) — so only its record follows.
func moveFromRetiredHQ(ctx context.Context, sshDeployer ops.SSHDeployer, stateDir string, hqc hq.Client, m *workflow.ServiceMeta) (string, bool) {
	retired := m.RemoteURL
	if !isRetiredHQRepository(retired, hqc.Address(), m.HQ) {
		return "", true
	}
	current, err := hqc.RepoURL(m.HQ.AppID, m.HQ.Repo)
	if err != nil {
		return fmt.Sprintf("it is wired at %s, an HQ this Mate no longer links to, and its repository on %s cannot be named: %v", topology.RedactRepoURLCredentials(retired), hqc.Address(), err), false
	}
	var out []byte
	if !m.DevIsPool() {
		out, err = sshDeployer.ExecSSH(ctx, m.Hostname, ops.BuildGitHQMoveCommand(hqPairWorkingDir(m), retired, current, hqc.Address()))
	}
	if err != nil {
		return fmt.Sprintf("it is wired at %s, an HQ this Mate no longer links to, and its checkout could not be moved to %s (%v) — retrying", topology.RedactRepoURLCredentials(retired), current, err), false
	}
	if err := workflow.UpsertServiceMeta(stateDir, m.Hostname, func(meta *workflow.ServiceMeta, existed bool) error {
		if !existed || meta.RemoteURL != retired {
			return workflow.ErrSkipWrite
		}
		meta.RemoteURL = current
		return nil
	}); err != nil {
		return fmt.Sprintf("its checkout is on %s now, but recording it failed (%v) — retrying", current, err), false
	}
	m.RemoteURL = current
	// A pool whose default repository is the pair's follows it.
	ops.MovePoolDefaultRepos(stateDir, retired, current)
	if !strings.Contains(string(out), ops.HQMovedMarker) {
		return "", true
	}
	return fmt.Sprintf("it was wired at %s, an HQ this Mate no longer links to, so its checkout's origin is its repository on the current HQ now (%s)", topology.RedactRepoURLCredentials(retired), current), true
}

// isRetiredHQRepository reports whether remote is the repository ref names
// on an HQ other than the one at address: https, a path of
// `/git/[<org>/]<app>/<repo>.git` naming ref's application and repository,
// on another host. Only such an address is a wiring zcp made before the Mate
// moved; any other remote — a GitHub repository an adopt recorded — is the
// person's.
func isRetiredHQRepository(remote, address string, ref *workflow.HQRepoRef) bool {
	if remote == "" || ref == nil || ops.IsHQRemote(remote, address) {
		return false
	}
	u, err := url.Parse(remote)
	if err != nil || !strings.EqualFold(u.Scheme, "https") {
		return false
	}
	segments := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if n := len(segments); n < 3 || n > 4 || segments[0] != "git" {
		return false
	}
	tail := segments[len(segments)-2:]
	return tail[0] == ref.AppID && tail[1] == ref.Repo+".git"
}

// boundedSSH runs each command within devGitSSHWithin.
type boundedSSH struct{ ops.SSHDeployer }

func (b boundedSSH) ExecSSH(ctx context.Context, hostname, command string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, devGitSSHWithin)
	defer cancel()
	return b.SSHDeployer.ExecSSH(ctx, hostname, command)
}
