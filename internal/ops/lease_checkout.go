package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// LeaseWorkDir is where a leased container keeps its code: the checkout an
// acquire puts on the lease's branch, where its supervisor runs and where
// `zcp git` on its alias runs git.
const LeaseWorkDir = "/var/www"

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

// leaseFromMissingMarker opens the line a checkout fails with when the branch
// it was to cut a new branch from is not on origin; leaseFromMissingExit is
// its exit status.
const (
	leaseFromMissingMarker = "the base branch is not on origin: "
	leaseFromMissingExit   = 5
)

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
// A query or a fragment (`?private_token=…`) is refused as userinfo is: the
// URL is kept verbatim in .git/config, zcp's records, the ssh command line and
// a failed checkout's message (review R4-9).
func validateLeaseRepo(raw string) (string, error) {
	bad := platform.NewPlatformError(platform.ErrInvalidParameter,
		"repo is not a clone URL a lease takes",
		"Pass an https://host/owner/repo URL with no credentials, query or fragment in it; the pool's GIT_TOKEN authenticates the clone")
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "https://") {
		return "", bad
	}
	if strings.ContainsFunc(raw, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) || strings.ContainsAny(raw, "?#") {
		return "", bad
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil {
		return "", bad
	}
	return raw, nil
}

// poolRepository is the repository a pool's leases are of, as its pair's
// ServiceMeta records it (the stand-up or the HQ wiring writes it; the one
// store, read through LeaseDeps.PoolRepository): the clone URL as recorded,
// and whether it was recorded wired in the Mate's HQ — an HQ repository,
// whose host is never the person's token's (DP-6), even once the Mate links
// to another HQ (D8). It carries no credential; compare it through
// topology.SameRepoURL, never as a string.
type poolRepository struct {
	Repo string
	HQ   bool
}

// poolRepository is the pool's recorded repository; the zero value when none
// is recorded or zcp has no reader of it.
func (d LeaseDeps) poolRepository(pool string) poolRepository {
	if d.PoolRepository == nil || !leasePoolNameRe.MatchString(pool) {
		return poolRepository{}
	}
	repo, onHQ := d.PoolRepository(pool)
	return poolRepository{Repo: strings.TrimSpace(repo), HQ: onHQ}
}

// resolveLeaseRepo is the clone URL an acquire clones, as it was given: the
// acquire's own, else the pool's recorded repository.
func resolveLeaseRepo(d LeaseDeps, pool, repo string) (string, error) {
	if strings.TrimSpace(repo) != "" {
		return validateLeaseRepo(repo)
	}
	have := d.poolRepository(pool).Repo
	if have == "" {
		return "", platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("acquire needs repo: pool '%s' has no repository recorded", pool),
			"Pass the clone URL as repo (https://host/owner/repo). A pool's repository is the one its stand-up records: zerops_standup records it for a pool that has none")
	}
	return validateLeaseRepo(have)
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
// same way. hqURL is the Mate's HQ, "" on a container not linked to one: a
// repository on it is fetched blobless (leaseFetchStep) with no helper of the
// command's own, as the command runs relayed (LeaseDeps.HQGit): the session's
// helper answers HQ with the Mate's project key, and nothing in the
// container holds a credential for HQ. It keeps only a helper that answers
// nothing and names `zcp git` to a plain caller (leaseHQHintHelperConfig).
// It is the checkout of the pool's own recorded repository: the pool's
// GIT_TOKEN answers its host (leaseTokenScope).
func leaseCheckoutCommand(workDir, repo, branch, base, hqURL string) string {
	return leaseCheckoutCommandFrom(workDir, repo, branch, base, "", hqURL, leaseHelperScope(repo))
}

