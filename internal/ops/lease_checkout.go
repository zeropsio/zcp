package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/zeropsio/zcp/internal/platform"
)

// leaseWorkDir is where a leased container keeps its code.
const leaseWorkDir = "/var/www"

// leaseDefaultBranch is the branch an empty branch means, and the one a new
// branch is cut from (DP-6).
const leaseDefaultBranch = defaultBranch

// leaseGuardFileLimit is how many unsaved files a refused release names.
const leaseGuardFileLimit = 20

// leaseSSHUnreachableExit is the exit status ssh itself gives when it cannot
// reach or enter the host (a remote command's own status is never 255 here).
const leaseSSHUnreachableExit = 255

// leaseBranchRe is the branches a lease takes: a conservative subset of git's
// ref names that cannot be read as an option and carries nothing a shell or a
// ref-format rule treats specially.
var leaseBranchRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

// leasePoolNameRe is the shape of a pool's service hostname; it is also what
// keeps a pool name from naming a path outside the lease directory.
var leasePoolNameRe = regexp.MustCompile(`^[a-z0-9]{1,40}$`)

// leaseCommitRe is a full git commit id (SHA-1 or SHA-256), the only shape a
// lease's base takes.
var leaseCommitRe = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

// leaseBaseMarker opens the line a checkout answers the main commit it cut a
// new branch from with.
const leaseBaseMarker = "@@zcp-lease base "

func validateLeaseBranch(branch string) error {
	if !leaseBranchRe.MatchString(branch) || strings.Contains(branch, "..") || strings.Contains(branch, "//") ||
		strings.HasSuffix(branch, "/") || strings.HasSuffix(branch, ".") || strings.HasSuffix(branch, ".lock") {
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			"The branch name is not one a lease takes",
			"Use letters, digits and . _ / - only, starting with a letter or digit (e.g. feature/login)")
	}
	return nil
}

// validateLeaseRepo checks a clone URL and returns it as given, only trimmed
// of surrounding space: git clones exactly that address, and a server that
// serves `…/repo.git` may not answer `…/repo` (DP-17), so the canonical form
// (topology.CanonicalRepoURL) is for identity only. Only https URLs without
// userinfo are taken: the credential reaches git through the session-env
// helper, never through the URL (DP-6), plain http would send it in clear, and
// a value that is not a URL could be read by git as an option or a transport.
func validateLeaseRepo(raw string) (string, error) {
	bad := platform.NewPlatformError(platform.ErrInvalidParameter,
		"repo is not a clone URL a lease takes",
		"Pass an https://host/owner/repo URL with no credentials in it; the pool's GIT_TOKEN authenticates the clone")
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "https://") {
		return "", bad
	}
	if strings.ContainsFunc(raw, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return "", bad
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil {
		return "", bad
	}
	return raw, nil
}

// poolDefault is zcp's record of the repository a pool's leases are for
// (.zcp/state/leases/pools/<pool>.json): the clone URL as the first acquire
// was given it, which a later acquire without repo clones. It carries no
// credential; compare it through topology.SameRepoURL, never as a string.
type poolDefault struct {
	Repo string `json:"repo"`
}

func poolDefaultPath(stateDir, pool string) string {
	return filepath.Join(leaseDir(stateDir), "pools", pool+".json")
}

// readPoolDefaultRepo is the pool's default repository, "" when none is
// recorded (or the record cannot be read: it names nothing zcp could use).
func readPoolDefaultRepo(stateDir, pool string) string {
	if stateDir == "" || !leasePoolNameRe.MatchString(pool) {
		return ""
	}
	raw, err := os.ReadFile(poolDefaultPath(stateDir, pool))
	if err != nil {
		return ""
	}
	var rec poolDefault
	if json.Unmarshal(raw, &rec) != nil {
		return ""
	}
	return rec.Repo
}

