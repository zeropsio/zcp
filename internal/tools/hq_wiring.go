package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A Mate's pairs keep their code in HQ (SPEC §3.2a): each dev/stage pair has
// a repository in the application HQ holds the Mate in, named after its dev
// hostname, and the pair's checkout descends from the repository's `main`.
// The agent works there with plain git — a branch of its own, pushed, landed
// on `main` by a person's merge. HQ makes the repository on the Mate's asking
// (ensure repo), and every Mate of the application reaches all of its
// repositories over git at `/git/<orgId>/<appId>/<repo>.git`.

// hqStateDir holds one small record per pair: what the last pass did and
// when. It exists for ONE reason — the backoff. Every other fact the pass
// needs (the repository, the branch, the change, whether git-push is
// configured) lives on the ServiceMeta, where the rest of ZCP can see it.
const hqStateDir = "hq"

// hqPairWorkingDir is where the pair's git repository is: the directory its
// dev half deploys from (ServiceMeta.WorkingDirFor) — the code volume's mount
// for a variant-B pair — else where a Zerops runtime's code lives.
func hqPairWorkingDir(m *workflow.ServiceMeta) string {
	if dir := m.WorkingDirFor(m.Hostname); dir != "" {
		return dir
	}
	return topology.CodeMountRoot
}

// hqWaitBackoff is how long a pass waits after an unproductive attempt before
// trying again, doubling to hqWaitBackoffMax. The passes that drive this step
// are agent tool calls, which can arrive in bursts, and a burst must not turn
// into a burst of HQ calls.
const (
	hqWaitBackoff    = 30 * time.Second
	hqWaitBackoffMax = 10 * time.Minute
)

// hqPairState is the per-pair record under hqStateDir.
type hqPairState struct {
	Attempts      int    `json:"attempts"`
	LastAttemptAt string `json:"lastAttemptAt"`
	LastOutcome   string `json:"lastOutcome,omitempty"`
	ProjectInput  string `json:"projectInput,omitempty"`
	BranchRefusal string `json:"branchRefusal,omitempty"`
	CheckoutState string `json:"checkoutState,omitempty"`
}

// hqCallTimeout bounds each call zcp makes to HQ's API; a 503's wait comes on
// top (hq.Client).
const hqCallTimeout = 30 * time.Second

// liveHQIdentity is this container's identity toward HQ — its origin,
// project and key — as the live env store holds it now, so a regenerated key
// is the one presented. It names no organisation.
func liveHQIdentity() hq.Identity {
	return mate.HQIdentity(mate.LiveLookup(mate.LiveEnvStorePath))
}

// hqIdentity is the identity the container rt describes presents to HQ: the
// live one (liveHQIdentity), or the one a test's container is linked to
// (runtime.Info.HQ), naming rt's organisation (runtime.Info.OrgID) for the
// clients that build repository addresses.
func hqIdentity(rt runtime.Info) hq.Identity {
	id := liveHQIdentity()
	if rt.HQ != nil {
		id = hq.Identity{Origin: rt.HQ.Origin, Key: rt.HQ.Key, ProjectID: rt.HQ.ProjectID, GitCredentialPath: rt.HQ.GitCredentialPath}
	}
	id.OrgID = rt.OrgID
	return id
}

// openHQ is a client of this Mate's HQ under its project key, over
// httpClient, as the container rt presents itself (hqIdentity); its
// repository addresses name rt's organisation, and with none they report
// hq.ErrNoOrganisation. False when the container has no key, project or HQ
// origin to present.
func openHQ(httpClient ops.HTTPDoer, rt runtime.Info) (hq.Client, bool) {
	if httpClient == nil {
		return hq.Client{}, false
	}
	client, err := hq.Open(httpClient, hqIdentity(rt))
	return client, err == nil
}

// hqAddress is the address of the HQ the container rt names, "" when it
// names none: what makes a remote recognisable as this Mate's HQ
// (ops.IsHQRemote).
func hqAddress(rt runtime.Info) string {
	return hqIdentity(rt).Origin
}

// hqCredential is a reader of the Git credential the Mate's server keeps from
// HQ in the container rt describes (hq.ReadGitCredential at
// hqIdentity(rt).GitCredentialPath), read at each call; false when there is
// none to read — the Mate is not linked yet, or HQ revoked it. It goes where
// zcp puts it for git — a pool's HQ credential for a lease of an HQ
// repository (ops.PoolHQCredential) — and never into a result, a log or a
// file zcp writes.
func hqCredential(rt runtime.Info) func() (string, bool) {
	return func() (string, bool) {
		credential, err := hq.ReadGitCredential(hqIdentity(rt).GitCredentialPath)
		if err != nil {
			return "", false
		}
		return credential, true
	}
}

// hqRepositoryActions are the public zerops_workflow actions that choose a
// pair's repository, its credential or what builds from it.
var hqRepositoryActions = map[string]bool{"git-push-setup": true, "build-integration": true}

// hqRepositoryActionRefusal is a Mate's answer to hqRepositoryActions: in a
// Mate all three are HQ's. zcp
// wires each pair to its repository in HQ itself (wireHQPair), git reaches it
// with this Mate's own HQ credential, and Core deploys the application's environments from the repository's commits
// — so a remote, a token or a CI chosen here would only diverge from HQ's
// record. A remote of the user's own a pair already pushes to is left alone
// (hqPairNeedsRepository); this action does not move it either.
func hqRepositoryActionRefusal(action string) *mcp.CallToolResult {
	return convertError(platform.NewPlatformError(
		platform.ErrInvalidUsage,
		fmt.Sprintf("%s does not run in a Mate: each pair's repository in HQ, the credential it pushes with and what builds from it are HQ's, and nothing here chooses them.", action),
		"ZCP wires a bootstrapped pair to its repository in HQ on the next bootstrap or adopt pass; work reaches it by plain git from the pair's checkout — a branch of your own, pushed, never `main` and never forced; no token is to be asked for or made up: git answers HQ with this Mate's own credential. A pair already pushing to a remote of the user's own keeps it; moving it is the person's call, not this action's.",
	), WithRecoveryStatus())
}