// leaseCheckoutCommandFrom is leaseCheckoutCommand with from, the branch a
// branch new to origin is cut from instead of main when base is no commit id
// ("" for main). A from origin does not have fails the command naming it
// (exit leaseFromMissingExit), whatever the branch: nothing is cut from
// another commit instead. A branch origin has is tracked as it is there, and
// a base commit wins over from: from never rewrites a branch (DP-6). from is
// shell-quoted; the caller has validated it. tokenScope is the one host the
// pool's GIT_TOKEN answers for (leaseTokenScope).
func leaseCheckoutCommandFrom(workDir, repo, branch, base, from, hqURL, tokenScope string) string {
	q := shellQuote
	cutFrom := "origin/" + leaseDefaultBranch
	if from != "" {
		cutFrom = q("origin/" + from)
	}
	cut := "git checkout -q -f -B " + q(branch) + " --no-track " + cutFrom +
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
		leaseFetchStep(repo, hqURL, tokenScope),
	}
	if from != "" {
		steps = append(steps, "{ git rev-parse --verify -q "+q("refs/remotes/origin/"+from)+" >/dev/null || { echo "+
			q(leaseFromMissingMarker+from)+" >&2; exit "+strconv.Itoa(leaseFromMissingExit)+"; }; }")
	}
	steps = append(steps,
		"if git rev-parse --verify -q "+q("refs/remotes/origin/"+branch)+" >/dev/null; then "+
			"git checkout -q -f -B "+q(branch)+" --track "+q("origin/"+branch)+"; else "+cut+"; fi",
		"git clean -fd -q",
		// A fresh pool container carries no git identity; the agent's
		// first commit needs one (set-if-absent, never over the image's).
		gitIdentityEnsureFragment(),
	)
	if persist := leaseCredentialHelperConfig(repo, hqURL, tokenScope); persist != "" {
		steps = append(steps, persist)
	}
	if hint := leaseHQHintHelperConfig(repo, hqURL); hint != "" {
		steps = append(steps, hint)
	}
	// The container's stable number, which its preview port follows; only
	// the container knows it (leaseSlotMarker).
	steps = append(steps, `printf '`+leaseSlotMarker+`%s\n' "${ZEROPS_StableNumber:-}"`)
	return strings.Join(steps, " && ")
}

// leaseFetchStep is the checkout's fetch of origin: with the lease's own
// helper alone for a repository outside HQ (leaseCredentialHelperShell), and
// with none of its own for one on the Mate's HQ (hqURL), which a relayed
// session's helper answers — a `-c credential.helper=` would clear it.
// A repository on HQ is fetched blobless with the default refspec, as the
// Mate's dev checkout is (hq_base_branch.go): every branch's commits and
// trees, so merge-base, rebase and log are whole, and file contents only as
// git needs them — HEAD's at the checkout, older ones on demand through the
// relay a `zcp git <alias>` runs in (git records origin as the promisor
// remote, so later fetches keep the filter). A narrowed refspec would save
// about 1.6 % and break a new branch's cut, `from` and the release guard's
// count (followup-lease §1.4).
func leaseFetchStep(repo, hqURL, tokenScope string) string {
	if IsHQRemote(repo, hqURL) {
		return "GIT_TERMINAL_PROMPT=0 git fetch -q --prune --filter=blob:none origin"
	}
	return "GIT_TERMINAL_PROMPT=0 git " + leaseInlineHelperArgs(leaseCredentialHelperShell(tokenScope)) + " fetch -q --prune origin"
}

// leaseCredentialHelperShell is the helper a lease authenticates with outside
// the Mate's HQ — inline at the checkout's fetch and a warm clone's, and kept
// in the repository for its origin's host: it answers a get with the invoking
// session's $GIT_TOKEN (the pool's secret reaches every ssh session in its
// containers), so the agent's own push over the alias needs no credential
// typed or stored; the config holds only this script. The pool's GIT_TOKEN is
// the person's token for one git host, so the helper reads the host= git asks
// for and answers only scope — that host, with its port when not 443
// (leaseTokenScope) — and nothing for any other host, nor without GIT_TOKEN
// (git run through the mount on the zcp side), never an empty password, so git
// goes on to its next helper (review R4-2). "" when scope is no host it can
// match: no helper at all. The reset at release takes it away with /var/www.
// The helper reads git's request to its end and matches its host= — the
// host, with its port when the URL names one — against scope, letter case
// aside (hostCasePattern).
func leaseCredentialHelperShell(scope string) string {
	pattern, ok := hostCasePattern(scope)
	if !ok {
		return ""
	}
	return `!f() { test "$1" = get || return 0; test -n "$` + GitTokenEnvKey + `" || return 0; h=; ` +
		`while IFS= read -r l; do case $l in host=*) h=${l#host=};; esac; done; ` +
		`case $h in ` + pattern + `) ;; *) return 0;; esac; ` +
		`echo username=oauth2; echo "password=$` + GitTokenEnvKey + `"; }; f`
}

// hostCasePattern is a shell case pattern matching scope — a host, lowercase
// or not, with an optional :port — in any letter case, as git passes the host
// the URL names: each letter as a bracket pair of its two cases, digits, dots,
// hyphens and the port's colon as they are; a scope without a port also
// matches its explicit :443. false for an empty scope or one holding anything
// else, which no pattern is built from.
func hostCasePattern(scope string) (string, bool) {
	if scope == "" {
		return "", false
	}
	var b strings.Builder
	for _, r := range strings.ToLower(scope) {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteString("[" + string(r) + strings.ToUpper(string(r)) + "]")
		case r >= '0' && r <= '9', r == '.', r == '-', r == ':':
			b.WriteRune(r)
		default:
			return "", false
		}
	}
	pattern := b.String()
	if !strings.Contains(scope, ":") {
		pattern += "|" + pattern + ":" + defaultHTTPSPort
	}
	return pattern, true
}

