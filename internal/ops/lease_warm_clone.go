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

	"github.com/zeropsio/zcp/internal/ops/git"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// A pool's warm container takes an acquire at once, but the checkout still
// cloned the whole repository into its empty /var/www: on z3's 86 MB / 24k-file
// tree that was most of a 10.8 s acquire (live, 2026-10-24). So right after zcp
// creates a pool's warm container, keeps one at a release or finds one at an
// acquire, it starts — detached in the container, over ssh to its hostname — a
// clone of the pool's recorded repository, at origin/main,
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
// and only for a pool it leased a repository from: the clone authenticates
// exactly as a checkout does and writes no credential, helper or identity. A
// repository outside the Mate's HQ is fetched with the pool's GIT_TOKEN,
// answered only to the host it is bound to (leaseTokenScope), and the clone
// runs detached. A repository on HQ is fetched blobless with no helper of its
// own (leaseFetchStep), as its checkout is: zcp answers HQ's git only over a
// session it holds (LeaseDeps.HQGit, DP-16), so the clone runs as a relayed
// script that a zcp process of its own (LeaseDeps.HoldWarmClone, `zcp lease
// warm-clone`) holds until the clone ends, and the pool stores no HQ
// credential. Not the process that warmed the container: a `zcp lease` call
// waits at most 20 s after its answer and an MCP server ends with its agent,
// and the clone dies with the session. Without it every acquire of an HQ
// repository cloned the whole tree through the relay: 70 s of a 92 s acquire
// (live, 2026-11-23).

// leaseWarmStateFile is the clone's state, inside the clone's .git so it lives
// and goes with the clone.
const leaseWarmStateFile = "zcp-warm"

// leaseWarmCloneWait is how long a checkout waits for a running clone of its
// own repository before it stops it: a clone that started before the checkout
// finishes no later than a fresh one would, and a stalled transfer ends itself
// (GIT_HTTP_LOW_SPEED_*).
const leaseWarmCloneWait = 3 * time.Minute

// leaseWarmCloneLaunchTimeout bounds one try of a detached launch: the ssh
// call returns as soon as the clone is detached, well under a second. A held
// launch (a repository on HQ) runs the whole clone, which RelayedSSH bounds
// (relayedScriptCeiling).
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

// What a launch answers: a detached one started; a held one running once the
// clone is in the background, then exit and the clone's exit status once it
// ended (heldCloneOutcome reads it); either present or tools.
const (
	warmLaunchStarted = "started"
	warmLaunchRunning = "running"
	warmLaunchExit    = "exit"
	warmLaunchPresent = "present"
	warmLaunchTools   = "tools"
)

// leaseWarmCloneNothingExit is the clone's exit status when it left the work
// dir alone: a checkout holds it, or it already holds a repository.
const leaseWarmCloneNothingExit = "3"

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
	// LeaseWarmCloneStarted: a detached clone runs in the container.
	LeaseWarmCloneStarted = "started"
	// LeaseWarmCloneRunning: a clone of a repository on HQ runs in a relayed
	// session its own zcp process holds; status reads one past its bound
	// (warmCloneBound) as failed (warmCloneView).
	LeaseWarmCloneRunning = "running"
	// LeaseWarmCloneDone: a clone zcp held the session of finished.
	LeaseWarmCloneDone = "done"
	// LeaseWarmCloneSkipped: nothing was written; Note says why.
	LeaseWarmCloneSkipped = "skipped"
	// LeaseWarmCloneFailed: the launch failed; Note says why.
	LeaseWarmCloneFailed = "failed"
)

// LeaseWarmClone is what zcp last did to clone a pool's repository into a warm
// container, kept per pool and reported by status. Started means a detached
// clone runs in the container; whether it finished only the container knows,
// and the next checkout acts on it either way.
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

