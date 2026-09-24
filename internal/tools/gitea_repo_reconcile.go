package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// giteaStateDir holds one small record per pair: what the last pass did and
// when. It exists for ONE reason — the backoff. Every other fact the step
// needs (the repository, the branch, whether git-push is configured) lives on
// the ServiceMeta, where the rest of ZCP can already see it.
const giteaStateDir = "gitea"

// giteaPairWorkingDir is where a Zerops runtime's code lives, and so where
// the pair's git repository is — the same default the git-push deploy uses.
const giteaPairWorkingDir = "/var/www"

// giteaPairMountRoot is where the Mate mounts each pair's working directory,
// one per hostname (ops.MountService). It is how a reconcile running in the
// `zcp` container reads a pair's files without an SSH round trip.
const giteaPairMountRoot = "/var/www"

// giteaProtectedBase is the branch a Mate lands on and never pushes: every
// repository's `main` is protected (docs/vocabulary.md). It is the fallback
// when the broker's answer named no default branch.
const giteaProtectedBase = "main"

// giteaWaitBackoff is how long a pass waits after an unproductive attempt
// before trying again, doubling to giteaWaitBackoffMax. At sign-up the three
// variables can land minutes after the Mate is up (guide 2.1), and the passes
// that drive this step are agent tool calls — which can arrive in bursts. A
// burst must not turn into a burst of broker calls.
const (
	giteaWaitBackoff    = 30 * time.Second
	giteaWaitBackoffMax = 10 * time.Minute
)

// giteaPairState is the per-pair record under giteaStateDir.
type giteaPairState struct {
	Attempts      int    `json:"attempts"`
	LastAttemptAt string `json:"lastAttemptAt"`
	LastOutcome   string `json:"lastOutcome,omitempty"`
}

// giteaEnvLookup resolves the Mate's Gitea variables. The container's LIVE
// env store comes first and the process environment second, and the order is
// load-bearing: the platform rewrites the store within seconds of a service
// env change, but a running process keeps the environment it started with
// until it restarts (measured 2026-09-16). A step that only read os.Getenv
// would wait forever for variables that arrived ten minutes ago.
func giteaEnvLookup(liveEnvPath string) func(string) string {
	live := map[string]string{}
	if lines, err := mate.LoadLiveEnv(liveEnvPath); err == nil {
		for _, line := range lines {
			if key, value, found := strings.Cut(line, "="); found {
				live[key] = value
			}
		}
	}
	return func(key string) string {
		if v, ok := live[key]; ok && v != "" {
			return v
		}
		return os.Getenv(key)
	}
}

// reconcileGiteaRepositories gives every bootstrapped pair on this Mate its
// repository on the account's Gitea and its own branch to work on (guide
// 2.1, A1). It is a RECONCILE, not a one-shot step: it runs on every
// bootstrap and status pass, does nothing for a pair already wired, and backs
// off when the environment has not arrived yet.
//
// It NEVER blocks bootstrap. Every failure it can meet — the variables not
// written yet, a broker that refuses (409 taken, 403 not registered), a
// broker that is down, an SSH hiccup — leaves the pair exactly as it was,
// with a working dev pair and no remote, and returns a line saying so. The
// next pass tries again.
//
// Returns one report line per pair that has something to say, oldest-first by
// hostname so the message is stable.
func reconcileGiteaRepositories(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	sshDeployer ops.SSHDeployer,
	rt runtime.Info,
	stateDir string,
	liveEnvPath string,
) []string {
	// Local mode holds no Mate environment and no container to push from.
	if !rt.InContainer || sshDeployer == nil {
		return nil
	}
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil || len(metas) == 0 {
		return nil
	}

	wiring := ops.ReadGiteaWiring(giteaEnvLookup(liveEnvPath))
	org := knownGiteaOrg(metas)
	pending := make([]*workflow.ServiceMeta, 0, len(metas))
	for _, m := range metas {
		if giteaPairNeedsRepository(m, wiring.GiteaURL, org) || giteaPairNeedsPullRequest(m) ||
			giteaPairNeedsPullRequestOutcome(m) {
			pending = append(pending, m)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].Hostname < pending[j].Hostname })

	now := time.Now().UTC()

	var report []string
	for _, m := range pending {
		state := readGiteaPairState(stateDir, m.Hostname)
		if !giteaAttemptDue(state, now) {
			continue
		}
		// Three reasons a pair is here, in the order they arise: it has no
		// repository yet; it has one and no pull request; or it has both, and
		// the only open question is what became of the request.
		var outcome string
		attempts := state.Attempts + 1
		switch {
		case giteaPairNeedsRepository(m, wiring.GiteaURL, org):
			var wired bool
			outcome, wired = reconcileOneGiteaPair(ctx, client, httpClient, sshDeployer, rt, stateDir, wiring, m)
			// A pair that just got wired starts its backoff afresh: the
			// failures that grew it are over, and its pull request is next.
			if wired {
				attempts = 1
			}
		case giteaPairNeedsPullRequest(m):
			outcome = reconcileGiteaPairPullRequest(ctx, httpClient, stateDir, wiring, m)
		default:
			outcome = readGiteaPairPullRequestOutcome(ctx, httpClient, sshDeployer, stateDir, wiring, m)
		}
		// The attempt is recorded even when it had nothing to say: a wired
		// pair that has not pushed yet is the ORDINARY state, and without the
		// backoff every agent tool call would ask Gitea about its branch.
		writeGiteaPairState(stateDir, m.Hostname, giteaPairState{
			Attempts:      attempts,
			LastAttemptAt: now.Format(time.RFC3339),
			LastOutcome:   outcome,
		})
		if outcome == "" {
			continue
		}
		report = append(report, m.Hostname+": "+outcome)
	}
	return report
}