// reconcileMateRepositories runs both repository reconciles in the one order
// that works: a pair gets its repository in HQ, and only then can the group
// recipe name what builds it. Every bootstrap and adopt pass calls this; both
// halves are silent when there is nothing to do. Pairs an older zcp left
// without stages get them back first, so the recipe names every stage.
func reconcileMateRepositories(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	sshDeployer ops.SSHDeployer,
	rt runtime.Info,
	stateDir string,
) []string {
	if !rt.MateEnabled {
		return reconcileHQRepositories(ctx, client, httpClient, sshDeployer, rt, stateDir, hqPassBackedOff)
	}
	rebuildMateStages(ctx, httpClient, rt, stateDir, recipePairMountRoot)
	lines := reconcileHQRepositories(ctx, client, httpClient, sshDeployer, rt, stateDir, hqPassBackedOff)
	if line := reconcileGroupRecipe(ctx, client, httpClient, rt, stateDir); line != "" {
		lines = append(lines, "the group's recipe: "+line)
	}
	return lines
}

// reconcileHQRepositories gives every bootstrapped pair on this Mate its
// repository in HQ with a checkout on its `main`, and keeps every wired pair
// current: the dev service holds the Git credential as the Mate's file has it now, the change
// on record is asked what became of it, and a pair whose Mate HQ now holds in
// another application is wired there. It pushes nothing: what goes to HQ is
// the agent's git. It is a RECONCILE, not a one-shot step: it runs on every
// bootstrap, adopt and develop start, does nothing for a pair with nothing
// to do, and backs off per pair (pace) — except a credential to renew, which
// git in the dev service cannot do without.
//
// It NEVER blocks bootstrap. Every failure it can meet — a Mate not linked
// yet, HQ refusing or not answering, an SSH hiccup — leaves the pair exactly
// as it was and returns a line saying so. The next pass tries again.
//
// Inside a Mate it also renews the credential of every pool an acquire gave
// it (renewHQPoolCredentials): a lease is held until released, so the next
// acquire may be far off, while its leases need the new credential from
// their next fresh session. A pool that keys a pair is renewed with its pair,
// from the pair's record (renewHQPairCredential), and never twice.
//
// Returns one report line per pair, then per pool, that has something to say,
// by hostname so the message is stable.
func reconcileHQRepositories(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	sshDeployer ops.SSHDeployer,
	rt runtime.Info,
	stateDir string,
	pace hqPassPace,
) []string {
	// Local mode holds no Mate and no container to push from.
	if !rt.InContainer || sshDeployer == nil {
		return nil
	}
	if !rt.MateEnabled {
		return reconcileProjectRepositories(ctx, httpClient, sshDeployer, rt, stateDir, pace)
	}
	hqc, linked := openHQ(httpClient, rt)
	if !linked {
		return nil
	}
	report := reconcileHQPairs(ctx, client, httpClient, sshDeployer, rt, stateDir, hqc, pace)
	return append(report, renewHQPoolCredentials(ctx, client, rt, stateDir, hqc)...)
}

// renewHQPoolCredentials gives every pool zcp put the Mate's Git credential on
// the one the Mate's file holds now (ops.RenewPoolHQCredentials), inside a Mate
// only — outside one zcp never writes a pool's HQ credential
// (ops.PoolHQCredentialEnvKey). Like a dev service's
// renewal it needs no backoff: a pool whose record matches costs no call, and
// one that could not be written is tried again on the next pass. One line per
// pool that could not be.
func renewHQPoolCredentials(ctx context.Context, client platform.Client, rt runtime.Info, stateDir string, hqc hq.Client) []string {
	if !rt.MateEnabled {
		return nil
	}
	var lines []string
	// No credential in the Mate's file is nothing to renew with: the pools
	// keep what they hold until HQ sends one, and the pass says which.
	credential, _, err := hqc.GitCredential()
	if err != nil {
		return poolCredentialsNotRenewed(stateDir, err)
	}
	for _, failure := range ops.RenewPoolHQCredentials(ctx, client, stateDir, credential) {
		lines = append(lines, fmt.Sprintf("%s: HQ sent the Mate a new Git credential, and git in the pool's leases cannot reach HQ until its %s holds it, which could not be written (%v) — retrying on the next pass, and the next acquire of an HQ repository there writes it too.", failure.Pool, ops.PoolHQCredentialEnvKey, failure.Err))
	}
	return lines
}

// poolCredentialsNotRenewed is the pass's one line when the Mate's server
// holds no Git credential to renew the pools with (why): every pool whose
// ZCP_HQ_GIT_TOKEN a renewal would write — one an acquire recorded
// (ops.PoolCredentialRecordPools), and one keying a wired pair, which its
// pair's renewal skips as well (hqPairCredentialStale) — keeps what it holds,
// and an acquire of an HQ repository there is refused until one arrives. None
// when zcp put the credential on no pool.
func poolCredentialsNotRenewed(stateDir string, why error) []string {
	pools := ops.PoolCredentialRecordPools(stateDir)
	if metas, err := workflow.ListServiceMetas(stateDir); err == nil {
		for _, m := range metas {
			if m.DevIsPool() && hqPairWired(m) && m.GitPushState == topology.GitPushConfigured && !slices.Contains(pools, m.Hostname) {
				pools = append(pools, m.Hostname)
			}
		}
	}
	if len(pools) == 0 {
		return nil
	}
	slices.Sort(pools)
	return []string{fmt.Sprintf("%s: %s was not renewed, because %v — the leases there keep the credential they hold, which HQ may have revoked; retrying on the next pass, and an acquire of an HQ repository there is refused until the Mate's server holds one.",
		strings.Join(pools, ", "), ops.PoolHQCredentialEnvKey, why)}
}

