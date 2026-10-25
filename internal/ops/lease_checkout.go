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
// serves `…/repo.git` may not answer `…/repo` (DP-18), so the canonical form
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
// (never clean -x). Before any of it the checkout settles a clone zcp started
// while the container was warm (leaseWarmCheckoutSteps, DP-32): it takes a
// finished one of repo, so the fetch and the checkout move only what changed,
// and removes any other. For an https origin the repository keeps the same helper
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
	lockWarm, settleWarm := leaseWarmCheckoutSteps(repo)
	steps := []string{
		"cd " + q(workDir),
		lockWarm,
		settleWarm,
		"{ [ -d .git ] || git init -q; }",
		"{ git remote add origin " + q(repo) + " 2>/dev/null || git remote set-url origin " + q(repo) + "; }",
		"GIT_TERMINAL_PROMPT=0 git -c credential.helper= -c credential.helper=" + q(leaseCredentialHelperFor(repo, hqURL)) + " fetch -q --prune origin",
		"if git rev-parse --verify -q " + q("refs/remotes/origin/"+branch) + " >/dev/null; then " +
			"git checkout -q -f -B " + q(branch) + " --track " + q("origin/"+branch) + "; else " + cut + "; fi",
		"git clean -fd -q",
		// A fresh pool container carries no git identity; the agent's
		// first commit needs one (set-if-absent, never over the image's).
		gitIdentityEnsureFragment(),
	}
	if persist := leaseCredentialHelperConfig(repo, hqURL); persist != "" {
		steps = append(steps, persist)
	}
	// The container's stable number, which its preview port follows; only
	// the container knows it (leaseSlotMarker).
	steps = append(steps, `printf '`+leaseSlotMarker+`%s\n' "${ZEROPS_StableNumber:-}"`)
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
// hqCredentialHelperShell answers it) with this Mate's HQ credential — which
// the pool holds under a variable of its own (PoolHQCredentialEnvKey), never
// as GIT_TOKEN, so no helper a repository outside HQ uses ever reads it. Unlike
// hqCredentialHelperShell it never falls back to `zcp hq git-credential`: zcp
// is not installed in a pool container, so it answers from that variable or
// not at all.
const leaseHQCredentialHelperShell = `!f() { test "$1" = get || return 0; test -n "$` + PoolHQCredentialEnvKey + `" || return 0; echo username=mate; echo "password=$` + PoolHQCredentialEnvKey + `"; }; f`

// leaseCredentialHelperFor is the helper a lease of repo authenticates with:
// HQ's user and the pool's HQ credential for a repository on the Mate's HQ
// (hqURL, "" without one), the lease's oauth2 helper and the pool's GIT_TOKEN
// otherwise.
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

// leaseCheckedOut is what a checkout answers: the main commit it cut a new
// branch from ("" when it tracked the branch origin has), the container's
// stable number (0 when its session had none), and what it did with a clone
// zcp started while the container was warm (checkoutWarm: used, discarded, ""
// for none).
type leaseCheckedOut struct {
	base         string
	stableNumber int
	warm         string
}

// checkoutLease puts the leased container's /var/www on the branch, a new one
// cut from base (leaseCheckoutCommand), and returns the main commit it cut a
// new branch from and the container's stable number. A failure carries the
// ssh error (git's stderr rides in it) and classifies auth failures so the
// credential contract reaches the agent.
func checkoutLease(ctx context.Context, d LeaseDeps, rec leaseRecord, repo, branch, base string) (leaseCheckedOut, error) {
	hqURL := d.hqAddress()
	out, err := d.SSH.ExecSSH(ctx, rec.alias(), leaseCheckoutCommand(leaseWorkDir, repo, branch, base, hqURL))
	if err == nil {
		cut, _ := checkoutBase(string(out))
		return leaseCheckedOut{base: cut, stableNumber: checkoutSlot(string(out)), warm: checkoutWarm(string(out))}, nil
	}
	code, suggestion := classifyCheckoutFailure(err, IsHQRemote(repo, hqURL))
	pe := platform.NewPlatformError(code,
		fmt.Sprintf("Lease %s could not put %s on branch %s of %s", rec.alias(), leaseWorkDir, branch, repo), suggestion)
	pe.Cause = err
	return leaseCheckedOut{}, pe
}

// leaseSlotMarker opens the line a checkout answers the container's stable
// number on (ZEROPS_StableNumber, empty when the session has none).
const leaseSlotMarker = "@@zcp-lease slot "

