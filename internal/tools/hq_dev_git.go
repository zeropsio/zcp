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

// A wired pair's dev service reaches HQ with plain git: origin is the pair's
// repository at HQ's address, git's persisted helper answers that host, and
// the helper reads the Mate's Git credential the dev service holds as its
// GIT_TOKEN. All three are copies of what the Mate holds — the HQ it links
// to, the credential HQ sends over the link — and go stale the moment
// either moves: a Mate moved to another HQ, and every link HQ opens, which
// sends a new credential and revokes the one before. The repository pass
// renews them, but runs only when an agent bootstraps, adopts or starts
// develop; the Mate's own service keeps them current in between
// (KeepMateDevGit).

// devGitDeps is what keeping the dev services' git reaches: the platform,
// HQ, the dev services over SSH, the container and its state.
type devGitDeps struct {
	client     platform.Client
	httpClient ops.HTTPDoer
	ssh        ops.SSHDeployer
	rt         runtime.Info
	stateDir   string
}

// devGitSSHWithin bounds each command the Mate's service runs on a dev
// service: one that does not answer must not hold the service, or the pair's
// lock, for an SSH session's own bound of minutes.
const devGitSSHWithin = 15 * time.Second

// devGitLookEvery is how often the Mate's service looks whether HQ sent a
// new credential: a read of one small file and the pairs' records.
const devGitLookEvery = 10 * time.Second

// KeepMateDevGit keeps every wired pair's dev service on this Mate's HQ
// (keepMateDevGit) for as long as ctx lasts: the Mate's own service runs it
// beside the server it supervises. env is the container's live env store as
// it is at each call — the unit's own environment carries neither the key nor
// the project — and a container that is no Mate returns at once. Each line
// said goes to log.
func KeepMateDevGit(ctx context.Context, env func() func(string) string, log func(string)) {
	if rt := runtime.DetectFrom(env()); !rt.InContainer || !rt.MateEnabled {
		return
	}
	ticker := time.NewTicker(devGitLookEvery)
	defer ticker.Stop()
	keepMateDevGit(ctx, liveDevGitDeps(env, log), time.Now(), ticker.C, log)
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
		return devGitDeps{client: client, httpClient: httpClient, ssh: sshDeployer, rt: rt, stateDir: stateDir}, true
	}
}

// keepMateDevGit keeps every wired pair's dev service on this Mate's HQ for
// as long as ctx lasts and ticks come: the Mate's start (startDevGit) at
// started, then each new credential HQ sends (followMateCredential) at the
// tick after it lands. A start that did not settle — a dev service stopped,
// HQ not answering — is taken again on hqAttemptDue's backoff, counted in
// the ticks' own times, while every tick between still follows the
// credential. deps is asked for at each look; false skips the look. A line
// goes to log when the look before did not say it.
func keepMateDevGit(ctx context.Context, deps func(context.Context) (devGitDeps, bool), started time.Time, ticks <-chan time.Time, log func(string)) {
	var (
		settled bool
		retry   hqPairState
		said    map[string]bool
		// renewals are the pairs whose last renewal failed, with its record:
		// each is tried again on hqAttemptDue's backoff.
		renewals = map[string]hqPairState{}
	)
	look := func(at time.Time) {
		d, ok := deps(ctx)
		if !ok {
			return
		}
		d.ssh = boundedSSH{SSHDeployer: d.ssh}
		var lines []string
		if settled || !hqAttemptDue(retry, at) {
			lines = followMateCredential(ctx, d.client, d.httpClient, d.ssh, d.rt, d.stateDir, at, renewals)
		} else {
			lines, settled = startDevGit(ctx, d.client, d.httpClient, d.ssh, d.rt, d.stateDir)
			retry = nextHQAttempt(retry, at, "", false)
		}
		saying := make(map[string]bool, len(lines))
		for _, line := range lines {
			if !said[line] {
				log(line)
			}
			saying[line] = true
		}
		said = saying
	}
	look(started)
	for {
		select {
		case <-ctx.Done():
			return
		case at, open := <-ticks:
			if !open {
				return
			}
			look(at)
		}
	}
}

// startDevGit is the Mate's start for every wired pair's dev service: a pair
// wired at an HQ the Mate no longer links to is moved to the current one
// (moveFromRetiredHQ), its credential is the Mate's current one
// (renewHQPairCredential), and origin is where HQ says its code is kept
// (hqPairOrigin). settled is false when a pair could not be brought there —
// HQ or the dev service not answering — so the caller tries again.
func startDevGit(
	ctx context.Context,
	client platform.Client,
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
	self := lazySelf(ctx, hqc)
	for _, hostname := range pairs {
		line, done := withPairLocked(ctx, stateDir, hostname, func(m *workflow.ServiceMeta) (string, bool) {
			return startPairDevGit(ctx, client, sshDeployer, rt, stateDir, hqc, self, m)
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
	client platform.Client,
	sshDeployer ops.SSHDeployer,
	rt runtime.Info,
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
	if hqPairCredentialStale(m, hqc) {
		line := renewHQPairCredential(ctx, client, rt.ProjectID, stateDir, hqc, m)
		say(line)
		settled = settled && line == ""
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
		line := hqPairOrigin(ctx, sshDeployer, hqc, m)
		say(line)
		settled = settled && !strings.Contains(line, "could not")
	} else {
		settled = false
	}
	return strings.Join(said, "; "), settled
}

// followMateCredential gives each wired pair's dev service the Mate's Git
// credential once HQ sent a new one (hqPairCredentialStale): a read of the
// file and the records, and nothing else, while none is new. A pair whose
// renewal failed is in renewals and tried again only once hqAttemptDue says
// so, counted at at, so a dev service that cannot take it is not asked of
// the platform at every look.
func followMateCredential(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	sshDeployer ops.SSHDeployer,
	rt runtime.Info,
	stateDir string,
	at time.Time,
	renewals map[string]hqPairState,
) []string {
	hqc, pairs, ok := wiredDevGitPairs(httpClient, sshDeployer, rt, stateDir)
	if !ok {
		return nil
	}
	var lines []string
	for _, hostname := range pairs {
		if failed, ok := renewals[hostname]; ok && !hqAttemptDue(failed, at) {
			continue
		}
		line, _ := withPairLocked(ctx, stateDir, hostname, func(m *workflow.ServiceMeta) (string, bool) {
			if !hqPairCredentialStale(m, hqc) {
				return "", true
			}
			return renewHQPairCredential(ctx, client, rt.ProjectID, stateDir, hqc, m), true
		})
		if line == "" {
			delete(renewals, hostname)
			continue
		}
		renewals[hostname] = nextHQAttempt(renewals[hostname], at, line, false)
		lines = append(lines, hostname+": "+line)
	}
	return lines
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
// nothing to move.
func moveFromRetiredHQ(ctx context.Context, sshDeployer ops.SSHDeployer, stateDir string, hqc hq.Client, m *workflow.ServiceMeta) (string, bool) {
	retired := m.RemoteURL
	if !isRetiredHQRepository(retired, hqc.Address(), m.HQ) {
		return "", true
	}
	current, err := hqc.RepoURL(m.HQ.AppID, m.HQ.Repo)
	if err != nil {
		return fmt.Sprintf("it is wired at %s, an HQ this Mate no longer links to, and its repository on %s cannot be named: %v", topology.RedactRepoURLCredentials(retired), hqc.Address(), err), false
	}
	out, err := sshDeployer.ExecSSH(ctx, m.Hostname, ops.BuildGitHQMoveCommand(hqPairWorkingDir(m), retired, current, hqc.Address()))
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
