package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

	pending := make([]*workflow.ServiceMeta, 0, len(metas))
	for _, m := range metas {
		if giteaPairNeedsRepository(m) {
			pending = append(pending, m)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].Hostname < pending[j].Hostname })

	wiring := ops.ReadGiteaWiring(giteaEnvLookup(liveEnvPath))
	now := time.Now().UTC()

	var report []string
	for _, m := range pending {
		state := readGiteaPairState(stateDir, m.Hostname)
		if !giteaAttemptDue(state, now) {
			continue
		}
		outcome := reconcileOneGiteaPair(ctx, client, httpClient, sshDeployer, rt, stateDir, wiring, m)
		if outcome == "" {
			continue
		}
		writeGiteaPairState(stateDir, m.Hostname, giteaPairState{
			Attempts:      state.Attempts + 1,
			LastAttemptAt: now.Format(time.RFC3339),
			LastOutcome:   outcome,
		})
		report = append(report, m.Hostname+": "+outcome)
	}
	return report
}

// giteaPairNeedsRepository reports whether this pair still has something to
// do. One repository per pair is enforced HERE, by state rather than by
// memory: a pair that already carries a Gitea ref and a configured git-push
// is done, and no number of passes will ask the broker for a second one.
func giteaPairNeedsRepository(m *workflow.ServiceMeta) bool {
	if m == nil || !m.IsComplete() {
		return false
	}
	if m.Gitea != nil && m.Gitea.FullName != "" && m.GitPushState == topology.GitPushConfigured {
		return false
	}
	// A pair the user already pointed at a remote of their own is theirs.
	// ZCP does not move a working repository to Gitea behind their back.
	return m.RemoteURL == "" || m.Gitea != nil
}

// reconcileOneGiteaPair does the work for one pair and returns a report line
// ("" when there is nothing worth saying). Never returns an error: every
// outcome is reportable.
func reconcileOneGiteaPair(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	sshDeployer ops.SSHDeployer,
	rt runtime.Info,
	stateDir string,
	wiring ops.GiteaWiring,
	m *workflow.ServiceMeta,
) string {
	if !wiring.Ready() {
		return "waiting for Gitea (" + strings.Join(wiring.MissingKeys(), ", ") + " not on this service yet)"
	}

	// Who this Mate is on that Gitea. The login names the branch, so it has
	// to be known before anything is pushed.
	identity, identityErr := ops.DeriveGiteaIdentity(ctx, httpClient, wiring.GiteaURL, wiring.Token)
	if identityErr != nil {
		return fmt.Sprintf("waiting for Gitea (could not read this Mate's bot identity: %v)", identityErr)
	}
	branch := ops.GiteaMateBranch(identity.Name)

	repo, repoErr := ops.RequestMateRepository(ctx, httpClient, wiring.BrokerURL, wiring.Token, m.Hostname)
	switch {
	case errors.Is(repoErr, ops.ErrRepositoryTaken):
		return fmt.Sprintf("the broker refuses a repository named %q — it exists in the group's org and this Mate is not a collaborator on it. Rename the service, or have someone add this Mate's bot to that repository.", m.Hostname)
	case errors.Is(repoErr, ops.ErrNotRegistered):
		return "the broker does not know this project yet (not a registered Mate) — it will once the group is registered; nothing else is blocked."
	case repoErr != nil:
		return fmt.Sprintf("could not reach the broker for a repository (%v) — retrying on the next pass.", repoErr)
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
		return fmt.Sprintf("repository %s is ready but wiring git-push to it failed — retrying on the next pass.", repo.FullName)
	}

	// The push source works ON that branch from now on. git-push-setup owns
	// the remote and the credential but never touches branches, and the pair
	// was git-initialised on `main`, so without this `git push -u origin
	// mate/{bot}` has no local ref to push and the delivery fails at the
	// refspec. Best-effort: a container that refuses the checkout still has
	// its repository, and the next pass tries again.
	if _, checkoutErr := sshDeployer.ExecSSH(ctx, m.Hostname,
		ops.BuildGitCheckoutBranchCommand(giteaPairWorkingDir, branch)); checkoutErr != nil {
		return fmt.Sprintf("repository %s is ready, but %s could not be put on %q (%v) — a push would have nothing to send; retrying on the next pass.",
			repo.FullName, m.Hostname, branch, checkoutErr)
	}

	base := repo.DefaultBranch
	if base == "" {
		base = giteaProtectedBase
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
		return fmt.Sprintf("repository %s is wired but recording it failed (%v) — the next pass re-reads it.", repo.FullName, err)
	}

	line := fmt.Sprintf("repository %s wired; this Mate works on %q and lands on %q through a pull request (never pushing %s directly)", repo.FullName, branch, base, base)
	if number, created, prErr := ops.EnsureGiteaPullRequest(
		ctx, httpClient, wiring.GiteaURL, wiring.Token, repo.FullName, repo.FullName, branch, base,
		"Mate: "+m.Hostname,
	); prErr == nil && number != 0 {
		verb := "tracked by"
		if created {
			verb = "opened as"
		}
		line += fmt.Sprintf("; %s pull request #%d", verb, number)
	}
	return line
}

// giteaAttemptDue applies the backoff: the first pass always runs, and each
// unproductive one pushes the next further out, capped. A pair that finished
// never reaches here — giteaPairNeedsRepository filtered it out.
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