// checkoutSlot is the stable number a checkout's answer names, 0 when it
// names none.
func checkoutSlot(out string) int {
	for line := range strings.SplitSeq(out, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line)+" ", leaseSlotMarker); ok {
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
				return n
			}
		}
	}
	return 0
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
			return platform.ErrPrerequisiteMissing, hqLeaseCredentialSuggestion("the container's session had no " + PoolHQCredentialEnvKey + ": the pool's secret is not set (or is empty), or had not reached the session yet")
		}
		return platform.ErrGitTokenMissing,
			"git asked for a credential and the container's session had none: the pool's GIT_TOKEN secret is not set (or is empty). Private repositories need it"
	case containsAny(out, "authentication failed", "invalid username or password", "invalid credentials",
		"permission denied", "error: 401", "error: 403", "password authentication was removed"):
		if onHQ {
			return platform.ErrPrerequisiteMissing, hqLeaseCredentialSuggestion("HQ refused the pool's " + PoolHQCredentialEnvKey + " for that user: it is not this Mate's current HQ credential")
		}
		return platform.ErrGitTokenInvalid,
			"The remote refused the pool's GIT_TOKEN (bad, expired or without access to this repository)"
	}
	if strings.Contains(out, "while it was warm") {
		return platform.ErrSSHDeployFailed,
			"A clone zcp started in the container while it was warm would not stop or could not be removed, so nothing was checked out on top of it. The container went back with a reset, which takes the clone away: acquire again"
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
// lacks when HQ's git would not take the pool's HQ credential (why). Inside a Mate
// an acquire writes the Mate credential there (DP-16) and forgets it once HQ
// refuses it, so the next acquire is the step: what is left is a Mate that is
// enrolling again, or a container that is not a Mate. It is no user's token to
// ask for.
func hqLeaseCredentialSuggestion(why string) string {
	return "The repository is on this Mate's HQ, whose git takes only the user `mate` with this Mate's HQ credential, and " + why +
		". zcp writes that credential to the pool's " + PoolHQCredentialEnvKey + " at an acquire of an HQ repository inside a Mate, and writes it afresh at the next one after a refusal. Acquire again. " +
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

// leaseGoneNext is what to do once a hand-back left no lease.
const leaseGoneNext = "This lease is gone; acquire again for another"

// handBackOutcome says where a failed checkout's container went — released,
// deleted because it could not be reset, removed or kept warm (DP-27), gone
// already, or still reserved —
// once for the message (what) and once as what to do (next), so the two never
// disagree.
func handBackOutcome(alias string, view LeaseView, err error) (what, next string) {
	switch {
	case err != nil:
		return fmt.Sprintf("handing the container back failed (%v), so %s stays reserved and its record is kept", err, alias),
			fmt.Sprintf("Release it before acquiring again: zerops_lease action=release lease=%s force=true", alias)
	case view.containerDeleted:
		return "the container could not be reset (the platform created it for the pool's minContainers, so it has no primary snapshot), so it was deleted and the lease dropped",
			leaseGoneNext + ". " + unresettableAdvice
	case view.containerRemoved:
		return "the container was removed with the lease (the pool already had its warm container, or the container ran an outdated version) and the lease dropped",
			leaseGoneNext
	case view.keptWarm:
		return "the container was released with a reset, kept as the pool's warm container, and the lease dropped",
			leaseGoneNext
	case view.State == LeaseStateLost:
		return "the orchestrator no longer held the container for this lease, so nothing was left to hand back and the record was dropped",
			leaseGoneNext
	}
	return "the container was released with a reset and the lease dropped", leaseGoneNext
}

// unsavedWork is what a release would lose from a container.
type unsavedWork struct {
	files    []string // git status --porcelain lines
	unpushed int      // commits on no remote branch, from any local branch or HEAD
}

func (w unsavedWork) any() bool { return len(w.files) > 0 || w.unpushed > 0 }

// describe names the work: the files (the first leaseGuardFileLimit of them)
// and the count of commits on no remote branch.
func (w unsavedWork) describe() string {
	return describeUnsaved(w.files, len(w.files), w.unpushed)
}

// describeUnsaved names unsaved work as a refused release does: count files —
// the first leaseGuardFileLimit of files by name — and unpushed commits.
func describeUnsaved(files []string, count, unpushed int) string {
	var parts []string
	if count > 0 {
		shown := files
		if len(shown) > leaseGuardFileLimit {
			shown = shown[:leaseGuardFileLimit]
		}
		more := ""
		if count > len(shown) {
			more = fmt.Sprintf(", first %d shown", len(shown))
		}
		parts = append(parts, fmt.Sprintf("%d uncommitted or untracked file(s) (%s%s)", count, strings.Join(shown, ", "), more))
	}
	if unpushed > 0 {
		parts = append(parts, fmt.Sprintf("%d commit(s) on no remote branch", unpushed))
	}
	return strings.Join(parts, " and ")
}

// leaseGitRead starts a git call that only reads a lease's checkout (DP-4):
// GIT_OPTIONAL_LOCKS=0 keeps `git status` from refreshing .git/index and
// writing it back — a new inode and mtime in the person's checkout, same
// bytes (live, 2026-10-23) — so a read writes nothing there.
const leaseGitRead = "GIT_OPTIONAL_LOCKS=0 git -C "

// leaseUnsavedFilesCommand is the guard's first read of the container's
// /var/www: its uncommitted and untracked files git does not ignore, one
// `git status --porcelain` line each (parseUnsavedFiles). The release guard
// and the status read (leaseActivityCommand) both run it, so they agree on
// what is unsaved.
func leaseUnsavedFilesCommand() string {
	return leaseGitRead + shellQuote(leaseWorkDir) + " status --porcelain"
}

// leaseUnpushedCommand is the guard's second read: how many commits are on no
// remote branch (parseUnpushedCount). A reset takes every commit in /var/www,
// so it counts what every local branch and HEAD reach — work left on a branch
// HEAD is not on, or on a detached HEAD, included — and none that origin's
// branches reach. HEAD is named only when it resolves: an unborn one (no
// commit yet, or an orphan branch) is no revision, while the branches still
// count — 0 in a checkout with no commit at all.
func leaseUnpushedCommand() string {
	wd := shellQuote(leaseWorkDir)
	return "if " + leaseGitRead + wd + " rev-parse -q --verify HEAD >/dev/null 2>&1; then " +
		leaseGitRead + wd + " rev-list --count --branches HEAD --not --remotes=origin; else " +
		leaseGitRead + wd + " rev-list --count --branches --not --remotes=origin; fi"
}

// parseUnsavedFiles reads leaseUnsavedFilesCommand's answer: one file per
// non-empty line.
func parseUnsavedFiles(out string) []string {
	var files []string
	for line := range strings.SplitSeq(out, "\n") {
		if line = strings.TrimRight(line, "\r "); line != "" {
			files = append(files, line)
		}
	}
	return files
}

// parseUnpushedCount reads leaseUnpushedCommand's answer: the count is its
// last field.
func parseUnpushedCount(out string) (int, error) {
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return 0, errors.New("git rev-list answered nothing")
	}
	n, err := strconv.Atoi(fields[len(fields)-1])
	if err != nil || n < 0 {
		return 0, fmt.Errorf("git rev-list answered %q", strings.TrimSpace(out))
	}
	return n, nil
}

// readUnsavedWork reads, over ssh, the uncommitted and untracked files and the
// commits on no remote branch of the container's /var/www, writing nothing
// into it (leaseGitRead).
func readUnsavedWork(ctx context.Context, ssh SSHDeployer, alias string) (unsavedWork, error) {
	var w unsavedWork
	out, err := ssh.ExecSSH(ctx, alias, leaseUnsavedFilesCommand())
	if err != nil {
		return w, fmt.Errorf("git status: %w", err)
	}
	w.files = parseUnsavedFiles(string(out))
	out, err = ssh.ExecSSH(ctx, alias, leaseUnpushedCommand())
	if err != nil {
		return w, fmt.Errorf("git rev-list: %w", err)
	}
	if w.unpushed, err = parseUnpushedCount(string(out)); err != nil {
		return w, err
	}
	return w, nil
}

// leaseGuardResult is what the release guard learned about the container.
type leaseGuardResult struct {
	// note is said to the agent on a release that went ahead unchecked.
	note string
	// lost: ssh could not reach the container, and the orchestrator says it
	// is no longer this lease's — why — so nothing was checked and nothing is
	// released (releaseGuarded drops the record as lost).
	lost string
	// woken: the release started the parked lease's container to check it
	// (wakeParkedForGuard, DP-28).
	woken bool
}

// guardLeaseRelease refuses a release that would lose work (DP-4): commits on
// no remote branch, or uncommitted/untracked files (inspectLeaseRelease).
func guardLeaseRelease(ctx context.Context, d LeaseDeps, rec leaseRecord) (leaseGuardResult, error) {
	guard, w, err := inspectLeaseRelease(ctx, d, rec)
	if err != nil {
		return leaseGuardResult{}, err
	}
	if w.any() {
		return leaseGuardResult{}, unsavedWorkRefusal(rec.alias(), w, IsHQRemote(rec.Repo, d.hqAddress()))
	}
	return guard, nil
}

// inspectLeaseRelease reads what releasing the lease would lose, changing
// nothing: the unsaved work it found (none for a release that may go ahead), or
// an error when zcp cannot tell. A container ssh cannot reach has no work zcp
// can save, so it passes unchecked when the orchestrator no longer holds it
// (unreachableGuard); any other failure to read it refuses (zcp cannot tell
// what would be lost).
func inspectLeaseRelease(ctx context.Context, d LeaseDeps, rec leaseRecord) (leaseGuardResult, unsavedWork, error) {
	if d.SSH == nil {
		return leaseGuardResult{note: "the container was not checked for unsaved work (this zcp has no ssh to it)"}, unsavedWork{}, nil
	}
	w, err := readUnsavedWork(ctx, d.SSH, rec.alias())
	if err != nil {
		if exitStatus(err) == leaseSSHUnreachableExit {
			guard, err := unreachableGuard(ctx, d, rec, err)
			return guard, unsavedWork{}, err
		}
		pe := platform.NewPlatformError(platform.ErrPrerequisiteMissing,
			fmt.Sprintf("Lease %s could not be checked for unsaved work, so it was not released", rec.alias()),
			"Look at the container's git state (git -C /var/www status) or release with force=true to discard whatever is there")
		pe.Cause = err
		return leaseGuardResult{}, unsavedWork{}, pe
	}
	return leaseGuardResult{}, w, nil
}

// unreachableGuard decides a release whose container ssh could not reach.
// ssh's 255 does not say the container is gone — an alias that does not
// resolve, a refused connection and a timeout all read the same — so the
// orchestrator decides (readLeaseOwnership): only a container that is no
// longer this lease's — a pool that is gone or being deleted holds none
// (DP-20) — is released unchecked (as lost); the release then says what
// became of it. One still this lease's may hold work and is refused, and so is
// one zcp cannot verify or ask about.
func unreachableGuard(ctx context.Context, d LeaseDeps, rec leaseRecord, sshErr error) (leaseGuardResult, error) {
	v := readLeaseOwnership(ctx, d, rec)
	if v.own.lost() {
		return leaseGuardResult{
			lost: v.why,
			note: "zcp could not reach the container over ssh and the orchestrator no longer holds it for this lease, so it was not checked for unsaved work",
		}, nil
	}
	state := "is still reserved"
	if v.own == leaseUnverified {
		state = "may still be reserved (the orchestrator did not answer: " + errorNote(v.err) + ")"
	}
	pe := platform.NewPlatformError(platform.ErrPrerequisiteMissing,
		fmt.Sprintf("Lease %s could not be reached over ssh to check for unsaved work and %s, so it was not released", rec.alias(), state),
		fmt.Sprintf("zerops_lease action=status shows whether its alias and mount are in place; release with force=true to discard whatever it holds: zerops_lease action=release lease=%s force=true", rec.alias()))
	pe.Cause = sshErr
	return leaseGuardResult{}, pe
}

// poolDeletionState names what a pool service's status says of its deletion:
// "is being deleted" (DELETING), "no longer exists" (DELETED), "" for any
// status of a pool that is still there. Either way out, its orchestrator
// holds nothing of a lease any more.
func poolDeletionState(status string) string {
	switch {
	case platform.ServiceDeleting(status):
		return "is being deleted"
	case platform.ServiceDeleted(status):
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
		if IsServiceGone(err) {
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
func unsavedWorkRefusal(alias string, w unsavedWork, onHQ bool) error {
	return platform.NewPlatformError(platform.ErrPrerequisiteMissing,
		fmt.Sprintf("Lease %s holds work that releasing would lose: %s", alias, w.describe()),
		fmt.Sprintf("%s, or release with force=true to discard it: zerops_lease action=release lease=%s force=true", saveWorkHint(onHQ, "the container"), alias))
}

// saveWorkHint is how to keep a lease's unsaved work: commit and push it from
// where ("the container", or each of several). onHQ: the lease's repository
// is on the Mate's HQ, which takes a push only on a branch of the agent's own,
// so the push it names keeps HQ's rule.
func saveWorkHint(onHQ bool, where string) string {
	if onHQ {
		return "Commit it on a branch of your own and push that branch from " + where + " — `git checkout -b <branch>` first if you are on `main`, then `git push -u origin <branch>`; HQ refuses a push of `main`, of a branch another Mate's open change holds, and any force push (zcp does not commit for you)"
	}
	return "Commit and push it from " + where + " (zcp does not commit for you)"
}
