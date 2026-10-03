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
	"sort"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A Mate's pairs deliver their code to HQ (SPEC §3.2a): each dev/stage pair
// has a repository in the application HQ holds the Mate in, named after its
// dev hostname, and the Mate works on a branch of its own that descends from
// the repository's `main`. HQ makes the repository on the Mate's asking
// (ensure repo), and every Mate of the application reaches all of its
// repositories over git at `/git/<appId>/<repo>.git`.

// hqStateDir holds one small record per pair: what the last pass did and
// when. It exists for ONE reason — the backoff. Every other fact the pass
// needs (the repository, the branch, the change, whether git-push is
// configured) lives on the ServiceMeta, where the rest of ZCP can see it.
const hqStateDir = "hq"

// hqPairWorkingDir is where a Zerops runtime's code lives, and so where the
// pair's git repository is — the same default the git-push deploy uses.
const hqPairWorkingDir = "/var/www"

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
}

// hqCallTimeout bounds each call zcp makes to HQ's API; a 503's wait comes on
// top (hq.Client).
const hqCallTimeout = 30 * time.Second

// openHQ is a client of the HQ this Mate enrolled with (hq.EnrollmentPath),
// over httpClient; false when the Mate is not enrolled yet — it then delivers
// nothing, the way a Mate without its forge wiring did.
func openHQ(httpClient ops.HTTPDoer) (hq.Client, bool) {
	if httpClient == nil {
		return hq.Client{}, false
	}
	client, err := hq.Open(httpClient, hq.EnrollmentPath())
	return client, err == nil
}

// hqWired reports whether this Mate delivers through HQ: it holds an
// enrollment with its org's official HQ.
func hqWired() bool {
	_, found, err := hq.LoadEnrollment(hq.EnrollmentPath())
	return err == nil && found
}

// hqAddress is the address of the HQ this Mate enrolled with, "" when it has
// none: what makes a remote recognisable as this Mate's HQ (ops.IsHQRemote).
func hqAddress() string {
	kept, found, err := hq.LoadEnrollment(hq.EnrollmentPath())
	if err != nil || !found {
		return ""
	}
	return strings.TrimRight(kept.HQ, "/")
}

// hqMateBranch is the Mate's local branch in a pair's checkout: mate/<its
// project id>. Each change's own branch at HQ is cut from it at its push
// (hq.Client.ChangeBranch).
func hqMateBranch(client hq.Client) string { return "mate/" + client.ProjectID() }

// reconcileMateRepositories runs both repository reconciles in the one order
// that works: a pair gets its repository in HQ, and only then can the group
// recipe name what builds it. Every bootstrap and adopt pass calls this; both
// halves are silent when there is nothing to do.
func reconcileMateRepositories(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	sshDeployer ops.SSHDeployer,
	rt runtime.Info,
	stateDir string,
) []string {
	lines := reconcileHQRepositories(ctx, client, httpClient, sshDeployer, rt, stateDir)
	if line := reconcileGroupRecipe(ctx, client, httpClient, rt, stateDir); line != "" {
		lines = append(lines, "the group's recipe: "+line)
	}
	return lines
}