// giteaPairNeedsRepository reports whether this pair still has something to
// do. One repository per pair is enforced HERE, by state rather than by
// memory: a pair is wired exactly when it carries a Gitea ref (written last,
// after the remote and the branch) and a configured git-push, and no number
// of passes will ask the broker for a second one.
//
// giteaURL is this Mate's Gitea and org the group's org there when a wired
// pair already names it, "" otherwise. A remote with no Gitea ref may be a
// wiring that stopped half-way — git-push-setup stamps the remote before the
// branch step runs — and is retried; any other remote is the user's own
// (giteaRemoteIsThePairs). Deciding that before the broker is asked matters:
// the broker CREATES the repository it is asked for.
func giteaPairNeedsRepository(m *workflow.ServiceMeta, giteaURL, org string) bool {
	if m == nil || !m.IsComplete() {
		return false
	}
	if m.Gitea != nil && m.Gitea.FullName != "" && m.GitPushState == topology.GitPushConfigured {
		return false
	}
	// A pair the user already pointed at a remote of their own is theirs.
	// ZCP does not move a working repository to Gitea behind their back.
	return m.RemoteURL == "" || m.Gitea != nil || giteaRemoteIsThePairs(m.RemoteURL, giteaURL, m.Hostname, org)
}

// giteaRemoteIsThePairs reports whether remote can be the repository the
// broker hands this pair: on this Mate's Gitea, named after the pair's
// hostname, and in the group's org when org is known. A remote that fails any
// of those is one the user chose. The broker's answer is still the authority
// on the org when it is not known here (reconcileOneGiteaPair).
func giteaRemoteIsThePairs(remote, giteaURL, hostname, org string) bool {
	if topology.ClassifyGitHost(remote, giteaURL) != topology.GitHostGitea {
		return false
	}
	path := canonicalGitRepository(remote)
	if u, err := url.Parse(path); err == nil && u.Host != "" {
		path = u.Path
	} else if i := strings.LastIndex(path, ":"); i >= 0 {
		path = path[i+1:]
	}
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) < 2 || !strings.EqualFold(segments[len(segments)-1], hostname) {
		return false
	}
	return org == "" || strings.EqualFold(segments[len(segments)-2], org)
}

// knownGiteaOrg is the group's org on its Gitea as a wired pair records it,
// "" while no pair is wired.
func knownGiteaOrg(metas []*workflow.ServiceMeta) string {
	for _, m := range metas {
		if m == nil || m.Gitea == nil {
			continue
		}
		if org, _, ok := strings.Cut(m.Gitea.FullName, "/"); ok && org != "" {
			return org
		}
	}
	return ""
}

// giteaPairNeedsPullRequest reports whether a pair A1 already wired is still
// missing the request its branch lands through. Stateful like the repository:
// the number is recorded on the pair, so a Mate that has one asks Gitea
// nothing on any later pass.
func giteaPairNeedsPullRequest(m *workflow.ServiceMeta) bool {
	return m != nil && m.IsComplete() && m.Gitea != nil &&
		m.Gitea.FullName != "" && m.Gitea.Branch != "" && m.Gitea.PullRequest == 0
}

// giteaPairNeedsPullRequestOutcome reports whether a pair that has both a
// repository and a request still has a question worth asking Gitea.
//
// It always does, once, per backoff window: the request's number is recorded
// and never re-derived, which is right for the number and wrong for its fate.
// A merge happens in Gitea's own UI, from a colleague, from a script — none
// of them passes through this process, so being told is not something that
// can be relied on and asking is. The backoff is what keeps that affordable:
// a settled pair reaches Gitea once per window, not once per tool call.
func giteaPairNeedsPullRequestOutcome(m *workflow.ServiceMeta) bool {
	return m != nil && m.Gitea != nil && m.Gitea.FullName != "" && m.Gitea.PullRequest != 0
}

