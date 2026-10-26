package ops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// A pool's warm container takes an acquire at once, but the checkout still
// cloned the whole repository into its empty /var/www: on z3's 86 MB / 24k-file
// tree that was most of a 10.8 s acquire (live, 2026-10-24). So right after zcp
// creates a pool's warm container, keeps one at a release or finds one at an
// acquire, it starts — detached in the container, over ssh to its hostname — a
// clone of the repository the pool's last lease checked out, at origin/main,
// into /var/www; the next acquire then only fetches what changed and checks its
// branch out (docs/spec-workflows.md §8 DP-32).
//
// The container is free while the clone runs, so the clone and a checkout
// meet under one rule in the container: both hold a flock on /var/www itself —
// the clone for its whole run, taken without waiting and only on a /var/www
// holding no repository; the checkout for its whole run, waiting. The clone
// writes its state in .git/zcp-warm (the repository's identity, its process
// group, the commit it checks out, `done` once it finished). The checkout takes
// a finished clone of its own repository, waits for one still running, stops
// one of another repository, and removes whatever clone is not a finished one
// of its own repository — every file of the commit it checked out, and its
// .git — before it checks out as on an empty /var/www. A reset takes the clone
// away with /var/www, which leaves the empty /var/www today's checkout expects.
//
// zcp writes into a free container only right after reading it by id as free,
// only for a pool it leased a repository from, and — for a repository on the
// Mate's HQ — only once the pool's HQ credential (PoolHQCredentialEnvKey) is
// this Mate's current one by zcp's own record (DP-16): the clone authenticates
// exactly as a checkout does — a repository outside HQ from the pool's
// GIT_TOKEN, answered only to the host it is bound to (leaseTokenScope), one
// on HQ from the HQ credential, which no other repository's helper reads —
// and writes no credential, helper or identity.

// leaseWarmStateFile is the clone's state, inside the clone's .git so it lives
// and goes with the clone.
const leaseWarmStateFile = "zcp-warm"

// leaseWarmCloneWait is how long a checkout waits for a running clone of its
// own repository before it stops it: a clone that started before the checkout
// finishes no later than a fresh one would, and a stalled transfer ends itself
// (GIT_HTTP_LOW_SPEED_*).
const leaseWarmCloneWait = 3 * time.Minute

// leaseWarmCloneLaunchTimeout bounds one try of a launch: the ssh call returns
// as soon as the clone is detached, well under a second.
const leaseWarmCloneLaunchTimeout = 10 * time.Second

// A launch ssh could not run is tried again, up to leaseWarmLaunchTries times
// leaseWarmLaunchRetry apart: ssh itself failing (exit 255) — as it once did
// while a release reinstalled the alias file beside the clone (live,
// 2026-10-24: "Permission denied"), which the alias file's rename into place
// now rules out — or a try that ran out of time before ssh
// got in, as the first connection to a container its create had just answered
// did, dropped until the container admitted zcp a second later (live,
// 2026-10-24). The launch changes nothing until it runs, and one that ran
// finds the clone there the second time.
const (
	leaseWarmLaunchTries = 4
	leaseWarmLaunchRetry = time.Second
)

// leaseWarmLaunchMarker opens the launch's answer; leaseWarmCheckoutMarker the
// line a checkout says what it did with a clone on.
const (
	leaseWarmLaunchMarker   = "@@zcp-warm "
	leaseWarmCheckoutMarker = "@@zcp-lease warm "
)

// What a launch answers.
const (
	warmLaunchStarted = "started"
	warmLaunchPresent = "present"
	warmLaunchTools   = "tools"
)

// What a checkout did with a clone (leaseCheckedOut.warm).
const (
	leaseWarmUsed      = "used"
	leaseWarmDiscarded = "discarded"
)