// leaseWarmCloneCommand starts the clone of repo at origin/main into workDir
// and answers on a leaseWarmLaunchMarker line; present (the work dir already
// holds a repository) and tools (the image lacks git, flock or setsid, without
// which nothing would keep a checkout from running beside it) start nothing.
// The clone runs in its own session (setsid), so a checkout can stop its whole
// process group; its output goes to $HOME/.zcp/warm/clone.log. It fetches as
// a checkout does (leaseFetchStep) and persists nothing: the checkout that
// takes the clone sets the helper and the identity.
//   - A repository outside the Mate's HQ (hqURL) is fetched through the inline
//     helper, so the credential comes from the session's env — GIT_TOKEN,
//     answering tokenScope alone — and is never composed in; the clone runs
//     detached and the launch answers started at once.
//   - A held launch (a repository on HQ, fetched blobless with no helper of
//     its own) runs as a relayed session (LeaseDeps.HQGit), whose helper
//     answers HQ only while the session lasts: it answers running once the
//     clone is in the background, then waits for it and answers exit and the
//     clone's exit status (heldCloneOutcome).
//
// The clone holds the work dir's flock on leaseCheckoutLockFD, a descriptor a
// relayed session leaves free. repo and workDir are shell-quoted.
func leaseWarmCloneCommand(workDir, repo, hqURL, tokenScope string, held bool) string {
	q := shellQuote
	wd := q(workDir)
	state := ".git/" + leaseWarmStateFile
	fd := leaseCheckoutLockFD
	clone := strings.Join([]string{
		"exec " + fd + "<" + wd,
		"flock -n " + fd + " || exit " + leaseWarmCloneNothingExit,
		"cd " + wd + " || exit 1",
		"[ -e .git ] && exit " + leaseWarmCloneNothingExit,
		"export GIT_TERMINAL_PROMPT=0 GIT_HTTP_LOW_SPEED_LIMIT=1000 GIT_HTTP_LOW_SPEED_TIME=30",
		"git init -q || exit 1",
		`printf 'repo %s\npid %s\n' ` + q(leaseWarmRepoKey(repo)) + ` "$$" > ` + state + " || exit 1",
		"git remote add origin " + q(repo) + " && " +
			leaseFetchStep(repo, hqURL, tokenScope) + " && " +
			"c=$(git rev-parse -q --verify " + q("refs/remotes/origin/"+leaseDefaultBranch+"^{commit}") + ") && " +
			`printf 'commit %s\n' "$c" >> ` + state + " && " +
			`git checkout -q -f --detach "$c" && ` +
			// A second later the refresh rewrites the index, so no file is in
			// its second: git, comparing whole seconds, would read every such
			// file again at the checkout that takes the clone.
			"sleep 1 && { git update-index -q --refresh >/dev/null 2>&1 || :; } && echo done >> " + state,
	}, "\n")
	run := "setsid sh -c " + q(clone) + ` </dev/null >>"$d/clone.log" 2>&1 &`
	answer := "echo " + q(leaseWarmLaunchMarker+warmLaunchStarted)
	if held {
		// wait $! is the clone's own exit status because setsid runs it in
		// place: setsid forks only when its caller leads a process group, and
		// a non-interactive sh runs a & job in its own group.
		answer = "echo " + q(leaseWarmLaunchMarker+warmLaunchRunning) + "\n" +
			"wait $!; echo " + q(leaseWarmLaunchMarker+warmLaunchExit) + ` "$?"`
	}
	return strings.Join([]string{
		"if ! command -v git >/dev/null 2>&1 || ! command -v flock >/dev/null 2>&1 || ! command -v setsid >/dev/null 2>&1; then echo " + q(leaseWarmLaunchMarker+warmLaunchTools) + "; exit 0; fi",
		"if [ -e " + q(workDir+"/.git") + " ]; then echo " + q(leaseWarmLaunchMarker+warmLaunchPresent) + "; exit 0; fi",
		"[ -d " + wd + " ] || exit 1",
		`d="$HOME/.zcp/warm"`,
		`mkdir -p "$d" || exit 1`,
		run,
		answer,
	}, "\n")
}

// leaseCheckoutLockFD is the descriptor a checkout holds the work dir's flock
// on: one a relayed session leaves free.
const leaseCheckoutLockFD = "7"