// reconcileOneGiteaPair does the work for one pair and returns a report line
// ("" when there is nothing worth saying) and whether the pair ended wired —
// its meta.Gitea recorded. Never returns an error: every outcome is
// reportable.
func reconcileOneGiteaPair(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	sshDeployer ops.SSHDeployer,
	rt runtime.Info,
	stateDir string,
	wiring ops.GiteaWiring,
	m *workflow.ServiceMeta,
) (string, bool) {
	if !wiring.Ready() {
		return "waiting for Gitea (" + strings.Join(wiring.MissingKeys(), ", ") + " not on this service yet)", false
	}

	// Who this Mate is on that Gitea. The login names the branch, so it has
	// to be known before anything is pushed.
	identity, identityErr := ops.DeriveGiteaIdentity(ctx, httpClient, wiring.GiteaURL, wiring.Token)
	if identityErr != nil {
		return fmt.Sprintf("waiting for Gitea (could not read this Mate's bot identity: %v)", identityErr), false
	}
	branch := ops.GiteaMateBranch(identity.Name)

	repo, repoErr := ops.RequestMateRepository(ctx, httpClient, wiring.BrokerURL, wiring.Token, m.Hostname)
	switch {
	case errors.Is(repoErr, ops.ErrRepositoryTaken):
		return fmt.Sprintf("the broker refuses a repository named %q — it exists in the group's org and this Mate is not a collaborator on it. Rename the service, or have someone add this Mate's bot to that repository.", m.Hostname), false
	case errors.Is(repoErr, ops.ErrNotRegistered):
		return "the broker does not know this project yet (not a registered Mate) — it will once the group is registered; nothing else is blocked.", false
	case repoErr != nil:
		return fmt.Sprintf("could not reach the broker for a repository (%v) — retrying on the next pass.", repoErr), false
	}
	// A remote on this Gitea counts as a wiring that stopped half-way only
	// when it is THIS pair's repository; any other one the user chose, and
	// git-push-setup would rewrite it.
	if m.RemoteURL != "" && !sameGitRepository(m.RemoteURL, repo.CloneURL) {
		return fmt.Sprintf("%s already pushes to %s, not to its repository %s — that remote is left as the user's own.",
			m.Hostname, topology.RedactRepoURLCredentials(m.RemoteURL), repo.FullName), false
	}

	// git-push-setup owns the credential, the probe, the origin sync and the
	// meta stamp. Calling it rather than reproducing it is what keeps the
	// token out of every file ZCP writes: it goes to a sensitive service env
	// on the push source and reaches git through the session credential
	// helper, exactly as a user-driven setup does.
	result, _, _ := confirmGitPushSetupContainer(
		ctx, client, httpClient, sshDeployer, rt.ProjectID, stateDir, wiring.GiteaURL,
		WorkflowInput{Service: m.Hostname, RemoteURL: repo.CloneURL, GitToken: wiring.Token},
		m,
	)
	if result != nil && result.IsError {
		return fmt.Sprintf("repository %s is ready but wiring git-push to it failed — retrying on the next pass.", repo.FullName), false
	}

	base := repo.DefaultBranch
	if base == "" {
		base = giteaProtectedBase
	}

	// The push source works ON that branch from now on, and the branch has to
	// DESCEND from the repository's protected base. git-push-setup owns the
	// remote and the credential but never touches branches, and the pair was
	// git-initialised with a history of its own — so without this `git push
	// -u origin mate/{bot}` has no local ref to push (the refspec fails), and
	// a branch pushed from an unrelated history makes Gitea refuse `merge`
	// and `squash` on the pull request. Best-effort: a container that refuses
	// still has its repository, and the next pass tries again.
	if out, branchErr := sshDeployer.ExecSSH(ctx, m.Hostname,
		ops.BuildGiteaMateBranchCommand(giteaPairWorkingDir, branch, base)); branchErr != nil {
		prefix := fmt.Sprintf("repository %s is ready, but %s could not be put on %q branched off %q", repo.FullName, m.Hostname, branch, base)
		refusal := ops.GiteaBranchRefusal(string(out))
		if remedy := ops.GiteaBranchRefusalRemedy(refusal, branch, base); remedy != "" {
			return fmt.Sprintf("%s (%s): in %s on %s, %s", prefix, refusal, giteaPairWorkingDir, m.Hostname, remedy), false
		}
		return fmt.Sprintf("%s (%v) — a push would have nothing to send, or nothing Gitea could merge; retrying on the next pass.", prefix, branchErr), false
	}
	if err := workflow.UpsertServiceMeta(stateDir, m.Hostname, func(meta *workflow.ServiceMeta, existed bool) error {
		if !existed {
			return fmt.Errorf("meta for %q vanished", m.Hostname)
		}
		meta.Gitea = &workflow.GiteaRepoRef{
			FullName:      repo.FullName,
			Branch:        branch,
			DefaultBranch: base,
			RequestedAt:   time.Now().UTC().Format(time.RFC3339),
		}
		return nil
	}); err != nil {
		return fmt.Sprintf("repository %s is wired but recording it failed (%v) — the next pass re-reads it.", repo.FullName, err), false
	}

	// A3: the workflow that ships this repository's code to the group's stage
	// has to BE in the repository. Nothing else in the backbone writes it —
	// build-integration only ever offered it as text for an agent to copy —
	// so a Mate's repository never carried one and the runner path never ran.
	// Written before the first push, content-idempotent, and reported rather
	// than fatal: a pair whose container refused the write still has its
	// repository, and the next pass writes it again.
	workflowNote := ""
	if _, emitErr := sshDeployer.ExecSSH(ctx, m.Hostname, ops.BuildWriteRepoFileCommand(
		giteaPairWorkingDir, giteaWorkflowFilePath, giteaWorkflowYAML(),
	)); emitErr != nil {
		workflowNote = fmt.Sprintf("; %s could not be written (%v) — nothing deploys the group's stage until it is there", giteaWorkflowFilePath, emitErr)
	}

	line := fmt.Sprintf("repository %s wired; this Mate works on %q and lands on %q through a pull request (never pushing %s directly)", repo.FullName, branch, base, base) + workflowNote
	// The branch exists only locally at this point, so this ordinarily finds
	// nothing — it is here for the pair whose branch a previous generation
	// already pushed. The request's real triggers are the git-push deploy and
	// the catch-up pass; both share this owner, and both record the number.
	m.Gitea = &workflow.GiteaRepoRef{FullName: repo.FullName, Branch: branch, DefaultBranch: base}
	if ref := openGiteaPairPullRequest(ctx, httpClient, wiring, stateDir, m); ref != nil {
		verb := "tracked by"
		if ref.Created {
			verb = "opened as"
		}
		line += fmt.Sprintf("; %s pull request #%d", verb, ref.Number)
	}
	return line, true
}