// A checkout refuses, with these words, when a clone would not stop or cannot
// be read to be removed (classifyCheckoutFailure).
const (
	leaseWarmBusy       = "a clone zcp started in the container while it was warm still holds /var/www"
	leaseWarmUnreadable = "a clone zcp started in the container while it was warm cannot be read to be removed"
)

// What zcp last did to clone a pool's repository into a warm container
// (LeaseWarmClone.Outcome).
const (
	// LeaseWarmCloneStarted: the clone runs, detached, in the container.
	LeaseWarmCloneStarted = "started"
	// LeaseWarmCloneSkipped: nothing was written; Note says why.
	LeaseWarmCloneSkipped = "skipped"
	// LeaseWarmCloneFailed: the launch failed; Note says why.
	LeaseWarmCloneFailed = "failed"
)

// LeaseWarmClone is what zcp last did to clone a pool's repository into a warm
// container, kept per pool and reported by status. Started means the clone
// runs in the container; whether it finished only the container knows, and the
// next checkout acts on it either way.
type LeaseWarmClone struct {
	At string `json:"at"` // RFC 3339
	// Container is the container's hostname; "" when no container was read.
	Container string `json:"container,omitempty"`
	// Repo is the repository's identity (topology.CanonicalRepoURL).
	Repo    string `json:"repo"`
	Outcome string `json:"outcome"`
	Note    string `json:"note,omitempty"`
}

// leaseWarmRepoKey is the repository's identity as a clone and a checkout
// compare it: the canonical form, case folded (topology.SameRepoURL), hashed,
// so no URL is ever composed into the comparison.
func leaseWarmRepoKey(repo string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(topology.CanonicalRepoURL(repo))))
	return hex.EncodeToString(sum[:16])
}

// leaseWarmCloneCommand starts, detached, the clone of repo at origin/main into
// workDir, and answers on a leaseWarmLaunchMarker line: started, present (the
// work dir already holds a repository) or tools (the image lacks git, flock or
// setsid, without which nothing would keep a checkout from running beside it).
// The clone runs in its own session (setsid), so a checkout can stop its whole
// process group; its output goes to $HOME/.zcp/warm/clone.log. It fetches
// through the inline helper a checkout uses (leaseCredentialHelperFor), so the
// credential comes from the session's env — GIT_TOKEN, answering tokenScope
// alone, or the pool's HQ credential for a repository on HQ, answering HQ
// alone — and is never composed in, and it persists nothing: the checkout
// that takes the clone sets the helper and the identity. repo and workDir are
// shell-quoted.
func leaseWarmCloneCommand(workDir, repo, hqURL, tokenScope string) string {
	q := shellQuote
	wd := q(workDir)
	state := ".git/" + leaseWarmStateFile
	clone := strings.Join([]string{
		"exec 9<" + wd,
		"flock -n 9 || exit 0",
		"cd " + wd + " || exit 0",
		"[ -e .git ] && exit 0",
		"export GIT_TERMINAL_PROMPT=0 GIT_HTTP_LOW_SPEED_LIMIT=1000 GIT_HTTP_LOW_SPEED_TIME=30",
		"git init -q || exit 1",
		`printf 'repo %s\npid %s\n' ` + q(leaseWarmRepoKey(repo)) + ` "$$" > ` + state + " || exit 1",
		"git remote add origin " + q(repo) + " && " +
			"git " + leaseInlineHelperArgs(leaseCredentialHelperFor(repo, hqURL, tokenScope)) + " fetch -q --prune origin && " +
			"c=$(git rev-parse -q --verify " + q("refs/remotes/origin/"+leaseDefaultBranch+"^{commit}") + ") && " +
			`printf 'commit %s\n' "$c" >> ` + state + " && " +
			`git checkout -q -f --detach "$c" && ` +
			// A second later the refresh rewrites the index, so no file is in
			// its second: git, comparing whole seconds, would read every such
			// file again at the checkout that takes the clone.
			"sleep 1 && { git update-index -q --refresh >/dev/null 2>&1 || :; } && echo done >> " + state,
	}, "\n")
	return strings.Join([]string{
		"if ! command -v git >/dev/null 2>&1 || ! command -v flock >/dev/null 2>&1 || ! command -v setsid >/dev/null 2>&1; then echo " + q(leaseWarmLaunchMarker+warmLaunchTools) + "; exit 0; fi",
		"if [ -e " + q(workDir+"/.git") + " ]; then echo " + q(leaseWarmLaunchMarker+warmLaunchPresent) + "; exit 0; fi",
		"[ -d " + wd + " ] || exit 1",
		`d="$HOME/.zcp/warm"`,
		`mkdir -p "$d" || exit 1`,
		"setsid sh -c " + q(clone) + ` </dev/null >>"$d/clone.log" 2>&1 &`,
		"echo " + q(leaseWarmLaunchMarker+warmLaunchStarted),
	}, "\n")
}