// leaseInlineHelperArgs are the `-c` arguments that hand one git call helper
// alone: the reset drops every helper configured before it; without a helper
// git asks none.
func leaseInlineHelperArgs(helper string) string {
	if helper == "" {
		return "-c credential.helper="
	}
	return "-c credential.helper= -c credential.helper=" + shellQuote(helper)
}

// leaseHelperScope is the scope a lease's helper answers for remoteURL: its
// host, lowercase, with its port when it names one other than 443
// (scopeWithPort); "" for a remote that is not https or whose host can be no
// scope (gitCredentialScopeHost).
func leaseHelperScope(remoteURL string) string {
	if !strings.HasPrefix(strings.ToLower(remoteURL), httpsScheme+"://") {
		return ""
	}
	host, ok := gitCredentialScopeHost(remoteURL)
	if !ok {
		return ""
	}
	return strings.ToLower(scopeWithPort(remoteURL, host))
}

// leaseCredentialHelperConfig is the git command that persists the lease's
// helper for an https repo outside the Mate's HQ (hqURL), scoped by the rule
// a dev service's checkout is (persistedHelperScope); "" for a repository on
// HQ — whose git the agent runs relayed, `zcp git <alias>`, so the container
// keeps no credential for it (leaseHQHintHelperConfig) — any other repo, a host that can be no scope, or a
// repository on another host than tokenScope, which its helper would never
// answer.
func leaseCredentialHelperConfig(repo, hqURL, tokenScope string) string {
	if !strings.HasPrefix(repo, httpsScheme+"://") || IsHQRemote(repo, hqURL) {
		return ""
	}
	scope, _, ok := persistedHelperScope(repo, hqURL, "", "")
	if !ok || !strings.EqualFold(scope, tokenScope) {
		return ""
	}
	helper := leaseCredentialHelperShell(tokenScope)
	if helper == "" {
		return ""
	}
	return "git config --replace-all " + shellQuote("credential.https://"+scope+".helper") + " " + shellQuote(helper)
}

// leaseHQHintMessage is what a git outside zcp's relayed session in a lease
// of an HQ repository prints when it asks HQ for a credential — a fetch, a
// push, or the lazy fetch of an old file's contents the blobless checkout
// (leaseFetchStep) left on HQ.
const leaseHQHintMessage = "zcp: this repository is on the Mate's HQ and this git ran outside zcp, so it has no credential for HQ: " +
	"run it as zcp git <alias> <git args> from the Mate. Old file contents (log -p, blame, show <old>:<file>, a diff with an old commit) " +
	"come from HQ on demand, so a tool reading them outside zcp git fails here"

// leaseHQHintHelperConfig is the git command that persists, for a repository
// on the Mate's HQ alone, a credential helper scoped to HQ's host that answers
// nothing, says why (leaseHQHintMessage) and answers quit=1, so git stops
// there and never prompts, a terminal under `ssh -t` included: a plain ssh git or a tool in the
// container that needs HQ — an old blob of the blobless checkout above all —
// then fails naming `zcp git` rather than with git's bare "could not read
// Username". A relayed session clears every persisted helper before its own
// (credential.helper= first: buildRelayedScriptCommand, `zcp git`'s argv), so
// only plain callers ever reach it. "" for any other repository.
func leaseHQHintHelperConfig(repo, hqURL string) string {
	scope := leaseHelperScope(repo)
	if scope == "" || !IsHQRemote(repo, hqURL) {
		return ""
	}
	helper := `!f() { [ "$1" = get ] || return 0; echo ` + shellQuote(leaseHQHintMessage) + ` >&2; echo quit=1; }; f`
	return "git config --replace-all " + shellQuote("credential.https://"+scope+".helper") + " " + shellQuote(helper)
}

// The pool's GIT_TOKEN is the person's token for one git host, and the
// repository an acquire names may be on any (a URL from a README, an issue or
// a page): so it answers one scope (leaseTokenScope), and an acquire naming
// a repository on another host fetches without it (review R4-2).

// poolTokenScopePath is zcp's record of the scope a pool's GIT_TOKEN answers
// when its recorded repository names none outside the Mate's HQ
// (.zcp/state/leases/pools/<pool>.token-scope.json, mode 0600).
func poolTokenScopePath(stateDir, pool string) string {
	return filepath.Join(leaseDir(stateDir), "pools", pool+".token-scope.json")
}

// poolTokenScope is that record: a host, with its port when not 443, never a
// credential.
type poolTokenScope struct {
	Scope string `json:"scope"`
}