// reconcileHQPairs is the pairs' half of the repository pass
// (reconcileHQRepositories), with the HQ client it opened.
func reconcileHQPairs(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	sshDeployer ops.SSHDeployer,
	rt runtime.Info,
	stateDir string,
	hqc hq.Client,
	pace hqPassPace,
) []string {
	// A meta list that cannot be read takes no pair's turn; the changes are
	// the ledger's and the leases', and still take theirs.
	metas, _ := workflow.ListServiceMetas(stateDir)
	address := hqc.Address()
	pending := make([]*workflow.ServiceMeta, 0, len(metas))
	for _, m := range metas {
		if hqPairGetsRepository(m, address, rt) || hqPairWired(m) {
			pending = append(pending, m)
		}
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].Hostname < pending[j].Hostname })

	now := time.Now().UTC()
	self, selfRead := trackedSelf(ctx, hqc)
	var report []string
	for _, m := range pending {
		state := readHQPairState(stateDir, m.Hostname)
		if pace == hqPassBackedOff && !hqAttemptDue(state, now) && !hqPairCredentialStale(m, hqc, rt) {
			continue
		}
		// A pair another process runs git on is left to it, and no attempt
		// is recorded: it stays due for the next pass.
		release, err := workflow.LockPair(ctx, stateDir, m.Hostname, 0)
		if err != nil {
			continue
		}
		outcome, wired := passHQPair(ctx, client, httpClient, sshDeployer, rt, stateDir, hqc, self, address, m.Hostname)
		release()
		// The attempt is recorded even when it had nothing to say: a wired
		// pair with nothing open is the ORDINARY state, and without the
		// backoff every agent tool call would ask HQ about it.
		recordHQAttempt(stateDir, m.Hostname, state, now, outcome, wired)
		if outcome != "" {
			report = append(report, m.Hostname+": "+outcome)
		}
	}
	// The changes are the repositories', not a pair's: their turn comes once
	// a pair's turn read the Mate's state, at develop's start, or — with no
	// pair to take a turn (work only in leases) — at every pass, while a
	// wired pair, the ledger or an HQ lease names a repository.
	if selfRead() || ((pace == hqPassNow || len(pending) == 0) && hqChangesHaveRepo(stateDir, address, pending)) {
		if state, err := self(); err == nil {
			report = append(report, hqChangesPass(ctx, stateDir, hqc, state, pace == hqPassNow)...)
		}
	}
	return report
}

// hqChangesHaveRepo reports whether a changes' turn has a repository to turn
// to: a wired pair among pending, a change the ledger keeps, or a lease zcp
// holds of a repository on this Mate's HQ (address).
func hqChangesHaveRepo(stateDir, address string, pending []*workflow.ServiceMeta) bool {
	return slices.ContainsFunc(pending, hqPairWired) || len(hqChanges(stateDir)) > 0 || len(hqLeaseBranches(stateDir, address)) > 0
}

// hqPassPace is whether a pass keeps each pair's backoff.
type hqPassPace int

const (
	// hqPassBackedOff: a pair is taken only once its backoff is over — the
	// pace of passes that agent tool calls drive in bursts.
	hqPassBackedOff hqPassPace = iota
	// hqPassNow: every pair is taken — develop's start, after which the agent
	// works on the checkout.
	hqPassNow
)

// passHQPair is one pair's turn of a pass, on its record as it is once the
// pass holds its checkout: wired, or kept current.
func passHQPair(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	sshDeployer ops.SSHDeployer,
	rt runtime.Info,
	stateDir string,
	hqc hq.Client,
	self func() (hq.MateState, error),
	address string,
	hostname string,
) (string, bool) {
	m, _ := workflow.FindServiceMeta(stateDir, hostname)
	switch {
	case hqPairGetsRepository(m, address, rt):
		attempt := wireHQPair(ctx, client, httpClient, sshDeployer, rt, stateDir, hqc, self, m, hqRepoNameOf(m))
		return attempt.line, attempt.wired
	case hqPairWired(m):
		takeOverPoolCredential(stateDir, hqc, m)
		if hqPairCredentialStale(m, hqc, rt) {
			if line := renewHQPairCredential(ctx, client, rt.ProjectID, stateDir, hqc, m); line != "" {
				return line, false
			}
		}
		return keepHQPairCurrent(ctx, client, httpClient, sshDeployer, rt, stateDir, hqc, self, m)
	}
	return "", false
}

// hqPairCredentialStale reports whether a wired pair's dev half holds another
// Git credential than the one in the Mate's file: HQ replaced it over the
// link. A pair wired before its record named the credential is renewed once,
// and so is a pair just keyed by its dev pool (the re-key clears the dev
// service's fingerprint). A Mate with no credential in its file has nothing
// to renew with. Outside a Mate zcp never writes a pool's HQ credential
// (docs/spec-workflows.md §8 DP-16), so a pool keying a pair is never stale
// there.
func hqPairCredentialStale(m *workflow.ServiceMeta, hqc hq.Client, rt runtime.Info) bool {
	if m.DevIsPool() && !rt.MateEnabled {
		return false
	}
	if !hqPairWired(m) || m.GitPushState != topology.GitPushConfigured {
		return false
	}
	_, fingerprint, err := hqc.GitCredential()
	return err == nil && m.HQ.CredentialFingerprint != fingerprint
}

// hqPairCredentialKey is the variable a wired pair's credential holder keeps
// the Mate's Git credential under: a dev service's GIT_TOKEN, which its
// sessions' git reads — or, for a pair keyed by its dev pool, the pool's own
// HQ credential (ops.PoolHQCredentialEnvKey), which only a lease of an HQ
// repository reads: the pool's GIT_TOKEN stays the user's, for its leases of
// repositories outside HQ (docs/spec-workflows.md §8 DP-16).
func hqPairCredentialKey(m *workflow.ServiceMeta) string {
	if m.DevIsPool() {
		return ops.PoolHQCredentialEnvKey
	}
	return ops.GitTokenEnvKey
}