// giteaAttemptDue applies the backoff: the first pass always runs, and each
// unproductive one pushes the next further out, capped.
func giteaAttemptDue(state giteaPairState, now time.Time) bool {
	if state.Attempts == 0 || state.LastAttemptAt == "" {
		return true
	}
	last, err := time.Parse(time.RFC3339, state.LastAttemptAt)
	if err != nil {
		return true
	}
	wait := giteaWaitBackoff << min(state.Attempts-1, 16)
	if wait > giteaWaitBackoffMax || wait <= 0 {
		wait = giteaWaitBackoffMax
	}
	return !now.Before(last.Add(wait))
}

func giteaPairStatePath(stateDir, hostname string) string {
	return filepath.Join(stateDir, giteaStateDir, hostname+".json")
}

// readGiteaPairState returns the zero state for anything unreadable — a
// missing or corrupt record must make the step RUN, never skip.
func readGiteaPairState(stateDir, hostname string) giteaPairState {
	data, err := os.ReadFile(giteaPairStatePath(stateDir, hostname))
	if err != nil {
		return giteaPairState{}
	}
	var state giteaPairState
	if json.Unmarshal(data, &state) != nil {
		return giteaPairState{}
	}
	return state
}

// writeGiteaPairState records the attempt. Best-effort: a failure to write
// the backoff record costs one extra pass, never a failed bootstrap, so it
// goes to stderr and nowhere else.
func writeGiteaPairState(stateDir, hostname string, state giteaPairState) {
	dir := filepath.Join(stateDir, giteaStateDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "zcp: gitea state dir: %v\n", err)
		return
	}
	data, err := json.Marshal(state)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zcp: gitea state encode: %v\n", err)
		return
	}
	if err := os.WriteFile(giteaPairStatePath(stateDir, hostname), data, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "zcp: gitea state write: %v\n", err)
	}
}

// appendGiteaReport folds the reconcile's lines into a bootstrap response's
// message. Separate from the reconcile so the reconcile stays a pure
// "what happened" reporter with no opinion about where it is rendered.
func appendGiteaReport(resp *workflow.BootstrapResponse, lines []string) {
	if resp == nil || len(lines) == 0 {
		return
	}
	resp.Message += " Gitea — " + strings.Join(lines, "; ") + "."
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