// leaseWarmCheckoutSteps are the two steps a checkout of repo runs in the work
// dir before anything else. lock holds the work dir's flock for the rest of
// the checkout — waiting for a running clone of repo up to leaseWarmCloneWait,
// stopping (its whole process group) one of another repository or one that
// outlasts the wait; settle takes a finished clone of repo (answering used) or
// removes any other — every file of the commit it checked out that lies
// inside the work dir (leaseWarmInsideNames), then its .git (answering
// discarded). A checkout that finds no clone changes nothing here.
// Where flock is missing no clone was ever started (leaseWarmCloneCommand).
// The lock is held on descriptor leaseCheckoutLockFD: the checkout of a
// repository on HQ runs as a relayed session, whose helper answers only
// while 3, 4, 8 and 9 are the session's own (relayPrologue).
func leaseWarmCheckoutSteps(repo string) (lock, settle string) {
	q := shellQuote
	state := ".git/" + leaseWarmStateFile
	mine := q("repo " + leaseWarmRepoKey(repo))
	wait := strconv.Itoa(int(leaseWarmCloneWait / time.Second))
	lock = "{ if command -v flock >/dev/null 2>&1; then exec " + leaseCheckoutLockFD + "<.; end=$(( $(date +%s) + " + wait + " )); " +
		"while ! flock -n " + leaseCheckoutLockFD + "; do " +
		"if { [ -f " + state + " ] && ! grep -qx " + mine + " " + state + "; } || [ \"$(date +%s)\" -ge \"$end\" ]; then " +
		"p=$(sed -n 's/^pid //p' " + state + " 2>/dev/null); if [ -n \"$p\" ]; then kill -s TERM -- \"-$p\" 2>/dev/null; fi; " +
		"flock -w 10 " + leaseCheckoutLockFD + " || { echo " + q(leaseWarmBusy) + " >&2; exit 4; }; break; fi; " +
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

// warmCloneRepo is the repository the pool's warm containers get: the pool's
// recorded one (LeaseDeps.PoolRepository); "" for a pool that records none or
// one whose record is no clone URL a lease takes.
func warmCloneRepo(d LeaseDeps, pool string) string {
	repo := d.poolRepository(pool).Repo
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

// warmCloneView is the pool's last clone outcome as status reports it: a held
// clone still running past its bound (warmCloneBound) never reported back —
// the zcp process holding its session ended first, as at a restart — and is
// said to have failed, never to run.
func (d LeaseDeps) warmCloneView(pool string) *LeaseWarmClone {
	c := readWarmClone(d.StateDir, pool)
	if c == nil || c.Outcome != LeaseWarmCloneRunning {
		return c
	}
	if at, err := time.Parse(time.RFC3339, c.At); err == nil && d.now().Sub(at) <= warmCloneBound(true) {
		return c
	}
	c.Outcome = LeaseWarmCloneFailed
	c.Note = "the clone zcp started at " + c.At + " never reported back (the zcp process holding its relayed session ended before the clone did); the next checkout removes what it left and clones"
	return c
}

// warmCloneHQSkip is the note of a clone zcp never starts: repo is on the
// Mate's HQ, whose git zcp answers only over a relayed session, and this zcp
// runs none (LeaseDeps.HoldWarmClone nil).
const warmCloneHQSkip = "the repository is on the Mate's HQ, whose git zcp answers only over a relayed session, and this zcp runs none (it runs outside a Mate)"

// warmCloneHeldNote is what status says while a held clone runs.
const warmCloneHeldNote = "the repository is on the Mate's HQ, so the clone runs in a relayed session a zcp process of its own holds until the clone ends; an acquire that comes meanwhile waits for it"

// warmCloneBound bounds the clone into one container: its read by id and its
// launch's tries, and for a held launch (a repository on HQ) the whole clone,
// which RelayedSSH ends at relayedScriptCeiling.
func warmCloneBound(held bool) time.Duration {
	bound := leaseReadTimeout + leaseWarmLaunchTries*(leaseWarmCloneLaunchTimeout+leaseWarmLaunchRetry)
	if held {
		bound += relayedScriptCeiling
	}
	return bound
}

// cloneLater starts the clone in each of ids in the background (cloneInto).
func (w *LeaseWarm) cloneLater(ctx context.Context, d LeaseDeps, rp *resolvedPool, ids []string) {
	if d.SSH == nil || len(ids) == 0 {
		return
	}
	w.spawn(func() { w.cloneInto(context.WithoutCancel(ctx), d, rp, ids) })
}

// cloneInto starts, in each container of ids still free, the clone of the
// pool's recorded repository, each within warmCloneBound, and records what it
// did. It does nothing for a pool that records none. A repository on the
// Mate's HQ it hands, container by container, to a zcp process of its own
// (LeaseDeps.HoldWarmClone, which runs LeaseWarmCloneHeld), and records a
// skip when none can answer its git (warmCloneHQSkip).
func (w *LeaseWarm) cloneInto(ctx context.Context, d LeaseDeps, rp *resolvedPool, ids []string) {
	if d.SSH == nil || len(ids) == 0 {
		return
	}
	pool := rp.svc.Name
	repo := warmCloneRepo(d, pool)
	if repo == "" {
		return
	}
	hqURL := d.hqAddress()
	if IsHQRemote(repo, hqURL) {
		if d.HoldWarmClone == nil || d.HQGit == nil {
			w.recordClone(d, pool, LeaseWarmClone{Repo: topology.CanonicalRepoURL(repo), Outcome: LeaseWarmCloneSkipped, Note: warmCloneHQSkip + "; nothing was cloned"})
			return
		}
		for _, id := range ids {
			if err := d.HoldWarmClone(pool, id); err != nil {
				w.recordClone(d, pool, LeaseWarmClone{Repo: topology.CanonicalRepoURL(repo), Outcome: LeaseWarmCloneFailed,
					Note: "the zcp process that holds the clone's relayed session could not be started, so nothing was cloned: " + err.Error()})
			}
		}
		return
	}
	for _, id := range ids {
		cctx, cancel := context.WithTimeout(ctx, warmCloneBound(false))
		w.recordClone(d, pool, w.launchClone(cctx, d, rp, id, repo, hqURL, false))
		cancel()
	}
}

// LeaseWarmCloneHeld is `zcp lease warm-clone --pool <pool> --container <id>`,
// the zcp process cloneInto starts for each free warm container of a pool
// whose repository is on the Mate's HQ: it reads the container by id and,
// while it is free, runs the clone as a relayed session (LeaseDeps.HQGit) it
// holds until the clone ends, within warmCloneBound, and records what the
// clone did (heldCloneOutcome). Status says running meanwhile.
func LeaseWarmCloneHeld(ctx context.Context, d LeaseDeps, pool, containerID string) error {
	repo := warmCloneRepo(d, pool)
	hqURL := d.hqAddress()
	switch {
	case repo == "":
		return fmt.Errorf("warm clone: pool %q records no repository", pool)
	case !IsHQRemote(repo, hqURL):
		return fmt.Errorf("warm clone: the repository of pool %q is not on the Mate's HQ", pool)
	case d.HQGit == nil || d.SSH == nil:
		return errors.New("warm clone: this zcp runs no relayed session (it runs outside a Mate)")
	}
	w := d.Warm
	if w == nil {
		w = &LeaseWarm{}
	}
	ctx, cancel := context.WithTimeout(ctx, warmCloneBound(true))
	defer cancel()
	rp, err := resolvePool(ctx, d, pool)
	if err != nil {
		w.recordClone(d, pool, LeaseWarmClone{Repo: topology.CanonicalRepoURL(repo), Outcome: LeaseWarmCloneSkipped,
			Note: "the pool could not be read, so nothing was written into its warm container: " + errorNote(err)})
		return fmt.Errorf("warm clone: %w", err)
	}
	w.recordClone(d, pool, w.launchClone(ctx, d, rp, containerID, repo, hqURL, true))
	return nil
}

// launchClone reads the container by id and, while it is free, ACTIVE and
// reachable, starts the clone of repo in it over ssh to its hostname — held,
// for a repository on HQ, as a relayed session (LeaseDeps.gitSSH) that ends
// with the clone, status saying it runs meanwhile.
func (w *LeaseWarm) launchClone(ctx context.Context, d LeaseDeps, rp *resolvedPool, id, repo, hqURL string, held bool) LeaseWarmClone {
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
	// The pool's GIT_TOKEN answers the scope its recorded repository or an
	// earlier lease bound it to; a clone never binds it (repo "").
	tokenScope, _ := leaseTokenScope(d, rp.svc.Name, "")
	if held {
		running := out
		running.Outcome, running.Note = LeaseWarmCloneRunning, warmCloneHeldNote
		w.recordClone(d, rp.svc.Name, running)
	}
	answer, err := w.runLaunch(ctx, d.gitSSH(held), c.Hostname, leaseWarmCloneCommand(LeaseWorkDir, repo, hqURL, tokenScope, held), held)
	if held {
		out.Outcome, out.Note = heldCloneOutcome(string(answer), err)
		return out
	}
	if err != nil {
		out.Outcome, out.Note = LeaseWarmCloneFailed, "the clone could not be started over ssh: "+sshFailureText(err)
		return out
	}
	switch v := warmLaunchAnswer(string(answer)); v {
	case warmLaunchStarted:
		out.Outcome = LeaseWarmCloneStarted
	case warmLaunchPresent:
		out.Outcome, out.Note = LeaseWarmCloneSkipped, warmClonePresentNote
	case warmLaunchTools:
		out.Outcome, out.Note = LeaseWarmCloneSkipped, warmCloneToolsNote
	default:
		out.Outcome, out.Note = LeaseWarmCloneFailed, fmt.Sprintf("the clone's launch answered %q", boundedText(string(answer)))
	}
	return out
}

// The notes of a launch that started nothing.
const (
	warmClonePresentNote = "the container's /var/www already holds a repository (a clone zcp started there before, or a checkout)"
	warmCloneToolsNote   = "the pool's image lacks git, flock or setsid, which the clone needs"
)

// heldCloneOutcome is what a held launch's answer and error say the clone
// did: exit 0 is done; exit leaseWarmCloneNothingExit is the clone leaving
// the work dir alone (a checkout holds it, or it holds a repository); any
// other exit status is failed, naming it; running with no exit is a session
// that ended while the clone ran, which loses the clone its credential.
func heldCloneOutcome(answer string, err error) (outcome, note string) {
	v := warmLaunchAnswer(answer)
	code, exited := strings.CutPrefix(v, warmLaunchExit+" ")
	switch {
	case exited && code == "0":
		return LeaseWarmCloneDone, ""
	case exited && code == leaseWarmCloneNothingExit:
		return LeaseWarmCloneSkipped, warmClonePresentNote
	case exited:
		return LeaseWarmCloneFailed, "the clone ended with exit status " + code +
			" before it finished (the container's $HOME/.zcp/warm/clone.log says why); the next checkout removes it and clones"
	case v == warmLaunchPresent:
		return LeaseWarmCloneSkipped, warmClonePresentNote
	case v == warmLaunchTools:
		return LeaseWarmCloneSkipped, warmCloneToolsNote
	case v == warmLaunchRunning:
		why := "it answered no exit status"
		if err != nil {
			why = sshFailureText(err)
		}
		return LeaseWarmCloneFailed, "the clone's relayed session ended while the clone ran (" + why + "), so its git lost its credential; the next checkout removes it and clones"
	case err != nil:
		return LeaseWarmCloneFailed, "the clone's relayed session ended before it answered: " + sshFailureText(err)
	}
	return LeaseWarmCloneFailed, fmt.Sprintf("the clone's launch answered %q", boundedText(answer))
}

// runLaunch runs the launch through runner to host, again while ssh could not
// run it (leaseWarmLaunchTries). A detached launch's try is bounded by
// leaseWarmCloneLaunchTimeout, and one that ran out of time is ssh's not
// getting in; a held one runs the whole clone, so only ssh's own failure
// before the clone started (no running answer) is tried again: a try after
// it would find the clone's .git and say present, hiding a clone that lost
// its session.
func (w *LeaseWarm) runLaunch(ctx context.Context, runner git.SSHExecutor, host, command string, held bool) ([]byte, error) {
	pause := w.LaunchRetry
	if pause == 0 {
		pause = leaseWarmLaunchRetry
	}
	for try := 1; ; try++ {
		lctx, cancel := ctx, context.CancelFunc(func() {})
		if !held {
			lctx, cancel = context.WithTimeout(ctx, leaseWarmCloneLaunchTimeout)
		}
		answer, err := runner.ExecSSH(lctx, host, command)
		cancel()
		sshDidNotRun := (exitStatus(err) == leaseSSHUnreachableExit && (!held || warmLaunchAnswer(string(answer)) == "")) ||
			(!held && errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil)
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

// warmLaunchAnswer is what a launch answered on its last marker line, "" when
// none.
func warmLaunchAnswer(out string) string {
	last := ""
	for line := range strings.SplitSeq(out, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), leaseWarmLaunchMarker); ok {
			last = v
		}
	}
	return last
}