// leaseWarmCheckoutSteps are the two steps a checkout of repo runs in the work
// dir before anything else. lock holds the work dir's flock for the rest of
// the checkout — waiting for a running clone of repo up to leaseWarmCloneWait,
// stopping (its whole process group) one of another repository or one that
// outlasts the wait; settle takes a finished clone of repo (answering used) or
// removes any other — every file of the commit it checked out that lies
// inside the work dir (leaseWarmInsideNames), then its .git (answering
// discarded). A checkout that finds no clone changes nothing here.
// Where flock is missing no clone was ever started (leaseWarmCloneCommand).
func leaseWarmCheckoutSteps(repo string) (lock, settle string) {
	q := shellQuote
	state := ".git/" + leaseWarmStateFile
	mine := q("repo " + leaseWarmRepoKey(repo))
	wait := strconv.Itoa(int(leaseWarmCloneWait / time.Second))
	lock = "{ if command -v flock >/dev/null 2>&1; then exec 8<.; end=$(( $(date +%s) + " + wait + " )); " +
		"while ! flock -n 8; do " +
		"if { [ -f " + state + " ] && ! grep -qx " + mine + " " + state + "; } || [ \"$(date +%s)\" -ge \"$end\" ]; then " +
		"p=$(sed -n 's/^pid //p' " + state + " 2>/dev/null); if [ -n \"$p\" ]; then kill -s TERM -- \"-$p\" 2>/dev/null; fi; " +
		"flock -w 10 8 || { echo " + q(leaseWarmBusy) + " >&2; exit 4; }; break; fi; " +
		"sleep 0.2 2>/dev/null || sleep 1; done; fi; }"
	settle = "{ if [ ! -f " + state + " ]; then :; " +
		"elif grep -qx " + mine + " " + state + " && grep -qx done " + state + "; then " +
		"rm -f " + state + " && echo " + q(leaseWarmCheckoutMarker+leaseWarmUsed) + "; " +
		"else c=$(sed -n 's/^commit //p' " + state + "); " +
		"if [ -n \"$c\" ]; then git -c core.quotePath=false ls-tree -r --name-only \"$c\" > " + state + ".files || { echo " + q(leaseWarmUnreadable) + " >&2; exit 4; }; " +
		leaseWarmInsideNames(state+".files") + " | xargs -0 rm -f -- 2>/dev/null; fi; " +
		"rm -rf .git || exit 4; echo " + q(leaseWarmCheckoutMarker+leaseWarmDiscarded) + "; fi; }"
	return lock, settle
}