// reconcileHQRepositories gives every bootstrapped pair on this Mate its
// repository in HQ and its own branch to work on, and keeps every wired pair
// current: a delivery HQ could not be reached for is finished, the change on
// record is asked what became of it, and a pair whose Mate HQ now holds in
// another application is wired there. It is a RECONCILE, not a one-shot step:
// it runs on every bootstrap and adopt pass, does nothing for a pair with
// nothing to do, and backs off per pair.
//
// It NEVER blocks bootstrap. Every failure it can meet — a Mate not enrolled
// yet, HQ refusing or not answering, an SSH hiccup — leaves the pair exactly
// as it was and returns a line saying so. The next pass tries again.
//
// Returns one report line per pair that has something to say, by hostname so
// the message is stable.
func reconcileHQRepositories(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	sshDeployer ops.SSHDeployer,
	rt runtime.Info,
	stateDir string,
) []string {
	// Local mode holds no Mate and no container to push from.
	if !rt.InContainer || sshDeployer == nil {
		return nil
	}
	hqc, enrolled := openHQ(httpClient)
	if !enrolled {
		return nil
	}
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil || len(metas) == 0 {
		return nil
	}
	address := hqc.Address()
	fromMain := mainGiteaFrom(metas)
	pending := make([]*workflow.ServiceMeta, 0, len(metas))
	for _, m := range metas {
		if hqPairNeedsRepository(m, address, fromMain) || hqPairWired(m) {
			pending = append(pending, m)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].Hostname < pending[j].Hostname })

	now := time.Now().UTC()
	self := lazySelf(ctx, hqc)
	var report []string
	for _, m := range pending {
		state := readHQPairState(stateDir, m.Hostname)
		if !hqAttemptDue(state, now) {
			continue
		}
		// A pair another process runs git on is left to it, and no attempt
		// is recorded: it stays due for the next pass.
		release, err := workflow.LockPair(ctx, stateDir, m.Hostname, 0)
		if err != nil {
			continue
		}
		outcome, wired := passHQPair(ctx, client, httpClient, sshDeployer, rt, stateDir, hqc, self, address, fromMain, m.Hostname)
		release()
		// The attempt is recorded even when it had nothing to say: a wired
		// pair with nothing open is the ORDINARY state, and without the
		// backoff every agent tool call would ask HQ about it.
		recordHQAttempt(stateDir, m.Hostname, state, now, outcome, wired)
		if outcome != "" {
			report = append(report, m.Hostname+": "+outcome)
		}
	}
	return report
}

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
	fromMain mainGitea,
	hostname string,
) (string, bool) {
	m, _ := workflow.FindServiceMeta(stateDir, hostname)
	switch {
	case hqPairNeedsRepository(m, address, fromMain):
		attempt := wireHQPair(ctx, client, httpClient, sshDeployer, rt, stateDir, hqc, m, hqRepoNameOf(m, fromMain))
		return attempt.line, attempt.wired
	case hqPairWired(m):
		return keepHQPairCurrent(ctx, client, httpClient, sshDeployer, rt, stateDir, hqc, self, m)
	}
	return "", false
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
// application HQ holds the Mate in now, if that moved; a pending delivery
// finished; and what became of its change on record. wired is whether the
// pass wired it again.
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
		attempt := wireHQPair(ctx, client, httpClient, sshDeployer, rt, stateDir, hqc, m, m.HQ.Repo)
		return attempt.line, attempt.wired
	}
	var lines []string
	if note := hqLearnLanding(stateDir, m, state); note != "" {
		lines = append(lines, note)
	}
	if m.HQ.Pending != nil {
		deliveryLanding(stateDir, m, state)
		if line := finishPendingDelivery(ctx, client, sshDeployer, rt, stateDir, hqc, m); line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "; "), false
}

// rewireHQPair runs the wiring for one pair now, outside the backoff, and
// records the attempt the way a pass does. It is the trigger a session has
// after bootstrap: a git-push of a pair whose remote is this Mate's HQ but
// which was never recorded as wired (handleGitPush), and a stand-up adopting
// a pair from the group recipe.
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
	attempt := wireHQPair(ctx, client, httpClient, sshDeployer, rt, stateDir, hqc, m, repoName)
	recordHQAttempt(stateDir, m.Hostname, readHQPairState(stateDir, m.Hostname), time.Now().UTC(), attempt.line, attempt.wired)
	return attempt
}

// recordHQAttempt writes one attempt onto the pair's backoff record. A pass
// that wired the pair starts the backoff afresh: the failures that grew it
// are over.
func recordHQAttempt(stateDir, hostname string, prior hqPairState, at time.Time, outcome string, wired bool) {
	attempts := prior.Attempts + 1
	if wired {
		attempts = 1
	}
	writeHQPairState(stateDir, hostname, hqPairState{
		Attempts:      attempts,
		LastAttemptAt: at.Format(time.RFC3339),
		LastOutcome:   outcome,
	})
}

// hqRepoNameOf is the repository a pair is wired to: the one its record
// names — a stand-up's comes from the group recipe — or the one main's zcp
// wired it to on the old Gitea, else its dev hostname.
func hqRepoNameOf(m *workflow.ServiceMeta, fromMain mainGitea) string {
	if hqPairWired(m) {
		return m.HQ.Repo
	}
	if name, ok := fromMain.repository(m); ok {
		return name
	}
	return m.Hostname
}