// readPoolTokenScope is the scope zcp recorded for the pool's GIT_TOKEN, ""
// when it recorded none (or the record names nothing a helper matches).
func readPoolTokenScope(stateDir, pool string) string {
	if stateDir == "" || !leasePoolNameRe.MatchString(pool) {
		return ""
	}
	raw, err := os.ReadFile(poolTokenScopePath(stateDir, pool))
	if err != nil {
		return ""
	}
	var rec poolTokenScope
	if json.Unmarshal(raw, &rec) != nil {
		return ""
	}
	if _, ok := hostCasePattern(rec.Scope); !ok {
		return ""
	}
	return rec.Scope
}

// storePoolTokenScope records scope as the one the pool's GIT_TOKEN answers,
// atomically with mode 0600.
func storePoolTokenScope(stateDir, pool, scope string) error {
	if stateDir == "" || !leasePoolNameRe.MatchString(pool) {
		return errors.New("no state directory or an unusable pool name")
	}
	path := poolTokenScopePath(stateDir, pool)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	data, err := json.Marshal(poolTokenScope{Scope: scope})
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := atomicWriteFile(path, data); err != nil {
		return fmt.Errorf("token scope of %s: %w", pool, err)
	}
	return nil
}

// leaseTokenScope is the one scope the pool's GIT_TOKEN answers, and whether
// repo binds it: the host of the pool's recorded repository when that is
// outside the Mate's HQ (DP-5); else the scope zcp recorded when it bound the
// token; else — no repository outside HQ and nothing bound yet, as in a Mate
// whose pools are of HQ repositories — repo's own host, which binds it once
// its checkout worked (bind true). "" when none applies (repo "" or on HQ):
// no GIT_TOKEN helper at all.
func leaseTokenScope(d LeaseDeps, pool, repo string) (scope string, bind bool) {
	hqURL := d.hqAddress()
	def := d.poolRepository(pool)
	if def.Repo != "" && !def.HQ && !IsHQRemote(def.Repo, hqURL) {
		return leaseHelperScope(def.Repo), false
	}
	if recorded := readPoolTokenScope(d.StateDir, pool); recorded != "" {
		return recorded, false
	}
	if repo == "" || IsHQRemote(repo, hqURL) || retiredHQDefault(def, repo, hqURL) {
		return "", false
	}
	scope = leaseHelperScope(repo)
	return scope, scope != ""
}