// renewHQPairCredential gives a wired pair's dev half the Git credential as
// the Mate's file holds it now, as the service secret its git reads
// (hqPairCredentialKey), and records which one it holds. Only the secret: the
// checkout's branch, origin and helper stay as the agent left them. For a
// pair keyed by its dev pool the secret is the pool's HQ credential, which
// every lease of an HQ repository reads from its next fresh session — written
// by the id the pair records, over no ssh. "" once renewed, else the line
// saying it was not. A pair gone from disk meanwhile (a pool migration
// re-keyed it) keeps no stub of it.
func renewHQPairCredential(ctx context.Context, client platform.Client, projectID, stateDir string, hqc hq.Client, m *workflow.ServiceMeta) string {
	credential, fingerprint, err := hqc.GitCredential()
	if err != nil {
		if m.DevIsPool() {
			return fmt.Sprintf("git in the leases of the dev pool %s cannot reach HQ: %v — retrying on the next pass.", m.Hostname, err)
		}
		return fmt.Sprintf("git in %s's sessions cannot reach HQ: %v — retrying on the next pass.", m.Hostname, err)
	}
	serviceID, err := hqPairCredentialHolder(ctx, client, projectID, m)
	if err == nil && m.DevIsPool() {
		// Under the pool's credential lock, which an acquire on the pool
		// writes under too: one that wrote the credential since the pass read
		// the records left its record, taken over here, never written again.
		var unlock func()
		if unlock, err = ops.LockPoolCredential(ctx, stateDir, m.Hostname); err == nil {
			defer unlock()
			takeOverPoolCredential(stateDir, hqc, m)
			if m.HQ.CredentialFingerprint == fingerprint {
				return ""
			}
		}
	}
	if err == nil {
		_, err = ops.EnvSetService(ctx, client, serviceID, hqPairCredentialKey(m), credential, true)
	}
	if err != nil {
		if m.DevIsPool() {
			return fmt.Sprintf("HQ sent the Mate a new Git credential, and git in the leases of the dev pool %s cannot reach HQ until the pool's %s holds it, which could not be written (%v) — retrying on the next pass; a lease's next fresh session reads it once written.", m.Hostname, ops.PoolHQCredentialEnvKey, err)
		}
		return fmt.Sprintf("HQ sent the Mate a new Git credential, and git in %s's sessions cannot reach HQ until it holds it, which could not be written (%v) — retrying on the next pass; your own shell's git is unaffected.", m.Hostname, err)
	}
	_ = workflow.UpsertServiceMeta(stateDir, m.Hostname, func(meta *workflow.ServiceMeta, existed bool) error {
		if !existed || meta.HQ == nil {
			return workflow.ErrSkipWrite
		}
		meta.HQ.CredentialFingerprint = fingerprint
		return nil
	})
	m.HQ.CredentialFingerprint = fingerprint
	return ""
}

// hqPairCredentialHolder is the service that holds a wired pair's Mate
// credential (hqPairCredentialKey): its dev service, by hostname — or, for a
// pair keyed by its dev pool, the pool, by the id the pair records
// (devPool.serviceId): a pool re-created under the hostname is another
// service, holding nothing zcp wrote. A pair that records no id finds its
// pool by hostname.
func hqPairCredentialHolder(ctx context.Context, client platform.Client, projectID string, m *workflow.ServiceMeta) (string, error) {
	if m.DevIsPool() && m.DevPool.ServiceID != "" {
		return m.DevPool.ServiceID, nil
	}
	svc, err := ops.LookupService(ctx, client, projectID, m.Hostname)
	if err != nil {
		return "", fmt.Errorf("find %s: %w", m.Hostname, err)
	}
	return svc.ID, nil
}

// takeOverPoolCredential makes a pair keyed by its dev pool the one home of
// the fingerprint of the Mate credential zcp put on the pool (docs/spec-workflows.md
// §8 DP-16). An acquire keeps that fingerprint per pool
// (ops.PoolCredentialRecord); once the pool keys a pair, the pair's HQ record
// keeps it, the pass renews the pool from it, and the per-pool record goes —
// so the per-pool renewal (renewHQPoolCredentials) never renews the pool a
// second time. The record is taken over only when it names the pool service
// the pair records; its fingerprint is adopted when it tells more than the
// pair's — the pair names none yet (the re-key clears the dev service's) or
// the record names the credential the Mate's file holds now. A record of
// another pool service is left to the per-pool renewal, which drops it once
// that service is gone; one the pass cannot weigh — no credential in the
// Mate's file, and the pair names another — is kept until it can.
func takeOverPoolCredential(stateDir string, hqc hq.Client, m *workflow.ServiceMeta) {
	if !m.DevIsPool() || m.DevPool.ServiceID == "" || m.HQ == nil {
		return
	}
	serviceID, fingerprint, found := ops.PoolCredentialRecord(stateDir, m.Hostname)
	if !found || serviceID != m.DevPool.ServiceID {
		return
	}
	// No credential in the Mate's file names none to match: the record is
	// adopted only where the pair names none yet. One naming another
	// credential than the pair's cannot be weighed against none, so it stays,
	// for a pass that can read the credential: dropped, the pass would know
	// only the pair's word for what the pool holds.
	_, current, err := hqc.GitCredential()
	if err != nil && m.HQ.CredentialFingerprint != "" && fingerprint != m.HQ.CredentialFingerprint {
		return
	}
	if adoptPoolFingerprint(m.HQ.CredentialFingerprint, fingerprint, current) {
		adopted := false
		err := workflow.UpdateServiceMeta(stateDir, m.Hostname, func(meta *workflow.ServiceMeta) error {
			if !meta.DevIsPool() || meta.DevPool.ServiceID != serviceID || meta.HQ == nil ||
				!adoptPoolFingerprint(meta.HQ.CredentialFingerprint, fingerprint, current) {
				return workflow.ErrSkipWrite
			}
			meta.HQ.CredentialFingerprint, adopted = fingerprint, true
			return nil
		})
		if err != nil {
			// The per-pool record stays the renewal's until a pass can write
			// the pair's.
			return
		}
		if adopted {
			m.HQ.CredentialFingerprint = fingerprint
		}
	}
	_ = ops.ForgetPoolCredentialRecord(stateDir, m.Hostname)
}