// hqPairWired reports whether a pair delivers to HQ: it has its repository
// there, and its stage deploy delivers by itself.
func hqPairWired(m *workflow.ServiceMeta) bool {
	return m != nil && m.HQ != nil && m.HQ.Repo != ""
}

// hqPairNeedsRepository reports whether a pair still needs wiring. One
// repository per pair is enforced HERE, by state rather than by memory: a
// pair is wired exactly when it carries an HQ record (written last, after the
// remote and the branch) and a configured git-push, and no number of passes
// asks HQ for a second one.
//
// A remote with no HQ record may be a wiring that stopped half-way —
// git-push-setup stamps the remote before the branch step runs — and is
// retried when it is this pair's repository on this Mate's HQ (address); the
// remote main's zcp set on the old Gitea moves to HQ (fromMain); any other
// remote is the user's own. Deciding that before HQ is asked matters: HQ
// makes the repository it is asked for.
func hqPairNeedsRepository(m *workflow.ServiceMeta, address string, fromMain mainGitea) bool {
	if m == nil || !m.IsComplete() {
		return false
	}
	if hqPairWired(m) && m.GitPushState == topology.GitPushConfigured {
		return false
	}
	// A pair the user already pointed at a remote of their own is theirs.
	// ZCP does not move a working repository to HQ behind their back.
	return m.RemoteURL == "" || m.HQ != nil || hqRemoteIsThePairs(m.RemoteURL, address, m.Hostname) || fromMain.owns(m)
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

// wireHQPair gives one pair its repository in HQ and its own branch: the
// repository repoName in the application HQ holds the Mate in (made if new),
// git-push to it, the Mate's branch cut from its `main`, the HQ record, the
// workflow file. repoName is the pair's dev hostname for a pair zcp
// bootstrapped, and the repository the group's recipe names for a pair a
// stand-up adopted from it (standup.go), or the one main's zcp wired the pair
// to on the old Gitea, which HQ's import brought under the same name
// (hq_main_gitea.go). A pair whose Mate HQ now holds in another application
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
	m *workflow.ServiceMeta,
	repoName string,
) hqWiringOutcome {
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
	remote := hqc.RepoURL(repo.AppID, repo.Name)
	// A remote on this Mate's HQ is ours to point again — a wiring that
	// stopped half-way, or the application the Mate left — and so is the one
	// main's zcp set on the old Gitea; any other one the user chose, and
	// git-push-setup would rewrite it.
	fromMain := mainGiteaOf(stateDir).owns(m)
	if m.RemoteURL != "" && !sameGitRepository(m.RemoteURL, remote) && !ops.IsHQRemote(m.RemoteURL, hqc.Address()) && !fromMain {
		return hqWiringOutcome{
			line: fmt.Sprintf("%s already pushes to %s, not to its repository %q in HQ — that remote is left as the user's own.",
				m.Hostname, topology.RedactRepoURLCredentials(m.RemoteURL), repo.Name),
			usersOwn: true,
		}
	}

	// The helper main persisted for the old Gitea's host answers GIT_TOKEN,
	// which is about to be the Mate credential, and the old remote stays as
	// zerops-original-origin: it goes first, so HQ's credential is never
	// handed to the old Gitea.
	if fromMain {
		if _, err := sshDeployer.ExecSSH(ctx, m.Hostname, ops.BuildDropCredentialHelperCommand(hqPairWorkingDir, m.RemoteURL)); err != nil {
			return hqWiringOutcome{line: fmt.Sprintf("repository %q is ready in HQ, but %s's credential helper for main's Gitea could not be removed (%v) — retrying on the next pass.", repo.Name, m.Hostname, err)}
		}
	}

	// git-push-setup owns the credential, the probe, the origin sync and the
	// meta stamp. Calling it rather than reproducing it is what keeps the
	// credential out of every file ZCP writes: it goes to a sensitive service
	// env on the push source and reaches git through the session credential
	// helper, exactly as a user-driven setup does.
	result, _, _ := confirmGitPushSetupContainer(
		ctx, client, httpClient, sshDeployer, rt.ProjectID, stateDir, hqc.Address(),
		WorkflowInput{Service: m.Hostname, RemoteURL: remote, GitToken: hqc.Credential()},
		m,
	)
	if result != nil && result.IsError {
		return hqWiringOutcome{line: fmt.Sprintf("repository %q is ready in HQ but wiring git-push to it failed — retrying on the next pass.", repo.Name)}
	}

	// The push source works ON the Mate's branch from now on, and the branch
	// has to DESCEND from the repository's `main`: the pair was
	// git-initialised with a history of its own, and a change unrelated to
	// `main` cannot be squashed onto it. Best-effort: a container that
	// refuses still has its repository, and the next pass tries again.
	branch := hqMateBranch(hqc)
	if out, branchErr := sshDeployer.ExecSSH(ctx, m.Hostname,
		ops.BuildMateBranchCommand(hqPairWorkingDir, branch)); branchErr != nil {
		prefix := fmt.Sprintf("repository %q is ready in HQ, but %s could not be put on %q branched off \"main\"", repo.Name, m.Hostname, branch)
		refusal := ops.MateBranchRefusal(string(out))
		if remedy := ops.MateBranchRefusalRemedy(refusal, branch); remedy != "" {
			return hqWiringOutcome{
				line:   fmt.Sprintf("%s (%s): in %s on %s, %s", prefix, refusal, hqPairWorkingDir, m.Hostname, remedy),
				remedy: fmt.Sprintf("In %s on %s, %s", hqPairWorkingDir, m.Hostname, remedy),
			}
		}
		return hqWiringOutcome{line: fmt.Sprintf("%s (%v) — a push would have nothing to send, or nothing HQ could merge; retrying on the next pass.", prefix, branchErr)}
	}
	var (
		left     int
		recorded *workflow.HQRepoRef
	)
	if err := workflow.UpsertServiceMeta(stateDir, m.Hostname, func(meta *workflow.ServiceMeta, existed bool) error {
		if !existed {
			return fmt.Errorf("meta for %q vanished", m.Hostname)
		}
		meta.HQ, left = wiredHQRecord(meta.HQ, repo, branch)
		carryMainGitea(meta.HQ, meta.MainGitea)
		meta.MainGitea, recorded = nil, meta.HQ
		return nil
	}); err != nil {
		return hqWiringOutcome{line: fmt.Sprintf("repository %q is wired but recording it failed (%v) — the next pass re-reads it.", repo.Name, err)}
	}
	m.HQ, m.MainGitea = recorded, nil

	line := fmt.Sprintf("repository %q wired in HQ; this Mate works on %q and lands on \"main\" through a change (never pushing main directly)", repo.Name, branch)
	if fromMain {
		line += "; it moved off main's Gitea, which stays as the remote zerops-original-origin"
	}
	if left != 0 {
		line += fmt.Sprintf("; this Mate moved to another application, and its change #%d stays in the one it was opened in", left)
	}
	return hqWiringOutcome{line: line, wired: true}
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

// wiredHQRecord is the pair's HQ record once it is wired to repo on branch.
// Wired again to the same repository it keeps everything; wired to another —
// the Mate moved to another application — its change and the landing of one
// stay with the old application (left is the change left there, 0 for none),
// while a delivery still owed and words kept for the next change come along.
func wiredHQRecord(prior *workflow.HQRepoRef, repo hq.Repo, branch string) (record *workflow.HQRepoRef, left int) {
	wiredAt := time.Now().UTC().Format(time.RFC3339)
	if prior != nil && prior.AppID == repo.AppID && prior.Repo == repo.Name {
		kept := *prior
		kept.Branch, kept.WiredAt = branch, wiredAt
		return &kept, 0
	}
	record = &workflow.HQRepoRef{AppID: repo.AppID, Repo: repo.Name, Branch: branch, WiredAt: wiredAt}
	if prior != nil {
		left = prior.Change
		record.Pending = prior.Pending
		if prior.ChangeDescription != nil && prior.ChangeDescription.Change == 0 {
			record.ChangeDescription = prior.ChangeDescription
		}
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

// appendRepositoryReport folds the reconcile's lines into a bootstrap
// response's message. Separate from the reconcile so the reconcile stays a
// pure "what happened" reporter with no opinion about where it is rendered.
func appendRepositoryReport(resp *workflow.BootstrapResponse, lines []string) {
	if resp == nil || len(lines) == 0 {
		return
	}
	resp.Message += " HQ — " + strings.Join(lines, "; ") + "."
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