// storePoolDefaultRepo records repo as the pool's default, atomically with
// mode 0600.
func storePoolDefaultRepo(stateDir, pool, repo string) error {
	if stateDir == "" || !leasePoolNameRe.MatchString(pool) {
		return errors.New("no state directory or an unusable pool name")
	}
	dir := filepath.Dir(poolDefaultPath(stateDir, pool))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(poolDefault{Repo: repo}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".pool-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	defer os.Remove(tmp.Name()) // best effort: already gone after a successful rename
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	if err := os.Rename(tmp.Name(), poolDefaultPath(stateDir, pool)); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// resolveLeaseRepo is the clone URL an acquire clones, as it was given: the
// acquire's own, else the pool's default. It reports whether the pool has no
// default yet, so the caller can record the given one once the checkout
// worked.
func resolveLeaseRepo(stateDir, pool, repo string) (cloneURL string, becomesDefault bool, err error) {
	have := readPoolDefaultRepo(stateDir, pool)
	if strings.TrimSpace(repo) == "" {
		if have == "" {
			return "", false, platform.NewPlatformError(platform.ErrInvalidParameter,
				fmt.Sprintf("acquire needs repo: pool '%s' has no default repository yet", pool),
				"Pass the clone URL as repo (https://host/owner/repo); the first acquire on a pool stores it as the pool's default")
		}
		cloneURL, err = validateLeaseRepo(have)
		return cloneURL, false, err
	}
	cloneURL, err = validateLeaseRepo(repo)
	if err != nil {
		return "", false, err
	}
	return cloneURL, have == "", nil
}

// leaseCheckoutCommand is the git script that puts workDir on branch of repo.
// It runs in the leased container over ssh. The credential is never composed
// into it: fetch authenticates through leaseCredentialHelperShell inline, which
// reads $GIT_TOKEN at request time (the pool's own env reaches its
// containers) and answers nothing without it, so a missing token reads as
// git's "terminal prompts disabled", not as a rejected empty password. repo
// and branch are shell-quoted; the caller has validated both.
//
// An existing origin/<branch> is checked out tracking it; a branch that does
// not exist yet is cut without tracking it (a push of the new branch must not
// target main) — from base, the main commit its group's other leases were cut
// from, or from origin/main when base is no commit id (DP-17) — and the
// command answers the commit it cut from on a leaseBaseMarker line
// (checkoutBase). A base origin does not have fails the command naming it:
// the branch is never cut from another commit than its group's. Files the
// image left that the branch does not track are removed; ignored files stay
// (never clean -x). For an https origin the repository keeps the same helper
// (leaseCredentialHelperConfig), so the agent's own push authenticates the
// same way. hqURL is the Mate's HQ, "" on a container not enrolled with one: a
// repository on it is fetched as HQ's git user (leaseCredentialHelperFor).
func leaseCheckoutCommand(workDir, repo, branch, base, hqURL string) string {
	q := shellQuote
	cut := "git checkout -q -f -B " + q(branch) + " --no-track origin/" + leaseDefaultBranch +
		" && printf '" + leaseBaseMarker + "%s\\n' \"$(git rev-parse HEAD)\""
	if leaseCommitRe.MatchString(base) {
		cut = "{ git rev-parse -q --verify " + q(base+"^{commit}") + " >/dev/null || { echo " +
			q("the branch's base commit "+base+" is not on origin") + " >&2; exit 3; }; } && " +
			"git checkout -q -f -B " + q(branch) + " --no-track " + q(base) + " && echo " + q(leaseBaseMarker+base)
	}
	steps := []string{
		"cd " + q(workDir),
		"{ [ -d .git ] || git init -q; }",
		"{ git remote add origin " + q(repo) + " 2>/dev/null || git remote set-url origin " + q(repo) + "; }",
		"GIT_TERMINAL_PROMPT=0 git -c credential.helper= -c credential.helper=" + q(leaseCredentialHelperFor(repo, hqURL)) + " fetch -q --prune origin",
		"if git rev-parse --verify -q " + q("refs/remotes/origin/"+branch) + " >/dev/null; then " +
			"git checkout -q -f -B " + q(branch) + " --track " + q("origin/"+branch) + "; else " + cut + "; fi",
		"git clean -fd -q",
	}
	if persist := leaseCredentialHelperConfig(repo, hqURL); persist != "" {
		steps = append(steps, persist)
	}
	return strings.Join(steps, " && ")
}

// leaseCredentialHelperShell is the helper a lease's repository keeps for its
// origin's host: it answers a get from the invoking session's $GIT_TOKEN (the
// pool's secret reaches every ssh session in its containers), so the agent's
// own push over the alias needs no credential typed or stored; the config holds
// only this script. Without GIT_TOKEN — git run through the mount on the zcp
// side — it answers nothing, never an empty password, so git goes on to its
// next helper. The reset at release takes it away with /var/www.
const leaseCredentialHelperShell = `!f() { test "$1" = get || return 0; test -n "$GIT_TOKEN" || return 0; echo username=oauth2; echo "password=$GIT_TOKEN"; }; f`

// leaseHQCredentialHelperShell is leaseCredentialHelperShell for a repository
// on the Mate's HQ, whose git takes only the user `mate` (as
// hqCredentialHelperShell answers it). Unlike that helper it never falls back
// to `zcp hq git-credential`: zcp is not installed in a pool container, so it
// answers from GIT_TOKEN or not at all.
const leaseHQCredentialHelperShell = `!f() { test "$1" = get || return 0; test -n "$GIT_TOKEN" || return 0; echo username=mate; echo "password=$GIT_TOKEN"; }; f`

// leaseCredentialHelperFor is the helper a lease of repo authenticates with:
// HQ's user for a repository on the Mate's HQ (hqURL, "" without one), the
// lease's oauth2 helper otherwise.
func leaseCredentialHelperFor(repo, hqURL string) string {
	if IsHQRemote(repo, hqURL) {
		return leaseHQCredentialHelperShell
	}
	return leaseCredentialHelperShell
}

// leaseCredentialHelperConfig is the git command that persists the lease's
// helper for an https repo, scoped by the rule a dev service's checkout is
// (persistedHelperScope: HQ's scope, its port included, for a repository on
// the Mate's HQ); "" for any other repo, or a host that can be no scope.
func leaseCredentialHelperConfig(repo, hqURL string) string {
	if !strings.HasPrefix(repo, httpsScheme+"://") {
		return ""
	}
	scope, helper, ok := persistedHelperScope(repo, hqURL, leaseCredentialHelperShell, leaseHQCredentialHelperShell)
	if !ok {
		return ""
	}
	return "git config --replace-all " + shellQuote("credential.https://"+scope+".helper") + " " + shellQuote(helper)
}

// hqAddress is the address of the HQ this Mate enrolled with, read now; ""
// when there is none.
func (d LeaseDeps) hqAddress() string {
	if d.HQAddress == nil {
		return ""
	}
	return d.HQAddress()
}

// checkoutLease puts the leased container's /var/www on the branch, a new one
// cut from base (leaseCheckoutCommand), and returns the main commit it cut a
// new branch from — "" when it tracked the branch origin has. A failure
// carries the ssh error (git's stderr rides in it) and classifies auth
// failures so the credential contract reaches the agent.
func checkoutLease(ctx context.Context, d LeaseDeps, rec leaseRecord, repo, branch, base string) (string, error) {
	hqURL := d.hqAddress()
	out, err := d.SSH.ExecSSH(ctx, rec.alias(), leaseCheckoutCommand(leaseWorkDir, repo, branch, base, hqURL))
	if err == nil {
		cut, _ := checkoutBase(string(out))
		return cut, nil
	}
	code, suggestion := classifyCheckoutFailure(err, IsHQRemote(repo, hqURL))
	pe := platform.NewPlatformError(code,
		fmt.Sprintf("Lease %s could not put %s on branch %s of %s", rec.alias(), leaseWorkDir, branch, repo), suggestion)
	pe.Cause = err
	return "", pe
}

// checkoutBase is the commit a checkout's answer says it cut a new branch
// from, false when it says none (it tracked the branch origin has).
func checkoutBase(out string) (string, bool) {
	for line := range strings.SplitSeq(out, "\n") {
		if base, ok := strings.CutPrefix(strings.TrimSpace(line), leaseBaseMarker); ok && leaseCommitRe.MatchString(base) {
			return base, true
		}
	}
	return "", false
}

// classifyCheckoutFailure maps git's stderr (and ssh's exit status) to an error
// code and what to do about it. onHQ: the repository is on the Mate's HQ,
// whose credential is no user's token (hqLeaseCredentialSuggestion).
func classifyCheckoutFailure(err error, onHQ bool) (code, suggestion string) {
	var sshErr *platform.SSHExecError
	out := ""
	if errors.As(err, &sshErr) {
		out = strings.ToLower(sshErr.Output)
	}
	switch {
	case containsAny(out, "could not read username", "could not read password", "terminal prompts disabled"):
		if onHQ {
			return platform.ErrPrerequisiteMissing, hqLeaseCredentialSuggestion("the container's session had no GIT_TOKEN: the pool's secret is not set (or is empty), or had not reached the session yet")
		}
		return platform.ErrGitTokenMissing,
			"git asked for a credential and the container's session had none: the pool's GIT_TOKEN secret is not set (or is empty). Private repositories need it"
	case containsAny(out, "authentication failed", "invalid username or password", "invalid credentials",
		"permission denied", "error: 401", "error: 403", "password authentication was removed"):
		if onHQ {
			return platform.ErrPrerequisiteMissing, hqLeaseCredentialSuggestion("HQ refused the pool's GIT_TOKEN for that user: it is not this Mate's current HQ credential")
		}
		return platform.ErrGitTokenInvalid,
			"The remote refused the pool's GIT_TOKEN (bad, expired or without access to this repository)"
	}
	if exitStatus(err) == leaseSSHUnreachableExit {
		return platform.ErrSSHDeployFailed,
			"The container did not answer ssh. Check that the pool's sshIsolation admits this service and that zerops_lease action=status shows the alias installed"
	}
	if strings.Contains(out, "base commit") && strings.Contains(out, "is not on origin") {
		return platform.ErrSSHDeployFailed,
			"The branch is new to origin, and its other leases were cut from a main commit origin no longer has (main was rewritten since). Push the branch from one of them (git push -u origin <branch>), then acquire again: a branch origin has is checked out as it is there"
	}
	return platform.ErrSSHDeployFailed,
		"Check the repo URL and that the container can reach the git host"
}

// hqLeaseCredentialSuggestion is what a lease of a repository on the Mate's HQ
// lacks when HQ's git would not take the pool's GIT_TOKEN (why). Inside a Mate
// an acquire writes the Mate credential there (DP-16) and forgets it once HQ
// refuses it, so the next acquire is the step: what is left is a Mate that is
// enrolling again, or a container that is not a Mate. It is no user's token to
// ask for.
func hqLeaseCredentialSuggestion(why string) string {
	return "The repository is on this Mate's HQ, whose git takes only the user `mate` with this Mate's HQ credential, and " + why +
		". zcp writes that credential to the pool's GIT_TOKEN at an acquire of an HQ repository inside a Mate, and writes it afresh at the next one after a refusal. Acquire again. " +
		"If HQ refuses it again, the Mate is enrolling with HQ anew (its own service, zcp service mate, keeps enrolling it) and an acquire after that writes the new one; outside a Mate zcp writes nothing there. " +
		"It is no user's token, so never ask for one or make one up. Meanwhile lease the pool for a repository outside HQ, or work on this one in the dev service of the pair wired to it"
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// exitStatus is the exit status of the process behind err, -1 when err has
// none.
func exitStatus(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// abandonLease hands a container whose checkout failed straight back: unmount,
// release with reset — or delete it when it cannot be reset (DP-15) — and drop
// the record and its alias stanza. The refusal's message and suggestion both
// say where the container went (handBackOutcome); one that could not be
// handed back stays reserved and recorded.
func abandonLease(ctx context.Context, d LeaseDeps, rec leaseRecord, cause error) error {
	// The caller's context may be what ended the checkout; the container must
	// still go back.
	view, err := releaseAndDetach(context.WithoutCancel(ctx), d, rec, rec.alias())
	what, next := handBackOutcome(rec.alias(), view, err)
	var pe *platform.PlatformError
	if !errors.As(cause, &pe) {
		return fmt.Errorf("%w (%s; %s)", cause, what, next)
	}
	out := *pe
	out.Message += "; " + what
	out.Suggestion = strings.TrimSuffix(strings.TrimSpace(pe.Suggestion), ".")
	if out.Suggestion != "" {
		out.Suggestion += ". "
	}
	out.Suggestion += next
	return &out
}

// handBackOutcome says where a failed checkout's container went — released,
// deleted because it could not be reset, gone already, or still reserved —
// once for the message (what) and once as what to do (next), so the two never
// disagree.
func handBackOutcome(alias string, view LeaseView, err error) (what, next string) {
	switch {
	case err != nil:
		return fmt.Sprintf("handing the container back failed (%v), so %s stays reserved and its record is kept", err, alias),
			fmt.Sprintf("Release it before acquiring again: zerops_lease action=release lease=%s force=true", alias)
	case view.containerDeleted:
		return "the container could not be reset (the platform created it for the pool's minContainers, so it has no primary snapshot), so it was deleted and the lease dropped",
			"This lease is gone; acquire again for another (the pool creates a fresh container). Set the pool's minContainers to 0 and pre-warm containers through the orchestrator"
	case view.State == LeaseStateLost:
		return "the orchestrator no longer held the container for this lease, so nothing was left to hand back and the record was dropped",
			"This lease is gone; acquire again for another"
	}
	return "the container was released with a reset and the lease dropped", "This lease is gone; acquire again for another"
}

// unsavedWork is what a release would lose from a container.
type unsavedWork struct {
	files    []string // git status --porcelain lines
	unpushed int      // commits on no remote branch
}

func (w unsavedWork) any() bool { return len(w.files) > 0 || w.unpushed > 0 }

// leaseGitRead starts a git call that only reads a lease's checkout (DP-4):
// GIT_OPTIONAL_LOCKS=0 keeps `git status` from refreshing .git/index and
// writing it back — a new inode and mtime in the person's checkout, same
// bytes (live, 2026-10-23) — so a read writes nothing there.
const leaseGitRead = "GIT_OPTIONAL_LOCKS=0 git -C "

// readUnsavedWork reads, over ssh, the uncommitted and untracked files and the
// commits on no remote branch of the container's /var/www, writing nothing
// into it (leaseGitRead).
func readUnsavedWork(ctx context.Context, ssh SSHDeployer, alias string) (unsavedWork, error) {
	var w unsavedWork
	out, err := ssh.ExecSSH(ctx, alias, leaseGitRead+shellQuote(leaseWorkDir)+" status --porcelain")
	if err != nil {
		return w, fmt.Errorf("git status: %w", err)
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		if line = strings.TrimRight(line, "\r "); line != "" {
			w.files = append(w.files, line)
		}
	}
	wd := shellQuote(leaseWorkDir)
	out, err = ssh.ExecSSH(ctx, alias, "if "+leaseGitRead+wd+" rev-parse -q --verify HEAD >/dev/null 2>&1; then "+
		leaseGitRead+wd+" rev-list --count HEAD --not --remotes=origin; else echo 0; fi")
	if err != nil {
		return w, fmt.Errorf("git rev-list: %w", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return w, errors.New("git rev-list answered nothing")
	}
	n, convErr := strconv.Atoi(fields[len(fields)-1])
	if convErr != nil || n < 0 {
		return w, fmt.Errorf("git rev-list answered %q", strings.TrimSpace(string(out)))
	}
	w.unpushed = n
	return w, nil
}

// leaseGuardResult is what the release guard learned about the container.
type leaseGuardResult struct {
	// note is said to the agent on a release that went ahead unchecked.
	note string
	// unreachable: ssh could not reach the container, so nothing was checked.
	unreachable bool
}

// guardLeaseRelease refuses a release that would lose work (DP-4): commits on
// no remote branch, or uncommitted/untracked files. A container ssh cannot
// reach has no work zcp can save, so it is released unchecked; any other
// failure to read it refuses (zcp cannot tell what would be lost).
func guardLeaseRelease(ctx context.Context, d LeaseDeps, rec leaseRecord) (leaseGuardResult, error) {
	if d.SSH == nil {
		return leaseGuardResult{note: "the container was not checked for unsaved work (this zcp has no ssh to it)"}, nil
	}
	w, err := readUnsavedWork(ctx, d.SSH, rec.alias())
	if err != nil {
		if exitStatus(err) == leaseSSHUnreachableExit {
			return unreachableGuard(ctx, d, rec, err)
		}
		pe := platform.NewPlatformError(platform.ErrPrerequisiteMissing,
			fmt.Sprintf("Lease %s could not be checked for unsaved work, so it was not released", rec.alias()),
			"Look at the container's git state (git -C /var/www status) or release with force=true to discard whatever is there")
		pe.Cause = err
		return leaseGuardResult{}, pe
	}
	if !w.any() {
		return leaseGuardResult{}, nil
	}
	return leaseGuardResult{}, unsavedWorkRefusal(rec.alias(), w, IsHQRemote(rec.Repo, d.hqAddress()))
}

// unreachableGuard decides a release whose container ssh could not reach.
// ssh's 255 does not say the container is gone — an alias that does not
// resolve, a refused connection and a timeout all read the same — so the
// orchestrator decides: only a container it no longer holds for this lease —
// a pool that is gone or being deleted holds none (DP-19) — is released
// unchecked (as lost); the release then says what became of it. One it still
// holds may hold work and is refused, and so is one zcp cannot ask about.
func unreachableGuard(ctx context.Context, d LeaseDeps, rec leaseRecord, sshErr error) (leaseGuardResult, error) {
	held, err := leaseContainerStillReserved(ctx, d, rec)
	if err == nil && !held {
		return leaseGuardResult{
			unreachable: true,
			note:        "zcp could not reach the container over ssh and the orchestrator no longer holds it, so it was not checked for unsaved work",
		}, nil
	}
	state := "is still reserved"
	if err != nil {
		state = "may still be reserved (the orchestrator did not answer)"
	}
	pe := platform.NewPlatformError(platform.ErrPrerequisiteMissing,
		fmt.Sprintf("Lease %s could not be reached over ssh to check for unsaved work and %s, so it was not released", rec.alias(), state),
		fmt.Sprintf("zerops_lease action=status shows whether its alias and mount are in place; release with force=true to discard whatever it holds: zerops_lease action=release lease=%s force=true", rec.alias()))
	pe.Cause = sshErr
	return leaseGuardResult{}, pe
}

// leaseContainerStillReserved reports whether the pool's orchestrator still
// lists the record's container as reserved. The pool is read by id — the
// service search lags writes, and a pool missing from one search is not a
// pool that is gone; one the direct read no longer finds, or finds being
// deleted, holds nothing of this lease (leasePoolGoneWhy), and its
// orchestrator is not asked.
func leaseContainerStillReserved(ctx context.Context, d LeaseDeps, rec leaseRecord) (bool, error) {
	svc, err := d.Client.GetService(ctx, rec.PoolServiceID)
	if err != nil {
		var pe *platform.PlatformError
		if errors.As(err, &pe) && pe.Code == platform.ErrServiceNotFound {
			return false, nil
		}
		return false, fmt.Errorf("read pool %s: %w", rec.PoolHostname, err)
	}
	if poolDeletionState(svc.Status) != "" {
		return false, nil
	}
	rp, err := orchestratorFor(ctx, d, svc)
	if err != nil {
		return false, err
	}
	containers, err := d.containers(ctx, rp)
	if err != nil {
		return false, fmt.Errorf("list pool containers: %w", err)
	}
	for _, c := range containers {
		if c.ID == rec.ContainerID {
			return c.LockState == leaseLockReserved, nil
		}
	}
	return false, nil
}

// poolDeletionState names what a pool service's status says of its deletion:
// "is being deleted" (DELETING), "no longer exists" (DELETED), "" for any
// status of a pool that is still there. Either way out, its orchestrator
// holds nothing of a lease any more.
func poolDeletionState(status string) string {
	switch status {
	case "DELETING", "SERVICE_DELETING":
		return "is being deleted"
	case "DELETED", "SERVICE_DELETED":
		return "no longer exists"
	}
	return ""
}

// leasePoolGoneWhy reads the record's pool by id (the service search lags)
// and says why it holds nothing of the lease any more — the read no longer
// finds it, or it is deleted or being deleted — or "" when it is still there
// or the read failed otherwise.
func leasePoolGoneWhy(ctx context.Context, d LeaseDeps, rec leaseRecord) string {
	svc, err := d.Client.GetService(ctx, rec.PoolServiceID)
	if err != nil {
		var pe *platform.PlatformError
		if errors.As(err, &pe) && pe.Code == platform.ErrServiceNotFound {
			return "the pool service no longer exists"
		}
		return ""
	}
	if state := poolDeletionState(svc.Status); state != "" {
		return fmt.Sprintf("the pool service %s (status %s)", state, svc.Status)
	}
	return ""
}

// unsavedWorkRefusal refuses the release, naming the work and how to save it.
// onHQ: the lease's repository is on the Mate's HQ, which takes a push only on
// a branch of the agent's own, so the push it names keeps HQ's rule.
func unsavedWorkRefusal(alias string, w unsavedWork, onHQ bool) error {
	var parts []string
	if n := len(w.files); n > 0 {
		shown := w.files
		more := ""
		if n > leaseGuardFileLimit {
			shown = shown[:leaseGuardFileLimit]
			more = fmt.Sprintf(", first %d shown", leaseGuardFileLimit)
		}
		parts = append(parts, fmt.Sprintf("%d uncommitted or untracked file(s) (%s%s)", n, strings.Join(shown, ", "), more))
	}
	if w.unpushed > 0 {
		parts = append(parts, fmt.Sprintf("%d commit(s) on no remote branch", w.unpushed))
	}
	save := "Commit and push it from the container (zcp does not commit for you)"
	if onHQ {
		save = "Commit it on a branch of your own and push that branch from the container — `git checkout -b <branch>` first if you are on `main`, then `git push -u origin <branch>`; HQ refuses a push of `main`, of a branch another Mate's open change holds, and any force push (zcp does not commit for you)"
	}
	return platform.NewPlatformError(platform.ErrPrerequisiteMissing,
		fmt.Sprintf("Lease %s holds work that releasing would lose: %s", alias, strings.Join(parts, " and ")),
		fmt.Sprintf("%s, or release with force=true to discard it: zerops_lease action=release lease=%s force=true", save, alias))
}