// leaseWarmInsideNames prints, NUL-terminated, the names in list — git
// ls-tree's, one per line, relative to the work dir the shell is in — that
// name a file inside it, so the discard deletes nothing else (review R4-3):
// a tree a hostile server serves can name `../x`, and a directory the image
// left as a symlink leads out of /var/www. A name with a `..`, `.` or empty
// component, or an absolute one, is skipped, and so is a name git quoted (one
// with a quote, a backslash or a control character in it: git clean or the
// reset takes such a file away instead); one whose directory, or any
// directory above it inside the work dir, is a symlink is skipped too. Each
// directory is checked once (ls-tree lists a directory's files together), in
// builtins only, so a tree of tens of thousands of files is read in about a
// second.
func leaseWarmInsideNames(list string) string {
	return "{ last=/; ok=; while IFS= read -r f; do " +
		"case /$f/ in */../*|*/./*|*//*) continue;; esac; " +
		`case $f in '"'*) continue;; esac; ` +
		"case $f in */*) d=${f%/*};; *) d=;; esac; " +
		"if [ \"$d\" != \"$last\" ]; then last=$d; ok=1; p=$d; " +
		"while [ -n \"$p\" ]; do if [ -L \"$p\" ]; then ok=; break; fi; case $p in */*) p=${p%/*};; *) p=;; esac; done; fi; " +
		"if [ -n \"$ok\" ]; then printf '%s\\0' \"$f\"; fi; " +
		"done < " + list + "; }"
}

// checkoutWarm is what a checkout's answer says it did with a clone: used,
// discarded, or "" when it found none.
func checkoutWarm(out string) string {
	for line := range strings.SplitSeq(out, "\n") {
		switch v, _ := strings.CutPrefix(strings.TrimSpace(line), leaseWarmCheckoutMarker); v {
		case leaseWarmUsed, leaseWarmDiscarded:
			return v
		}
	}
	return ""
}

// discardedWarmNote is what a lease says when its checkout removed a clone.
const discardedWarmNote = "the container held a clone zcp had started while it was warm, of another repository or unfinished; it was removed before the checkout"

// warmCloneRepoPath is the pool's record of the repository its last lease
// checked out (<stateDir>/warm/<pool>.repo.json), the clone URL as given.
func warmCloneRepoPath(stateDir, pool string) string {
	return filepath.Join(stateDir, leaseWarmDir, pool+".repo.json")
}

// warmCloneRepoRecord is that record: the URL as given, never a credential.
type warmCloneRepoRecord struct {
	Repo string `json:"repo"`
}

// storeWarmCloneRepo records repo — the clone URL as given (DP-18) — as the one
// the pool's warm containers get, atomically with mode 0600.
func storeWarmCloneRepo(stateDir, pool, repo string) error {
	if stateDir == "" || !leasePoolNameRe.MatchString(pool) {
		return errors.New("no state directory or an unusable pool name")
	}
	if err := writeWarmFile(stateDir, warmCloneRepoPath(stateDir, pool), warmCloneRepoRecord{Repo: repo}); err != nil {
		return fmt.Errorf("warm repository of %s: %w", pool, err)
	}
	return nil
}

// writeWarmFile writes v as JSON to path in the state dir's warm directory,
// atomically with mode 0600.
func writeWarmFile(stateDir, path string, v any) error {
	if err := os.MkdirAll(filepath.Join(stateDir, leaseWarmDir), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Join(stateDir, leaseWarmDir), err)
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return atomicWriteFile(path, data)
}

// readWarmCloneRepo is the repository the pool's warm containers get: the one
// its last lease checked out, else the pool's default (recorded only after an
// acquire's checkout worked); "" for a pool zcp never leased a repository from
// or one whose record is no clone URL a lease takes.
func readWarmCloneRepo(stateDir, pool string) string {
	repo := readPoolDefaultRepo(stateDir, pool)
	if stateDir != "" && leasePoolNameRe.MatchString(pool) {
		if raw, err := os.ReadFile(warmCloneRepoPath(stateDir, pool)); err == nil {
			var rec warmCloneRepoRecord
			if json.Unmarshal(raw, &rec) == nil && rec.Repo != "" {
				repo = rec.Repo
			}
		}
	}
	if repo == "" {
		return ""
	}
	cloneURL, err := validateLeaseRepo(repo)
	if err != nil {
		return ""
	}
	return cloneURL
}