// hqAddress is the address of the HQ this container presents itself to, read now; ""
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
func checkoutLease(ctx context.Context, d LeaseDeps, rec leaseRecord, repo, branch, base, from string) (leaseCheckedOut, error) {
	hqURL := d.hqAddress()
	tokenScope, bind := leaseTokenScope(d, rec.PoolHostname, repo)
	onHQ := IsHQRemote(repo, hqURL)
	out, err := d.gitSSH(onHQ).ExecSSH(ctx, rec.alias(), leaseCheckoutCommandFrom(LeaseWorkDir, repo, branch, base, from, hqURL, tokenScope))
	if err == nil {
		if bind {
			_ = storePoolTokenScope(d.StateDir, rec.PoolHostname, tokenScope)
		}
		cut, _ := checkoutBase(string(out))
		return leaseCheckedOut{base: cut, stableNumber: checkoutSlot(string(out)), warm: checkoutWarm(string(out))}, nil
	}
	code, suggestion := classifyCheckoutFailure(err, onHQ)
	tokenCode := code == platform.ErrGitTokenMissing || code == platform.ErrGitTokenInvalid
	switch repoScope := leaseHelperScope(repo); {
	case tokenCode && retiredHQDefault(d.poolRepository(rec.PoolHostname), repo, hqURL):
		code, suggestion = platform.ErrPrerequisiteMissing, retiredHQDefaultSuggestion(repo)
	case tokenCode && repoScope != "" && !strings.EqualFold(repoScope, tokenScope):
		code, suggestion = platform.ErrPrerequisiteMissing, foreignHostSuggestion(repoScope, tokenScope)
	}
	pe := platform.NewPlatformError(code,
		fmt.Sprintf("Lease %s could not put %s on branch %s of %s", rec.alias(), LeaseWorkDir, branch, repo), suggestion)
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
// whose git zcp answers with the Mate's project key, no user's token
// (hqLeaseCredentialSuggestion).
func classifyCheckoutFailure(err error, onHQ bool) (code, suggestion string) {
	var sshErr *platform.SSHExecError
	out := ""
	if errors.As(err, &sshErr) {
		out = strings.ToLower(sshErr.Output)
	}
	switch {
	case containsAny(out, "could not read username", "could not read password", "terminal prompts disabled"):
		if onHQ {
			return platform.ErrPrerequisiteMissing, hqLeaseCredentialSuggestion("git asked for a credential and nothing answered it: that git ran outside zcp's relayed session (a plain ssh or git), which stores no credential in the container")
		}
		return platform.ErrGitTokenMissing,
			"git asked for a credential and the container's session had none: the pool's GIT_TOKEN secret is not set (or is empty). Private repositories need it"
	case containsAny(out, "authentication failed", "invalid username or password", "invalid credentials",
		"permission denied", "error: 401", "error: 403", "password authentication was removed"):
		if onHQ {
			return platform.ErrPrerequisiteMissing, hqLeaseCredentialSuggestion("HQ refused the Mate's project key zcp presented: the Mate is not linked to HQ, or HQ no longer admits it (the Mate's /mate/setup.json says why)")
		}
		return platform.ErrGitTokenInvalid,
			"The remote refused the pool's GIT_TOKEN (bad, expired or without access to this repository)"
	}
	if strings.Contains(out, "while it was warm") {
		return platform.ErrSSHDeployFailed,
			"A clone zcp started in the container while it was warm would not stop or could not be removed, so nothing was checked out on top of it. The container went back with a reset, which takes the clone away: acquire again"
	}
	if why, ok := leaseNotAnswering(err); ok && why == leaseProcessLimitWhy {
		return platform.ErrSSHDeployFailed,
			"The container is not answering: " + why + ". Tell the person rather than wait on it or retry in a loop: it does not recover by itself while what fills it runs"
	}
	if exitStatus(err) == leaseSSHUnreachableExit {
		return platform.ErrSSHDeployFailed,
			"The container did not answer ssh. Check that the pool's sshIsolation admits this service and that zerops_lease action=status shows the alias installed"
	}
	if strings.Contains(out, leaseFromMissingMarker) {
		return platform.ErrInvalidParameter,
			"The branch named as from is not on origin, so nothing was cut from it and the container went back. Pass a branch origin has (git ls-remote --heads origin over ssh in any lease lists them), or leave from out to cut the branch from main"
	}
	if strings.Contains(out, "base commit") && strings.Contains(out, "is not on origin") {
		return platform.ErrSSHDeployFailed,
			"The branch is new to origin, and its other leases were cut from a main commit origin no longer has (main was rewritten since). Push the branch from one of them (git push -u origin <branch>), then acquire again: a branch origin has is checked out as it is there"
	}
	return platform.ErrSSHDeployFailed,
		"Check the repo URL and that the container can reach the git host"
}

// retiredHQDefault reports whether repo is the pool's repository recorded
// wired in the Mate's HQ (def.HQ) while that HQ is not the one the Mate links
// to now (hqURL): an HQ repository the Mate left, whose host never was the
// person's token's.
func retiredHQDefault(def poolRepository, repo, hqURL string) bool {
	return def.HQ && topology.SameRepoURL(def.Repo, repo) && !IsHQRemote(repo, hqURL)
}

// retiredHQDefaultSuggestion is what a checkout of the pool's repository says
// when it is an HQ repository the Mate has left (retiredHQDefault) and git
// asked for a credential there: no person's token answers it.
func retiredHQDefaultSuggestion(repo string) string {
	return "The repository " + repo + " is the pool's repository, recorded while it was on this Mate's HQ, and the Mate links to another HQ now: no person's token answers it. " +
		"Name the repository on the HQ this Mate links to now with repo=<its address there> (zerops_workflow action=\"status\" names each pair's repository); the next repository pass moves the pool's record to it. Never ask for a token or make one up"
}

// foreignHostSuggestion is what a checkout says when git asked for a
// credential for repoScope, a host the pool's GIT_TOKEN is not for: the token
// answers tokenScope alone ("" for none), so git asked without it. It is no
// missing token, and the person's token is never sent to a host an acquire
// names.
func foreignHostSuggestion(repoScope, tokenScope string) string {
	bound := "the pool's GIT_TOKEN answers no host outside HQ yet"
	if tokenScope != "" {
		bound = "the pool's GIT_TOKEN is the person's token for " + tokenScope + " (the host of the pool's repository, or of its first lease outside HQ) and answers no other host"
	}
	return "The repository on " + repoScope + " asked for a credential, and " + bound +
		", so git asked without it. Check that the repo URL is the one the person meant; a private repository on " + repoScope +
		" is leased from a pool whose recorded repository is there. Never ask for the token's value or make one up"
}

// hqLeaseCredentialSuggestion is what a lease of a repository on the Mate's
// HQ says when its git reached HQ without the Mate's project key, or HQ
// refused it (why). zcp answers HQ's git over its own relayed session alone
// (LeaseDeps.HQGit, `zcp git <alias>`); the pool stores no credential for it,
// and it is no user's token to ask for.
func hqLeaseCredentialSuggestion(why string) string {
	return "The repository is on this Mate's HQ, whose git zcp answers with this Mate's project key over its own ssh session, and " + why +
		". Run git against it through zcp: zcp git <alias> <git args>. Once the Mate is linked again, try again. " +
		"It is no user's token, so never ask for one or make one up"
}

// gitCredentialRefused reports whether err is git asking for a credential the
// session had none of, or the remote refusing the one it presented — the
// failures classifyCheckoutFailure names as credential ones.
func gitCredentialRefused(err error) bool {
	var sshErr *platform.SSHExecError
	if !errors.As(err, &sshErr) {
		return false
	}
	return containsAny(strings.ToLower(sshErr.Output),
		"could not read username", "could not read password", "terminal prompts disabled",
		"authentication failed", "invalid username or password", "invalid credentials",
		"error: 401", "error: 403")
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// exitStatus is the exit status ssh ended with behind err, -1 when err has
// none (platform.SSHExitCode, the one reader of it).
func exitStatus(err error) int {
	return platform.SSHExitCode(err)
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
	case view.containerRemoved && view.fullPoolMax > 0:
		return fmt.Sprintf("the container was removed with the lease (the pool is full at its maximum of %d containers, so a container kept warm would lose its preview route; the pool's next acquire creates one) and the lease dropped", view.fullPoolMax),
			leaseGoneNext
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
	return leaseGitRead + shellQuote(LeaseWorkDir) + " status --porcelain"
}

// leaseUnpushedCommand is the guard's second read: how many commits are on no
// remote branch (parseUnpushedCount). A reset takes every commit in /var/www,
// so it counts what every local branch and HEAD reach — work left on a branch
// HEAD is not on, or on a detached HEAD, included — and none that origin's
// branches reach. HEAD is named only when it resolves: an unborn one (no
// commit yet, or an orphan branch) is no revision, while the branches still
// count — 0 in a checkout with no commit at all.
func leaseUnpushedCommand() string {
	wd := shellQuote(LeaseWorkDir)
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
// no remote branch, or uncommitted/untracked files (inspectUnsavedWork).
func guardLeaseRelease(ctx context.Context, d LeaseDeps, rec leaseRecord) (leaseGuardResult, error) {
	guard, w, err := inspectUnsavedWork(ctx, d, rec)
	if err != nil {
		return leaseGuardResult{}, err
	}
	if w.any() {
		return leaseGuardResult{}, unsavedWorkRefusal(rec.alias(), w, IsHQRemote(rec.Repo, d.hqAddress()))
	}
	return guard, nil
}

// inspectUnsavedWork is the one read of whether a lease's work is saved,
// for a release of the lease alone (guardLeaseRelease) and of its branch:
// inspectLeaseRelease, then for commits on no branch of origin the compare
// with origin (compareWithOrigin) and with origin's main, relayed for a
// repository on the Mate's HQ (mainHoldsUnpushed): commits main already holds
// — merged as a squash — are no work to keep. A compare that could not run is
// the lease's refusal (leaseCompareRefusal), never a count it could not check.
func inspectUnsavedWork(ctx context.Context, d LeaseDeps, rec leaseRecord) (leaseGuardResult, unsavedWork, error) {
	guard, w, err := inspectLeaseRelease(ctx, d, rec)
	if err != nil || w.unpushed == 0 {
		return guard, w, err
	}
	onHQ := IsHQRemote(rec.Repo, d.hqAddress())
	if w, err = compareWithOrigin(ctx, d, rec, w, onHQ); err != nil || w.unpushed == 0 {
		return guard, w, err
	}
	held, err := mainHoldsUnpushed(ctx, d, rec.alias(), onHQ)
	if err != nil {
		return guard, w, leaseCompareRefusal(rec.alias(), w.unpushed, "compare their change with origin's main", err, onHQ)
	}
	if held {
		guard.note = joinNotes(guard.note, fmt.Sprintf("its %d commit(s) are on no branch of origin, but origin's main already holds what they change — merged as a squash — so releasing loses nothing", w.unpushed))
		w.unpushed = 0
	}
	return guard, w, nil
}

// leaseGuardFetchCommand brings the checkout's remote-tracking refs up to
// origin's branches — nothing else in the checkout moves — authenticating
// through the helper the lease's checkout persisted (DP-6), or, for a
// repository on the Mate's HQ, through the relayed session it runs in.
func leaseGuardFetchCommand() string {
	return "GIT_TERMINAL_PROMPT=0 git -C " + shellQuote(LeaseWorkDir) + " fetch -q --prune origin"
}

// leaseMainHoldsMark is the last line leaseMainHoldsCommand prints where
// origin's main already holds every unpushed commit's change.
const leaseMainHoldsMark = "main-holds-them"

// leaseMainHoldsCommand asks whether origin's main already holds what the
// checkout's unpushed commits change: for each local branch tip and HEAD
// that reaches a commit origin's branches do not, the change from its merge
// base with main to the tip must reverse-apply cleanly onto main's tree. A
// change merged as a squash or a rebase keeps its content, not its commits,
// so counted by commit it reads as work a release would lose, and HQ's
// merge pass — which releases through this guard — could never release it.
// Main later changing those lines again, or a tip with no history in common
// with main, reads as not held: never a release of work main lacks. It reads
// only: the index it applies against is a temporary file, never the
// checkout's, and nothing in /var/www moves. A step that could not run —
// above all git dying (128) on an old file's contents a blobless checkout
// could not fetch — ends it with leaseMainHoldsFailedExit and git's own
// stderr, never as "not held": only merge-base's "no common history" (1) and
// apply's "does not apply" (1) are answers.
func leaseMainHoldsCommand() string {
	g := leaseGitRead + shellQuote(LeaseWorkDir)
	fail := "exit " + strconv.Itoa(leaseMainHoldsFailedExit)
	answer := func(step string) string { // 0 goes on, 1 is "not held", anything else could not run
		return step + "; case $? in 0) ;; 1) exit 0;; *) " + fail + ";; esac; "
	}
	return "main=refs/remotes/origin/main; " +
		g + " rev-parse -q --verify \"$main\" >/dev/null 2>&1 || main=refs/remotes/origin/master; " +
		g + " rev-parse -q --verify \"$main\" >/dev/null 2>&1 || exit 0; " +
		"tmp=$(mktemp -d) || " + fail + "; trap 'rm -rf \"$tmp\"' EXIT; " +
		// The tips: a listing that fails ends the compare as not run, never
		// as "held" over no tips; an unborn HEAD (rev-parse's 1) has none.
		"heads=$(" + g + " for-each-ref --format='%(objectname)' refs/heads) || " + fail + "; " +
		"head=$(" + g + " rev-parse -q --verify HEAD); case $? in 0|1) ;; *) " + fail + ";; esac; " +
		"for tip in $(printf '%s\\n%s\\n' \"$head\" \"$heads\" | sort -u); do " +
		"[ \"$(" + g + " rev-list --count \"$tip\" --not --remotes=origin)\" = 0 ] && continue; " +
		answer("mb=$("+g+" merge-base \"$main\" \"$tip\")") +
		g + " diff --binary \"$mb\" \"$tip\" > \"$tmp/change\" || " + fail + "; " +
		"[ -s \"$tmp/change\" ] || continue; " +
		"GIT_INDEX_FILE=\"$tmp/index\" " + g + " read-tree \"$main\" || " + fail + "; " +
		answer("GIT_INDEX_FILE=\"$tmp/index\" "+g+" apply --cached --check --reverse \"$tmp/change\" 2>\"$tmp/apply\" >/dev/null; rc=$?; [ $rc = 1 ] || cat \"$tmp/apply\" >&2; (exit $rc)") +
		"done; echo " + leaseMainHoldsMark
}

// leaseMainHoldsFailedExit is leaseMainHoldsCommand's exit status when a step
// of the compare could not run.
const leaseMainHoldsFailedExit = 3

// mainHoldsUnpushed runs leaseMainHoldsCommand in the lease's container
// through ssh — relayed for a repository on the Mate's HQ (LeaseDeps.gitSSH),
// whose blobless checkout fetches the old contents the diff and the apply
// read through it: true only where it printed its mark, false where it
// answered "not held", and an error carrying git's or ssh's own words where
// the compare could not run.
func mainHoldsUnpushed(ctx context.Context, d LeaseDeps, alias string, onHQ bool) (bool, error) {
	out, err := d.gitSSH(onHQ).ExecSSH(ctx, alias, leaseMainHoldsCommand())
	if err != nil {
		return false, err
	}
	fields := strings.Fields(string(out))
	return len(fields) > 0 && fields[len(fields)-1] == leaseMainHoldsMark, nil
}

// compareWithOrigin counts the commits on no remote branch (w.unpushed) again
// once the checkout has fetched origin's branches: a commit pushed from
// elsewhere — the pair's dev service, the Mate's own shell — is on origin
// while the lease's remote-tracking refs still predate it, and counted
// against those it reads as work a release would lose (D9, live 2026-10-27).
// A repository on the Mate's HQ fetches relayed (LeaseDeps.HQGit). A fetch
// or a recount that fails is a refusal saying the guard could not compare
// and why (leaseCompareRefusal) — never the count it could not check, which
// would tell the agent to push commits origin may already hold (D8).
func compareWithOrigin(ctx context.Context, d LeaseDeps, rec leaseRecord, w unsavedWork, onHQ bool) (unsavedWork, error) {
	alias := rec.alias()
	if _, err := d.gitSSH(onHQ).ExecSSH(ctx, alias, leaseGuardFetchCommand()); err != nil {
		return w, leaseCompareRefusal(alias, w.unpushed, "fetch origin's branches", err, onHQ)
	}
	out, err := d.SSH.ExecSSH(ctx, alias, leaseUnpushedCommand())
	if err != nil {
		return w, leaseCompareRefusal(alias, w.unpushed, "count them again after fetching origin", err, onHQ)
	}
	n, err := parseUnpushedCount(string(out))
	if err != nil {
		return w, leaseCompareRefusal(alias, w.unpushed, "count them again after fetching origin", err, onHQ)
	}
	w.unpushed = n
	return w, nil
}

// leaseCompareRefusal refuses a release whose guard counted unpushed commits
// on no branch of origin but could not compare them with origin — step names
// what did not run, err is its failure in git's or ssh's own words — so it
// cannot tell whether they are saved. It is never unsavedWorkRefusal: the
// commits may be on origin already. A credential refusal says whose
// credential and never asks for a token (classifyCheckoutFailure on HQ).
func leaseCompareRefusal(alias string, unpushed int, step string, err error, onHQ bool) error {
	force := fmt.Sprintf("Once you know origin has them, release with force=true: zerops_lease action=release lease=%s force=true", alias)
	suggestion := "zcp could not " + step + " (" + sshFailureText(err) + "). Look at the cause. If they are not on origin, save them: " + saveWorkHint(onHQ, "the container") + ". " + force
	switch {
	case gitCredentialRefused(err) && onHQ:
		_, suggestion = classifyCheckoutFailure(err, onHQ)
		suggestion += ". " + force
	case gitCredentialRefused(err):
		suggestion = "The repository's host did not take the pool's GIT_TOKEN when zcp tried to " + step + " (" + sshFailureText(err) +
			"), so zcp could not tell whether origin has them; the person's token is never asked for here. Push them from the container if they are not on origin. " + force
	}
	pe := platform.NewPlatformError(platform.ErrPrerequisiteMissing,
		fmt.Sprintf("Lease %s has %d commit(s) its checkout does not see on origin, and zcp could not compare them with origin, so it cannot tell whether they are saved; nothing was released", alias, unpushed),
		suggestion)
	pe.Cause = err
	return pe
}

// inspectLeaseRelease reads what releasing the lease would lose, changing
// nothing: the unsaved work it found (none for a release that may go ahead), or
// an error when zcp cannot tell. A container ssh cannot reach has no work zcp
// can save, so it passes unchecked when the orchestrator no longer holds it
// (unreachableGuard); any other failure to read it refuses (zcp cannot tell
// what would be lost) — and so does a zcp with no ssh to it at all (local
// mode): only force releases what nobody checked.
func inspectLeaseRelease(ctx context.Context, d LeaseDeps, rec leaseRecord) (leaseGuardResult, unsavedWork, error) {
	if d.SSH == nil {
		return leaseGuardResult{}, unsavedWork{}, platform.NewPlatformError(platform.ErrPrerequisiteMissing,
			fmt.Sprintf("Lease %s could not be checked for unsaved work — this zcp has no ssh to the container — so it was not released", rec.alias()),
			fmt.Sprintf("Release it from a zcp in the project, which reaches the container over ssh, or release with force=true to discard whatever it holds: zerops_lease action=release lease=%s force=true", rec.alias()))
	}
	w, err := readUnsavedWork(ctx, d.SSH, rec.alias())
	noteLeaseAnswer(d.StateDir, rec, err, d.now())
	if err != nil {
		if why, ok := leaseNotAnswering(err); ok && why == leaseProcessLimitWhy {
			// Still this lease's container, at its process limit: what it
			// holds cannot be read, so only force releases it.
			pe := leaseNotAnsweringError(rec.alias(), why, "zcp could not check it for unsaved work and did not release it", err)
			pe.Suggestion += fmt.Sprintf(". Releasing it unchecked (zerops_lease action=release lease=%s force=true) discards whatever it holds unpushed: only after the person agreed", rec.alias())
			return leaseGuardResult{}, unsavedWork{}, pe
		}
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
		return "Commit it on a branch of your own in " + where + " and push that branch through zcp — `git checkout -b <branch>` first if you are on `main`, then `zcp git <alias> push -u origin <branch>`, as a plain `git push` there holds no credential for HQ; HQ refuses a push of `main`, of a branch another Mate's open change holds, and any force push (zcp does not commit for you)"
	}
	return "Commit and push it from " + where + " (zcp does not commit for you)"
}