// adoptPoolFingerprint reports whether the per-pool record's fingerprint is
// the better word on what the pool holds than the pair's own: the pair names
// none, or the record names the credential the Mate's file holds now.
func adoptPoolFingerprint(pair, record, current string) bool {
	return record != pair && (pair == "" || record == current)
}

// lazySelf reads the Mate's own state from HQ at most once, when first asked:
// one read serves every pair of a pass.
func lazySelf(ctx context.Context, hqc hq.Client) func() (hq.MateState, error) {
	var (
		state hq.MateState
		err   error
		read  bool
	)
	return func() (hq.MateState, error) {
		if !read {
			state, err = hqc.Self(ctx)
			read = true
		}
		return state, err
	}
}

// keepHQPairCurrent is one wired pair's turn of a pass: wired again in the
// application HQ holds the Mate in now, if that moved, how its code is kept
// (recordHQOrigin) and so where its checkout's origin points (hqPairOrigin),
// and its `main` against HQ's (hqPairMain). Its repository's changes are the
// pass's turn after the pairs' (hqChangesPass). wired is whether the pass
// wired it again.
func keepHQPairCurrent(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	sshDeployer ops.SSHDeployer,
	rt runtime.Info,
	stateDir string,
	hqc hq.Client,
	self func() (hq.MateState, error),
	m *workflow.ServiceMeta,
) (string, bool) {
	state, err := self()
	if err != nil {
		// Saying nothing beats saying anything a failed read cannot know.
		return "", false
	}
	if state.AppID == nil {
		return "HQ holds this Mate in no application now, so its repository takes nothing from it — it will once the Mate is in one; nothing else is blocked.", false
	}
	if *state.AppID != m.HQ.AppID {
		attempt := wireHQPair(ctx, client, httpClient, sshDeployer, rt, stateDir, hqc, self, m, m.HQ.Repo)
		return attempt.line, attempt.wired
	}
	recordHQOrigin(stateDir, m, state)
	lines := []string{}
	for _, line := range []string{hqPairOrigin(ctx, sshDeployer, hqc, m), hqPairMain(ctx, sshDeployer, hqc, m)} {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "; "), false
}

// hqPairMain checks a wired pair's local `main` against HQ's
// (ops.BuildMainSyncCommand): a `main` that is only zcp's empty seed is put on
// HQ's; one with commits HQ's lacks is left, with the step that keeps them.
// "" when there is nothing to say — a check that could not run among it: the
// next pass runs it again. A container with no organisation cannot build
// HQ's address, and says so.
//
// A pair keyed by its dev pool has no checkout of its own — the pool's
// hostname answers from a random container, possibly someone's lease — so
// there is no `main` to check: each lease checks its branch out from the
// repository afresh.
func hqPairMain(ctx context.Context, sshDeployer ops.SSHDeployer, hqc hq.Client, m *workflow.ServiceMeta) string {
	if m.DevIsPool() {
		return ""
	}
	dir := hqPairWorkingDir(m)
	remote, err := hqc.RepoURL(m.HQ.AppID, m.HQ.Repo)
	if err != nil {
		return fmt.Sprintf("its %q could not be checked against HQ's: %v", hqBase, err)
	}
	out, err := sshDeployer.ExecSSH(ctx, m.Hostname, ops.BuildMainSyncCommand(dir, remote))
	if err != nil {
		return ""
	}
	state, detail := ops.MainSync(string(out))
	switch state {
	case ops.MainFromSeed:
		return fmt.Sprintf("its %q held only zcp's empty seed and nothing of yours, so it is HQ's %q now (%.12s) and takes HQ's work by fast-forward", hqBase, hqBase, detail)
	case ops.MainSeedInTheWay:
		return fmt.Sprintf("its %q holds only zcp's empty seed, and files the working copy holds uncommitted stand where HQ's %q puts its own: in your own shell on the mounted checkout, commit them on a branch of your own (`git checkout -b <branch> && git add -A && git commit`); the next develop start then puts %q on HQ's", hqBase, hqBase, hqBase)
	case ops.MainOwnCommits:
		return fmt.Sprintf("its %q holds %s commit(s) HQ's %q does not, and %q moves only by a merge: in your own shell on the mounted checkout, keep them on a branch (`git branch <name> main`), put %q on HQ's (`git checkout main && git fetch origin && git reset --keep origin/main`), and carry what they need onto a branch cut from it with `git cherry-pick`", hqBase, detail, hqBase, hqBase, hqBase)
	}
	return ""
}

// rewireHQPair runs the wiring for one pair now, outside the backoff, and
// records the attempt the way a pass does: a stand-up adopting a pair from
// the group recipe wires it at once.
func rewireHQPair(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	sshDeployer ops.SSHDeployer,
	rt runtime.Info,
	stateDir string,
	hqc hq.Client,
	m *workflow.ServiceMeta,
	repoName string,
) hqWiringOutcome {
	attempt := wireHQPair(ctx, client, httpClient, sshDeployer, rt, stateDir, hqc, lazySelf(ctx, hqc), m, repoName)
	recordHQAttempt(stateDir, m.Hostname, readHQPairState(stateDir, m.Hostname), time.Now().UTC(), attempt.line, attempt.wired)
	return attempt
}