func warmClonePath(stateDir, pool string) string {
	return filepath.Join(stateDir, leaseWarmDir, pool+".clone.json")
}

// recordClone keeps what zcp last did to clone into the pool's warm
// containers; a record that cannot be written only leaves status without it.
func (w *LeaseWarm) recordClone(d LeaseDeps, pool string, c LeaseWarmClone) {
	if d.StateDir == "" || !leasePoolNameRe.MatchString(pool) {
		return
	}
	c.At = d.now().UTC().Format(time.RFC3339)
	_ = writeWarmFile(d.StateDir, warmClonePath(d.StateDir, pool), c)
}

// readWarmClone is the pool's last clone outcome; nil when there is none.
func readWarmClone(stateDir, pool string) *LeaseWarmClone {
	if stateDir == "" || !leasePoolNameRe.MatchString(pool) {
		return nil
	}
	raw, err := os.ReadFile(warmClonePath(stateDir, pool))
	if err != nil {
		return nil
	}
	var c LeaseWarmClone
	if json.Unmarshal(raw, &c) != nil || c.Outcome == "" {
		return nil
	}
	return &c
}

// warmCloneCredentialGap says why the pool cannot fetch repo for a clone zcp
// would start now, "" when it can: a repository outside the Mate's HQ takes
// the pool's own GIT_TOKEN (the user's, DP-6); one on HQ takes only this
// Mate's HQ credential, which only an acquire writes there (DP-16, under
// PoolHQCredentialEnvKey), so a clone waits for zcp's record to say the pool
// holds the Git credential the Mate's server keeps from HQ now.
func warmCloneCredentialGap(d LeaseDeps, pool *platform.ServiceStack, repo string) string {
	if !IsHQRemote(repo, d.hqAddress()) {
		return ""
	}
	const onHQ = "the repository is on this Mate's HQ, whose git takes only this Mate's HQ credential, and "
	if d.PoolCredential.Read == nil {
		return onHQ + "zcp runs outside a Mate, so the pool holds no HQ credential it wrote"
	}
	credential, ok := d.PoolCredential.Read()
	if !ok || credential == "" {
		return onHQ + "the Mate's server holds no Git credential from HQ: it is not linked to HQ yet (its /mate/setup.json says why)"
	}
	rec, found := readPoolCredential(d.StateDir, pool.Name)
	if !found || rec.PoolServiceID != pool.ID || rec.Fingerprint != hq.GitCredentialFingerprint(credential) {
		return onHQ + "zcp's record does not say the pool holds the one the Mate's server keeps from HQ now (an acquire of an HQ repository writes it)"
	}
	return ""
}

// cloneLater starts the clone in each of ids in the background (cloneInto).
func (w *LeaseWarm) cloneLater(ctx context.Context, d LeaseDeps, rp *resolvedPool, ids []string) {
	if d.SSH == nil || len(ids) == 0 {
		return
	}
	w.spawn(func() {
		bctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Duration(len(ids))*(leaseReadTimeout+leaseWarmLaunchTries*(leaseWarmCloneLaunchTimeout+leaseWarmLaunchRetry)))
		defer cancel()
		w.cloneInto(bctx, d, rp, ids)
	})
}

// cloneInto starts, in each container of ids still free, the clone of the
// repository the pool's last lease checked out, and records what it did. It
// does nothing for a pool zcp never leased a repository from, and records a
// skip when the pool's credential cannot fetch it (warmCloneCredentialGap).
func (w *LeaseWarm) cloneInto(ctx context.Context, d LeaseDeps, rp *resolvedPool, ids []string) {
	if d.SSH == nil || len(ids) == 0 {
		return
	}
	pool := rp.svc.Name
	repo := readWarmCloneRepo(d.StateDir, pool)
	if repo == "" {
		return
	}
	if gap := warmCloneCredentialGap(d, rp.svc, repo); gap != "" {
		w.recordClone(d, pool, LeaseWarmClone{Repo: topology.CanonicalRepoURL(repo), Outcome: LeaseWarmCloneSkipped, Note: gap + "; nothing was cloned"})
		return
	}
	for _, id := range ids {
		w.recordClone(d, pool, w.launchClone(ctx, d, rp, id, repo))
	}
}

// launchClone reads the container by id and, while it is free, ACTIVE and
// reachable, starts the clone of repo in it over ssh to its hostname.
func (w *LeaseWarm) launchClone(ctx context.Context, d LeaseDeps, rp *resolvedPool, id, repo string) LeaseWarmClone {
	out := LeaseWarmClone{Repo: topology.CanonicalRepoURL(repo)}
	c, err := d.containerByID(ctx, rp, id)
	if err != nil {
		out.Outcome, out.Note = LeaseWarmCloneSkipped, "the container could not be read, so nothing was written into it: "+errorNote(err)
		return out
	}
	out.Container = c.Hostname
	switch {
	case c.LockState != "" || c.Status != platform.SwarmContainerActive || c.UnreachableSince != "":
		out.Outcome, out.Note = LeaseWarmCloneSkipped, "the container was no longer free (an acquire took it, or it stopped or is being removed), so nothing was written into it"
		return out
	case platform.ValidateContainerHostname(c.Hostname) != nil:
		out.Outcome, out.Note = LeaseWarmCloneSkipped, "the container's hostname is not a pool container's, so nothing was written into it"
		return out
	}
	// The pool's GIT_TOKEN answers the scope its default repository or an
	// earlier lease bound it to; a clone never binds it (repo "").
	tokenScope, _ := leaseTokenScope(d.StateDir, rp.svc.Name, d.hqAddress(), "")
	answer, err := w.runLaunch(ctx, d, c.Hostname, leaseWarmCloneCommand(leaseWorkDir, repo, d.hqAddress(), tokenScope))
	if err != nil {
		out.Outcome, out.Note = LeaseWarmCloneFailed, "the clone could not be started over ssh: "+sshFailureText(err)
		return out
	}
	switch warmLaunchAnswer(string(answer)) {
	case warmLaunchStarted:
		out.Outcome = LeaseWarmCloneStarted
	case warmLaunchPresent:
		out.Outcome, out.Note = LeaseWarmCloneSkipped, "the container's /var/www already holds a repository (a clone zcp started there before, or a checkout)"
	case warmLaunchTools:
		out.Outcome, out.Note = LeaseWarmCloneSkipped, "the pool's image lacks git, flock or setsid, which the clone needs"
	default:
		out.Outcome, out.Note = LeaseWarmCloneFailed, fmt.Sprintf("the clone's launch answered %q", boundedText(string(answer)))
	}
	return out
}

// runLaunch runs the launch over ssh to host, again while ssh could not run
// it (leaseWarmLaunchTries).
func (w *LeaseWarm) runLaunch(ctx context.Context, d LeaseDeps, host, command string) ([]byte, error) {
	pause := w.LaunchRetry
	if pause == 0 {
		pause = leaseWarmLaunchRetry
	}
	for try := 1; ; try++ {
		lctx, cancel := context.WithTimeout(ctx, leaseWarmCloneLaunchTimeout)
		answer, err := d.SSH.ExecSSH(lctx, host, command)
		cancel()
		sshDidNotRun := exitStatus(err) == leaseSSHUnreachableExit || (errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil)
		if err == nil || !sshDidNotRun || try == leaseWarmLaunchTries {
			return answer, err
		}
		select {
		case <-ctx.Done():
			return answer, err
		case <-time.After(pause):
		}
	}
}

// warmLaunchAnswer is what a launch answered on its marker line, "" when none.
func warmLaunchAnswer(out string) string {
	for line := range strings.SplitSeq(out, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), leaseWarmLaunchMarker); ok {
			return v
		}
	}
	return ""
}