// recordHQAttempt writes one attempt onto the pair's backoff record. A pass
// that wired the pair starts the backoff afresh: the failures that grew it
// are over.
func recordHQAttempt(stateDir, hostname string, prior hqPairState, at time.Time, outcome string, wired bool) {
	writeHQPairState(stateDir, hostname, nextHQAttempt(prior, at, outcome, wired))
}

// nextHQAttempt is the shared pacing record for both repository modes.
func nextHQAttempt(prior hqPairState, at time.Time, outcome string, wired bool) hqPairState {
	attempts := prior.Attempts + 1
	if wired {
		attempts = 1
	}
	return hqPairState{
		Attempts:      attempts,
		LastAttemptAt: at.Format(time.RFC3339),
		LastOutcome:   outcome,
	}
}

// hqRepoNameOf is the repository a pair is wired to: the one its record
// names — a stand-up's comes from the group recipe — else its dev hostname.
func hqRepoNameOf(m *workflow.ServiceMeta) string {
	if hqPairWired(m) {
		return m.HQ.Repo
	}
	return m.Hostname
}

// hqPairWired reports whether a pair keeps its code in HQ: it has its
// repository there.
func hqPairWired(m *workflow.ServiceMeta) bool {
	return m != nil && m.HQ != nil && m.HQ.Repo != ""
}

// hqPairGetsRepository is hqPairNeedsRepository for this container: being a
// Mate (runtime.Info.MateEnabled), not holding a key or an HQ origin, is what
// makes HQ the authority (docs/spec-mate.md, "A Mate is HQ's by what it is"),
// so a pair with no HQ record is given a repository only inside a Mate. A
// container that is not a Mate takes the plain-project door
// (reconcileProjectRepositories) and never reaches this pass.
func hqPairGetsRepository(m *workflow.ServiceMeta, address string, rt runtime.Info) bool {
	return hqPairNeedsRepository(m, address) && (rt.MateEnabled || (m != nil && m.HQ != nil))
}

// hqPairNeedsRepository reports whether a pair still needs wiring. One
// repository per pair is enforced HERE, by state rather than by memory: a
// pair is wired exactly when it carries an HQ record (written last, after the
// remote and the branch) and a configured git-push, and no number of passes
// asks HQ for a second one.
//
// A remote with no HQ record may be a wiring that stopped half-way —
// git-push-setup stamps the remote before the branch step runs — and is
// retried when it is this pair's repository on this Mate's HQ (address); any
// other remote is the user's own. Deciding that before HQ is asked matters: HQ
// makes the repository it is asked for.
//
// A pair keyed by its dev pool never needs wiring: wiring sets git-push up on
// the dev service's checkout and puts it on `main`, and a pool holds no
// checkout of its own. The pool migration moves only a pair that already has
// its origin.
func hqPairNeedsRepository(m *workflow.ServiceMeta, address string) bool {
	if m == nil || !m.IsComplete() || m.DevIsPool() {
		return false
	}
	if hqPairWired(m) && m.GitPushState == topology.GitPushConfigured {
		return false
	}
	// A pair the user already pointed at a remote of their own is theirs.
	// ZCP does not move a working repository to HQ behind their back.
	return m.RemoteURL == "" || m.HQ != nil || hqRemoteIsThePairs(m.RemoteURL, address, m.Hostname)
}

// hqRemoteIsThePairs reports whether remote can be the repository HQ hands
// this pair: on this Mate's HQ, and named after the pair's hostname. A remote
// that fails either is one the user chose.
func hqRemoteIsThePairs(remote, address, hostname string) bool {
	if !ops.IsHQRemote(remote, address) {
		return false
	}
	u, err := url.Parse(remote)
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSuffix(path.Base(u.Path), ".git"), hostname)
}

// hqWiringOutcome is what one wiring attempt for a pair ended in.
type hqWiringOutcome struct {
	// line is the report line, "" when there is nothing worth saying.
	line string
	// wired: the pair's meta.HQ is recorded.
	wired bool
	// remedy is the one thing to do before another attempt can wire the pair
	// — a refusal of the branch step no retry changes; "" when a retry is
	// enough.
	remedy string
	// usersOwn: the pair's remote is another repository than the one HQ
	// answers with — the user's own, not a wiring to complete.
	usersOwn bool
}

// wireHQPair gives one pair its repository in HQ: the repository repoName in
// the application HQ holds the Mate in (made if new), git-push to it, the
// checkout on a `main` descending from the repository's, the HQ record, and
// the checkout's origin where the person keeps the code (hqPairOrigin) —
// nothing builds from the repository on the Mate's side: Core deploys the
// application's environments from its commits. repoName is the pair's dev
// hostname for a pair zcp
// bootstrapped, and the repository the group's recipe names for a pair a
// stand-up adopted from it (standup.go). A pair whose Mate HQ now holds in another application
// is wired again there: the repository of the same name in the new
// application, its change in the old one left where it is.
func wireHQPair(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	sshDeployer ops.SSHDeployer,
	rt runtime.Info,
	stateDir string,
	hqc hq.Client,
	self func() (hq.MateState, error),
	m *workflow.ServiceMeta,
	repoName string,
) hqWiringOutcome {
	if m.DevIsPool() {
		return hqWiringOutcome{line: poolPairNotWired(m)}
	}
	if line, remedy := reservedRepository(m.Hostname, repoName); line != "" {
		return hqWiringOutcome{line: line, remedy: remedy}
	}
	callCtx, cancel := context.WithTimeout(ctx, hqCallTimeout)
	repo, err := hqc.EnsureRepo(callCtx, repoName)
	cancel()
	var refused *hq.RefusedError
	switch {
	case errors.As(err, &refused) && refused.Status == 403:
		return hqWiringOutcome{line: fmt.Sprintf("HQ refuses this Mate a repository (%s) — it gives one only to a Mate it holds in an application; nothing else is blocked.", hqRefusalWords(refused))}
	case errors.As(err, &refused):
		return hqWiringOutcome{line: fmt.Sprintf("HQ refused a repository named %q (%s) — retrying on the next pass.", repoName, hqRefusalWords(refused))}
	case err != nil:
		return hqWiringOutcome{line: fmt.Sprintf("could not reach HQ for a repository (%v) — retrying on the next pass.", err)}
	}
	credential, fingerprint, err := hqc.GitCredential()
	if err != nil {
		return hqWiringOutcome{line: fmt.Sprintf("repository %q is ready in HQ but git cannot reach it: %v — retrying on the next pass.", repo.Name, err)}
	}
	remote, err := hqc.RepoURL(repo.AppID, repo.Name)
	if err != nil {
		return hqWiringOutcome{line: fmt.Sprintf("repository %q is ready in HQ but git cannot be pointed at it: %v — retrying on the next pass.", repo.Name, err)}
	}
	// A remote on this Mate's HQ is ours to point again — a wiring that
	// stopped half-way, or the application the Mate left; any other one the
	// user chose, and git-push-setup would rewrite it.
	if m.RemoteURL != "" && !sameGitRepository(m.RemoteURL, remote) && !ops.IsHQRemote(m.RemoteURL, hqc.Address()) {
		return hqWiringOutcome{
			line: fmt.Sprintf("%s already pushes to %s, not to its repository %q in HQ — that remote is left as the user's own.",
				m.Hostname, topology.RedactRepoURLCredentials(m.RemoteURL), repo.Name),
			usersOwn: true,
		}
	}

	// git-push-setup owns the credential, the probe, the origin sync and the
	// meta stamp. Calling it rather than reproducing it is what keeps the
	// credential out of every file ZCP writes: it goes to a sensitive service
	// env on the push source and reaches git through the session credential
	// helper, exactly as a user-driven setup does.
	result, _, _ := confirmGitPushSetupContainer(
		ctx, client, httpClient, sshDeployer, rt.ProjectID, stateDir, hqc.Address(),
		WorkflowInput{Service: m.Hostname, RemoteURL: remote, GitToken: credential},
		m,
	)
	if result != nil && result.IsError {
		return hqWiringOutcome{line: fmt.Sprintf("repository %q is ready in HQ but wiring git-push to it failed — retrying on the next pass.", repo.Name)}
	}

	// The push source's checkout has to DESCEND from the repository's `main`:
	// the pair was git-initialised with a history of its own, and a branch
	// unrelated to `main` cannot land on it. Best-effort: a container that
	// refuses still has its repository, and the next pass tries again.
	if out, branchErr := sshDeployer.ExecSSH(ctx, m.Hostname,
		ops.BuildBaseBranchCommand(hqPairWorkingDir(m))); branchErr != nil {
		prefix := fmt.Sprintf("repository %q is ready in HQ, but %s could not be put on %q", repo.Name, m.Hostname, hqBase)
		refusal := ops.BaseBranchRefusal(string(out))
		if remedy := ops.BaseBranchRefusalRemedy(refusal); remedy != "" {
			return hqWiringOutcome{
				line:   fmt.Sprintf("%s (%s): in %s on %s, %s", prefix, refusal, hqPairWorkingDir(m), m.Hostname, remedy),
				remedy: fmt.Sprintf("In %s on %s, %s", hqPairWorkingDir(m), m.Hostname, remedy),
			}
		}
		return hqWiringOutcome{line: fmt.Sprintf("%s (%v) — a branch cut from it could not land on %q; retrying on the next pass.", prefix, branchErr, hqBase)}
	}
	var (
		left     int
		recorded *workflow.HQRepoRef
	)
	if err := workflow.UpsertServiceMeta(stateDir, m.Hostname, func(meta *workflow.ServiceMeta, existed bool) error {
		if !existed {
			return fmt.Errorf("meta for %q vanished", m.Hostname)
		}
		meta.HQ, left = wiredHQRecord(meta.HQ, repo, fingerprint)
		recorded = meta.HQ
		return nil
	}); err != nil {
		return hqWiringOutcome{line: fmt.Sprintf("repository %q is wired but recording it failed (%v) — the next pass re-reads it.", repo.Name, err)}
	}
	m.HQ = recorded
	// git-push-setup pointed origin at HQ; where the person keeps the code on
	// GitHub, the wiring points it there last.
	if state, err := self(); err == nil {
		recordHQOrigin(stateDir, m, state)
	}

	line := fmt.Sprintf("repository %q wired in HQ; work goes on branches cut from %q and lands on it by a person's merge, never by pushing %q", repo.Name, hqBase, hqBase)
	if left != 0 {
		line += fmt.Sprintf("; this Mate moved to another application, and its change #%d stays in the one it was opened in", left)
	}
	if kept := hqPairOrigin(ctx, sshDeployer, hqc, m); kept != "" {
		line += "; " + kept
	}
	return hqWiringOutcome{line: line, wired: true}
}

// poolPairNotWired is why a pass does not wire a pair keyed by its dev pool —
// reached when HQ holds the Mate in another application than the pair's
// repository: wiring runs git-push setup and git on the dev service's
// checkout, and the pool's hostname reaches a random container of the pool,
// possibly someone's lease. The pair keeps its repository where it is.
func poolPairNotWired(m *workflow.ServiceMeta) string {
	kept := "it has no repository in HQ"
	if hqPairWired(m) {
		kept = fmt.Sprintf("it keeps its repository %q in the application it was wired in", m.HQ.Repo)
	}
	return fmt.Sprintf("the pair keyed by the dev pool %s is not wired again: a pool holds no checkout of its own to wire, and its hostname reaches a random one of its containers; %s, and a lease of the pool checks out whichever repository it is given", m.Hostname, kept)
}

// reservedRepository is why the pair hostname gets no repository repoName in
// HQ, with the one thing to do about it; "" when it may have one. HQ answers
// a repository named hq.RecipeRepo with the application's recipe repository,
// whoever asks, so that name is reserved here: a pair wired to it would push
// its code into the recipe.
func reservedRepository(hostname, repoName string) (line, remedy string) {
	switch {
	case strings.EqualFold(hostname, hq.RecipeRepo):
		remedy = fmt.Sprintf("Rename the service %q: %q is the application's recipe repository, so no service may be named after it.", hostname, hq.RecipeRepo)
	case strings.EqualFold(repoName, hq.RecipeRepo):
		remedy = fmt.Sprintf("Fix the recipe: its buildFromGit for %s names %q, the application's recipe repository, which builds no service.", hostname, hq.RecipeRepo)
	default:
		return "", ""
	}
	return fmt.Sprintf("%s gets no repository in HQ: %q is the application's recipe repository. %s", hostname, hq.RecipeRepo, remedy), remedy
}

// wiredHQRecord is the pair's HQ record once it is wired to repo, its dev
// service given the credential fingerprint names. Wired again to the same
// repository it keeps everything else; wired to another — the Mate moved to
// another application — its change stays with the old application (left is
// the change left there, 0 for none).
func wiredHQRecord(prior *workflow.HQRepoRef, repo hq.Repo, fingerprint string) (record *workflow.HQRepoRef, left int) {
	wiredAt := time.Now().UTC().Format(time.RFC3339)
	if prior != nil && prior.AppID == repo.AppID && prior.Repo == repo.Name {
		kept := *prior
		kept.WiredAt, kept.CredentialFingerprint = wiredAt, fingerprint
		return &kept, 0
	}
	record = &workflow.HQRepoRef{AppID: repo.AppID, Repo: repo.Name, WiredAt: wiredAt, CredentialFingerprint: fingerprint}
	if prior != nil {
		left = prior.Change
	}
	return record, left
}

// hqRefusalWords is HQ's refusal as a line names it: its reason, or its code.
func hqRefusalWords(refused *hq.RefusedError) string {
	if refused.Reason != "" {
		return refused.Reason
	}
	if refused.Code != "" {
		return refused.Code
	}
	return fmt.Sprintf("status %d", refused.Status)
}

// hqAttemptDue applies the backoff: the first pass always runs, and each
// unproductive one pushes the next further out, capped.
func hqAttemptDue(state hqPairState, now time.Time) bool {
	if state.Attempts == 0 || state.LastAttemptAt == "" {
		return true
	}
	last, err := time.Parse(time.RFC3339, state.LastAttemptAt)
	if err != nil {
		return true
	}
	wait := hqWaitBackoff << min(state.Attempts-1, 16)
	if wait > hqWaitBackoffMax || wait <= 0 {
		wait = hqWaitBackoffMax
	}
	return !now.Before(last.Add(wait))
}

func hqPairStatePath(stateDir, hostname string) string {
	return filepath.Join(stateDir, hqStateDir, hostname+".json")
}

// readHQPairState returns the zero state for anything unreadable — a missing
// or corrupt record must make the step RUN, never skip.
func readHQPairState(stateDir, hostname string) hqPairState {
	data, err := os.ReadFile(hqPairStatePath(stateDir, hostname))
	if err != nil {
		return hqPairState{}
	}
	var state hqPairState
	if json.Unmarshal(data, &state) != nil {
		return hqPairState{}
	}
	return state
}

// writeHQPairState records the attempt. Best-effort: a failure to write the
// backoff record costs one extra pass, never a failed bootstrap, so it goes
// to stderr and nowhere else.
func writeHQPairState(stateDir, hostname string, state hqPairState) {
	dir := filepath.Join(stateDir, hqStateDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "zcp: hq state dir: %v\n", err)
		return
	}
	data, err := json.Marshal(state)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zcp: hq state encode: %v\n", err)
		return
	}
	if err := os.WriteFile(hqPairStatePath(stateDir, hostname), data, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "zcp: hq state write: %v\n", err)
	}
}

// forgetHQPairState removes hostname's record: a deleted service's backoff
// must not outlive it.
func forgetHQPairState(stateDir, hostname string) error {
	if err := os.Remove(hqPairStatePath(stateDir, hostname)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove hq state %s: %w", hostname, err)
	}
	return nil
}

// appendRepositoryReport folds the reconcile's lines into a bootstrap
// response's message. Separate from the reconcile so the reconcile stays a
// pure "what happened" reporter with no opinion about where it is rendered.
func appendRepositoryReport(resp *workflow.BootstrapResponse, lines []string) {
	if resp == nil || len(lines) == 0 {
		return
	}
	resp.Message += " HQ — " + strings.Join(lines, "; ") + "."
}

// withRepositoryReport adds the reconcile's lines to a tool result as one
// more text block; a result with nothing to add is returned as it is.
func withRepositoryReport(result *mcp.CallToolResult, lines []string) *mcp.CallToolResult {
	if result == nil || len(lines) == 0 {
		return result
	}
	result.Content = append(result.Content, &mcp.TextContent{Text: "HQ — " + strings.Join(lines, "; ") + "."})
	return result
}

// sameGitRepository reports whether two remote URLs name one repository:
// credentials, a trailing slash or ".git" and the host's case aside.
func sameGitRepository(a, b string) bool {
	return canonicalGitRepository(a) == canonicalGitRepository(b)
}

func canonicalGitRepository(remote string) string {
	remote = topology.CanonicalRepoURL(remote)
	if u, err := url.Parse(remote); err == nil && u.Host != "" {
		u.User = nil
		u.Host = strings.ToLower(u.Host)
		return u.String()
	}
	return remote
}
