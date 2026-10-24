package ops

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/platform"
)

// A branch that spans several app parts holds one lease per part's pool
// (DP-17), and the agent commits in one of them. A sync moves those commits to
// the branch's other leases through origin (docs/spec-workflows.md §8 DP-24):
// the agent commits over `ssh <alias>` — zcp stages and commits nothing
// (GF-2) — then one sync pushes the lease's branch and fast-forwards every
// other lease of its group. Through origin, never lease to lease: origin is
// where review reads the branch, and the release guard counts unpushed work
// against it (DP-4). Nothing is ever forced: the push is a plain push of the
// branch to the same branch, never main, and a lease that cannot be
// fast-forwarded — commits of its own, local changes the fast-forward would
// overwrite, a checkout on another branch, a container ssh cannot reach — is
// left as it was and named. Every read-only git call runs with
// GIT_OPTIONAL_LOCKS=0 (leaseGitRead), so a read writes nothing into the
// checkout; only the push, the fetch, the fast-forward and the upstream it
// sets write. A fast-forward that changes .zcp/dev-servers.json is zcp's own
// change to the lease's checkout, as an acquire's checkout is (DP-13), so the
// lease's supervisor takes it in the same call: the sibling overlay first
// (DP-22), then a reload. The code a server runs is another matter: a
// declared server reloads by itself (the supervisor's watch, or its command's
// own watcher), while a runtime entry keeps the code it started with, and the
// sync names it with its restart, which stays the agent's to make.

// Push outcomes.
const (
	// LeaseSyncPushed: origin's branch moved forward to the source's commit.
	LeaseSyncPushed = "pushed"
	// LeaseSyncNewBranch: origin had no such branch; the push created it.
	LeaseSyncNewBranch = "new-branch"
)

// Member outcomes. LeaseSyncUpToDate is also a push's: origin already had the
// source's commit.
const (
	LeaseSyncUpToDate      = "up-to-date"
	LeaseSyncFastForwarded = "fast-forwarded"
	// LeaseSyncHasOwnCommits: the member's branch has commits origin's lacks;
	// zcp never rebases or merges them, so it is left as it was.
	LeaseSyncHasOwnCommits = "has-own-commits"
	// LeaseSyncLocalChangesInTheWay: git refused the fast-forward, which
	// would overwrite the member's uncommitted or untracked files.
	LeaseSyncLocalChangesInTheWay = "local-changes-in-the-way"
	// LeaseSyncOnAnotherBranch: the member's checkout is not on the branch.
	LeaseSyncOnAnotherBranch = "on-another-branch"
	// LeaseSyncUnreachable: ssh could not reach the member.
	LeaseSyncUnreachable = "unreachable"
	// LeaseSyncParked: the member is parked (DP-28) — its container stopped —
	// so zcp did not try it.
	LeaseSyncParked = "parked"
	// LeaseSyncLost: the member's container is no longer its lease's — gone,
	// free or another holder's (DP-2) — so zcp sent it nothing.
	LeaseSyncLost = "lost"
	// LeaseSyncUnverified: zcp cannot verify the member's container is still
	// its lease's, or could not ask, so it sent it nothing.
	LeaseSyncUnverified = "unverified"
	// LeaseSyncFailed: the member's fetch or fast-forward failed otherwise.
	LeaseSyncFailed = "failed"
)

// LeaseSyncView is what a sync did: the branch, the lease it pushed from, the
// commit origin's branch is now at, the push and every other lease of the
// branch, with how long each step took. It never carries a lease string.
type LeaseSyncView struct {
	Branch string `json:"branch"`
	Source string `json:"source"`
	// Commit is the source's commit, which origin's branch is at after the
	// push.
	Commit  string            `json:"commit"`
	Push    LeaseSyncPush     `json:"push"`
	Members []LeaseSyncMember `json:"members"`
	// CheckMillis is how long reading the source took before the push.
	CheckMillis int64 `json:"checkMs"`
	// Millis is the whole sync.
	Millis int64  `json:"ms"`
	Note   string `json:"note,omitempty"`
}

// LeaseSyncPush is the source's push.
type LeaseSyncPush struct {
	Outcome string `json:"outcome"`
	// SetUpstream: the source's branch tracked nothing on origin, so the push
	// set it to (git push -u).
	SetUpstream bool  `json:"setUpstream,omitempty"`
	Millis      int64 `json:"ms"`
}

// LeaseSyncMember is one other lease of the branch: what its step did, the
// commit it was on (From) and, when it moved or already was there, the one it
// is on now (To).
type LeaseSyncMember struct {
	Alias   string `json:"alias"`
	Pool    string `json:"pool"`
	Outcome string `json:"outcome"`
	From    string `json:"from,omitempty"`
	To      string `json:"to,omitempty"`
	// OwnCommits counts the commits a has-own-commits member has that
	// origin's branch lacks.
	OwnCommits int `json:"ownCommits,omitempty"`
	// UnwatchedServers are the dev servers running on a fast-forwarded member
	// that nothing zcp knows of reloads — runtime entries; a declared server
	// reloads by itself — so they keep serving the code they started with
	// until restarted.
	UnwatchedServers []string `json:"unwatchedServers,omitempty"`
	// Reloaded: the fast-forward changed the member's .zcp/dev-servers.json
	// and its supervisor took it — the sibling overlay sent when it changed,
	// then a reload.
	Reloaded bool `json:"reloaded,omitempty"`
	// Code is the error code of a failed member's step.
	Code   string `json:"code,omitempty"`
	Note   string `json:"note,omitempty"`
	Millis int64  `json:"ms"`
}

// leaseSyncMarker opens each line a sync's remote script answers with.
const leaseSyncMarker = "@@zcp-sync "

// LeaseSyncCall is the zerops_lease call that syncs from alias.
func LeaseSyncCall(alias string) string { return "zerops_lease action=sync lease=" + alias }

// LeaseSync pushes the branch of the lease addressed by alias to origin and
// fast-forwards every other lease of its group (DP-17) to it. It refuses,
// changing nothing anywhere, a lease of main, a zcp without ssh into its
// leases, a source whose container is no longer its lease's or that zcp
// cannot verify or ask about (readLeaseOwnership, DP-2), a source checkout
// holding uncommitted or untracked files (named, read
// with the release guard's own read) or on another branch, and a push origin
// refuses (moved on, or its rules) or that cannot reach the source. Once the
// push went, every other lease's outcome is in the view, the ones zcp could not
// fast-forward named with why — a member no longer its lease's is lost, one
// zcp cannot verify unverified, neither reached over ssh; none fails the sync.
func LeaseSync(ctx context.Context, d LeaseDeps, alias string) (LeaseSyncView, error) {
	start := time.Now()
	rec, err := loadLeaseRecord(d.StateDir, alias)
	if err != nil {
		return LeaseSyncView{}, err
	}
	branch := recordBranch(rec)
	if branch == leaseDefaultBranch {
		return LeaseSyncView{}, platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("Lease %s is on %s, and a sync never pushes %s: it changes only through review", alias, leaseDefaultBranch, leaseDefaultBranch),
			fmt.Sprintf("Work on a branch of your own: zerops_lease action=acquire pool=%s branch=<branch> (add pools for every part it edits), commit there, then sync it", rec.PoolHostname))
	}
	if d.SSH == nil {
		return LeaseSyncView{}, platform.NewPlatformError(platform.ErrPrerequisiteMissing,
			fmt.Sprintf("Lease %s cannot be synced: this zcp has no ssh into its leases", alias),
			"Run zcp in the project's container, where it reaches each lease over its ssh alias")
	}
	if rec.Parked {
		return LeaseSyncView{}, LeaseParkedError(rec.targetInfo(), "a sync")
	}
	// A sync pushes from the source and fast-forwards its siblings over ssh:
	// never into a container that is no longer its lease's (DP-2).
	verdict := readLeaseOwnership(ctx, d, rec)
	switch {
	case verdict.own.lost():
		return LeaseSyncView{}, lostLeaseRefusal(alias, "sync", verdict.why)
	case verdict.own == leaseUnverified:
		return LeaseSyncView{}, unverifiedLeaseRefusal(alias, "synced", verdict)
	}
	group, err := LeaseGroup(d.StateDir, alias)
	if err != nil {
		return LeaseSyncView{}, err
	}
	view := LeaseSyncView{Branch: branch, Source: alias, Members: []LeaseSyncMember{}}
	// Every step reaches a container through its alias: put back a stanza
	// another call or a redeploy lost before any of them runs.
	view.Note = d.Host.refreshAliases(ctx, d)
	if err := checkSyncSource(ctx, d.SSH, alias); err != nil {
		return LeaseSyncView{}, err
	}
	view.CheckMillis = time.Since(start).Milliseconds()
	pushStart := time.Now()
	onHQ := IsHQRemote(rec.Repo, d.hqAddress())
	view.Push, view.Commit, err = pushLeaseBranch(ctx, d.SSH, alias, branch, onHQ)
	if err != nil {
		return LeaseSyncView{}, err
	}
	view.Push.Millis = time.Since(pushStart).Milliseconds()
	var members []*LeaseSyncMember
	parked := map[string]bool{}
	for _, m := range group {
		if m.Alias != alias {
			members = append(members, &LeaseSyncMember{Alias: m.Alias, Pool: m.Pool})
			parked[m.Alias] = m.Parked
		}
	}
	runMembers(members, func(m *LeaseSyncMember) {
		if !memberStillHeld(ctx, d, m, alias) {
			return
		}
		if parked[m.Alias] {
			parkedMember(m, alias)
			return
		}
		fastForwardMember(ctx, d, m, branch, alias, onHQ)
	})
	for _, m := range members {
		view.Members = append(view.Members, *m)
	}
	view.Millis = time.Since(start).Milliseconds()
	return view, nil
}

// checkSyncSource refuses a source whose checkout holds uncommitted or
// untracked files, read with the release guard's own read (readUnsavedWork)
// and named as it names them: zcp commits nothing (GF-2). Its commits on no
// remote branch are what the sync pushes.
func checkSyncSource(ctx context.Context, ssh SSHDeployer, alias string) error {
	w, err := readUnsavedWork(ctx, ssh, alias)
	if err != nil {
		if exitStatus(err) == leaseSSHUnreachableExit {
			code, suggestion := classifyCheckoutFailure(err, false)
			pe := platform.NewPlatformError(code, fmt.Sprintf("Lease %s did not answer ssh, so nothing was pushed", alias), suggestion)
			pe.Cause = err
			return pe
		}
		pe := platform.NewPlatformError(platform.ErrPrerequisiteMissing,
			fmt.Sprintf("Lease %s could not be checked for uncommitted files, so nothing was pushed", alias),
			"Look at the container's git state (git -C /var/www status) over ssh, then sync again")
		pe.Cause = err
		return pe
	}
	if len(w.files) == 0 {
		return nil
	}
	return platform.NewPlatformError(platform.ErrPrerequisiteMissing,
		fmt.Sprintf("Lease %s holds %s, so nothing was pushed", alias, unsavedWork{files: w.files}.describe()),
		fmt.Sprintf("Commit them in %s over ssh, or discard them (zcp does not commit for you), then sync again: %s", alias, LeaseSyncCall(alias)))
}

// leaseSyncPushCommand is the source's push: when the checkout's HEAD is on
// branch, it answers the commit it pushes and pushes refs/heads/<branch> to
// the same branch on origin — with -u (answered "upstream") when the branch
// does not track origin's yet — with --porcelain, so the outcome is git's own
// flag. Never a force flag or a + refspec. A checkout on another branch (or a
// detached HEAD) answers the branch it is on and pushes nothing. The push
// authenticates through the helper the lease's checkout persisted (DP-6).
func leaseSyncPushCommand(branch string) string {
	q := shellQuote
	wd := q(leaseWorkDir)
	read := leaseGitRead + wd
	return strings.Join([]string{
		"b=$(" + read + " symbolic-ref -q --short HEAD)",
		`if [ "$b" != ` + q(branch) + ` ]; then echo "` + leaseSyncMarker + `head $b"; exit 0; fi`,
		"h=$(" + read + " rev-parse -q --verify HEAD) || exit 1",
		`echo "` + leaseSyncMarker + `commit $h"`,
		"u=$(" + read + " rev-parse --symbolic-full-name " + q(branch+"@{upstream}") + " 2>/dev/null)",
		"set_u=",
		`if [ "$u" != ` + q("refs/remotes/origin/"+branch) + ` ]; then set_u=-u; echo "` + leaseSyncMarker + `upstream"; fi`,
		"GIT_TERMINAL_PROMPT=0 git -C " + wd + " push --porcelain $set_u origin " + q("refs/heads/"+branch+":refs/heads/"+branch),
	}, "\n")
}

// leaseSyncAnswer is a sync script's answer: its marker lines by key, and
// every other line.
type leaseSyncAnswer struct {
	marks map[string]string
	rest  []string
}

func parseLeaseSyncAnswer(out string) leaseSyncAnswer {
	a := leaseSyncAnswer{marks: map[string]string{}}
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if body, ok := strings.CutPrefix(line, leaseSyncMarker); ok {
			key, value, _ := strings.Cut(body, " ")
			a.marks[key] = strings.TrimSpace(value)
			continue
		}
		if strings.TrimSpace(line) != "" {
			a.rest = append(a.rest, line)
		}
	}
	return a
}

// pushedRef is git's --porcelain line for the branch's refspec: its flag and
// summary; false when git printed none (it failed before reaching origin).
func (a leaseSyncAnswer) pushedRef(branch string) (flag byte, summary string, ok bool) {
	ref := "refs/heads/" + branch
	for _, line := range a.rest {
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) == 3 && len(fields[0]) == 1 && fields[1] == ref+":"+ref {
			return fields[0][0], fields[2], true
		}
	}
	return 0, "", false
}

// pushLeaseBranch runs the source's push (leaseSyncPushCommand) and reads its
// outcome, refusing a checkout on another branch and a push origin refused or
// that never reached it. onHQ: the lease's repository is on the Mate's HQ.
func pushLeaseBranch(ctx context.Context, ssh SSHDeployer, alias, branch string, onHQ bool) (LeaseSyncPush, string, error) {
	out, err := ssh.ExecSSH(ctx, alias, leaseSyncPushCommand(branch))
	a := parseLeaseSyncAnswer(string(out))
	if head, ok := a.marks["head"]; ok && err == nil {
		on := "a detached HEAD"
		if head != "" {
			on = "branch " + head
		}
		return LeaseSyncPush{}, "", platform.NewPlatformError(platform.ErrPrerequisiteMissing,
			fmt.Sprintf("Lease %s's %s is on %s, not on its branch %s, so nothing was pushed", alias, leaseWorkDir, on, branch),
			fmt.Sprintf("A sync pushes the lease's own branch: check %s out in %s (git -C %s checkout %s) with the commits on it, then sync again: %s", branch, alias, leaseWorkDir, branch, LeaseSyncCall(alias)))
	}
	flag, summary, reported := a.pushedRef(branch)
	if err != nil {
		return LeaseSyncPush{}, "", syncPushRefusal(err, alias, branch, onHQ, summary)
	}
	commit := a.marks["commit"]
	if !reported || !leaseCommitRe.MatchString(commit) {
		return LeaseSyncPush{}, "", fmt.Errorf("push %s from %s: unreadable answer %q", branch, alias, boundedText(string(out)))
	}
	push := LeaseSyncPush{Outcome: LeaseSyncPushed}
	_, push.SetUpstream = a.marks["upstream"]
	switch flag {
	case '=':
		push.Outcome = LeaseSyncUpToDate
	case '*':
		push.Outcome = LeaseSyncNewBranch
	}
	return push, commit, nil
}

// syncPushRefusal is the refusal of a push that failed: origin's rules
// refused the branch (a hook or a protected branch — HQ's branch rule for a
// repository on HQ), origin moved on (non-fast-forward: the agent integrates
// it, never a forced push), or what the checkout's failures say (no
// credential, a refused one, ssh that cannot reach the source).
func syncPushRefusal(err error, alias, branch string, onHQ bool, summary string) error {
	var output string
	var sshErr *platform.SSHExecError
	if errors.As(err, &sshErr) {
		output = sshErr.Output
	}
	var pe *platform.PlatformError
	switch {
	case strings.Contains(summary, "remote rejected") || strings.Contains(output, "[remote rejected]"):
		suggestion := "Origin's own rules (a protected branch or a hook) refused the branch: nothing is wrong with the credential. Push a branch the repository takes, or ask whoever owns it"
		if onHQ {
			suggestion = "HQ takes a push only on a branch of the agent's own: it refuses `main`, a branch another Mate's open change holds, and any forced push. Nothing is wrong with the credential: work on a branch of your own (zerops_lease action=acquire branch=<branch>), commit there and sync it"
		}
		pe = platform.NewPlatformError(platform.ErrPermissionDenied,
			fmt.Sprintf("Origin refused branch %s from lease %s (%s), so no other lease was touched", branch, alias, summary), suggestion)
	case IsNonFastForwardRejection(output):
		pe = platform.NewPlatformError(platform.ErrGitPushNonFastForward,
			fmt.Sprintf("Origin's %s has commits lease %s lacks (%s), so nothing was pushed and no other lease was touched", branch, alias, cmp.Or(summary, "rejected")),
			fmt.Sprintf("Integrate origin/%s in %s over ssh (git -C %s pull --ff-only origin %s, or rebase or merge when both moved), then sync again: %s", branch, alias, leaseWorkDir, branch, LeaseSyncCall(alias)))
	default:
		code, suggestion := classifyCheckoutFailure(err, onHQ)
		pe = platform.NewPlatformError(code, fmt.Sprintf("Lease %s could not push branch %s to origin, so no other lease was touched", alias, branch), suggestion)
	}
	pe.Cause = err
	return pe
}

// leaseSyncMemberCommand is one other lease's step: when its checkout's HEAD
// is on branch, fetch origin's branch, then answer the commit it is on (from)
// and origin's (origin), and — HEAD being origin's: up-to-date; HEAD having
// commits origin's lacks: own <count>, nothing else done; otherwise
// `git merge --ff-only` to origin's commit, answering to <commit>, or, when
// git refuses (it never overwrites a local change), its words and blocked
// <exit>. Up to date or fast-forwarded, a branch that tracks nothing yet is
// set to track origin's; a fast-forward that changed .zcp/dev-servers.json
// also answers declaration. A checkout on another branch answers head
// <branch> and is not touched.
func leaseSyncMemberCommand(branch string) string {
	q := shellQuote
	wd := q(leaseWorkDir)
	read := leaseGitRead + wd
	mark := func(s string) string { return `echo "` + leaseSyncMarker + s + `"` }
	return strings.Join([]string{
		"b=$(" + read + " symbolic-ref -q --short HEAD)",
		`if [ "$b" != ` + q(branch) + ` ]; then ` + mark("head $b") + `; exit 0; fi`,
		"GIT_TERMINAL_PROMPT=0 git -C " + wd + " fetch -q origin " + q("refs/heads/"+branch) + " || exit $?",
		"t=$(" + read + " rev-parse -q --verify FETCH_HEAD) || exit 1",
		"h=$(" + read + " rev-parse -q --verify HEAD) || exit 1",
		mark("from $h"),
		mark("origin $t"),
		"track() { " + read + " rev-parse -q --verify " + q(branch+"@{upstream}") + " >/dev/null 2>&1 || git -C " + wd + " branch -q --set-upstream-to=" + q("origin/"+branch) + " >/dev/null 2>&1 || :; }",
		`if [ "$h" = "$t" ]; then track; ` + mark("up-to-date") + `; exit 0; fi`,
		"if ! " + read + ` merge-base --is-ancestor "$h" "$t"; then ` + mark("own $("+read+` rev-list --count "$t..$h")`) + `; exit 0; fi`,
		`m=$(LC_ALL=C git -C ` + wd + ` merge --ff-only -q "$t" 2>&1); c=$?`,
		`if [ "$c" -ne 0 ]; then printf '%s\n' "$m"; ` + mark("blocked $c") + `; exit 0; fi`,
		"track",
		"if ! " + read + ` diff --quiet "$h" "$t" -- ` + q(devsup.DeclarationPath) + "; then " + mark("declaration") + "; fi",
		mark("to $t"),
	}, "\n")
}

// fastForwardMember runs one other lease's step (leaseSyncMemberCommand) and
// records its outcome on m, with how long it took; a member that moved also
// names its running dev servers nothing reloads. source is the lease the sync
// pushed from, onHQ whether the repository is on the Mate's HQ.
func fastForwardMember(ctx context.Context, d LeaseDeps, m *LeaseSyncMember, branch, source string, onHQ bool) {
	start := time.Now()
	defer func() { m.Millis = time.Since(start).Milliseconds() }()
	out, err := d.SSH.ExecSSH(ctx, m.Alias, leaseSyncMemberCommand(branch))
	if err != nil {
		memberFailure(m, err, onHQ)
		return
	}
	a := parseLeaseSyncAnswer(string(out))
	again := LeaseSyncCall(source)
	if head, ok := a.marks["head"]; ok {
		on := "a detached HEAD"
		if head != "" {
			on = "branch " + head
		}
		m.Outcome = LeaseSyncOnAnotherBranch
		m.Note = fmt.Sprintf("%s's %s is on %s, not %s, so zcp left it as it was; check %s out there (git -C %s checkout %s), then sync again: %s",
			m.Alias, leaseWorkDir, on, branch, branch, leaseWorkDir, branch, again)
		return
	}
	m.From = a.marks["from"]
	theirs := a.marks["origin"]
	switch {
	case hasMark(a, "up-to-date"):
		m.Outcome, m.To = LeaseSyncUpToDate, theirs
	case hasMark(a, "own"):
		m.Outcome = LeaseSyncHasOwnCommits
		m.OwnCommits, _ = strconv.Atoi(a.marks["own"])
		m.Note = fmt.Sprintf("%s has %d commit(s) origin/%s lacks, so zcp left it as it was (it never rebases or merges them): integrate origin/%s there over ssh (rebase or merge), then sync from it: %s",
			m.Alias, m.OwnCommits, branch, branch, LeaseSyncCall(m.Alias))
	case hasMark(a, "blocked"):
		blockedMember(ctx, d.SSH, m, a, again)
	case hasMark(a, "to"):
		m.Outcome, m.To = LeaseSyncFastForwarded, a.marks["to"]
		var fresh map[string]bool
		var declared, unwatched string
		if hasMark(a, "declaration") {
			m.Reloaded, fresh, declared = takeMemberDeclaration(ctx, d, m.Alias, m.Pool)
		}
		m.UnwatchedServers, unwatched = unwatchedServers(ctx, d.SSH, m.Alias, fresh)
		m.Note = joinNotes(declared, unwatched)
	default:
		m.Outcome = LeaseSyncFailed
		m.Note = fmt.Sprintf("%s answered nothing zcp could read (%q), so it may not have been fast-forwarded; zerops_lease action=status shows its checkout", m.Alias, boundedText(string(out)))
	}
}

func hasMark(a leaseSyncAnswer, key string) bool {
	_, ok := a.marks[key]
	return ok
}

// memberStillHeld asks the orchestrator whose a member's container is
// (readLeaseOwnership) and reports whether it is its lease's; one that is not
// — lost, or one the orchestrator did not answer about — is recorded so,
// nothing sent to it.
func memberStillHeld(ctx context.Context, d LeaseDeps, m *LeaseSyncMember, source string) bool {
	rec, err := loadLeaseRecord(d.StateDir, m.Alias)
	if err != nil {
		m.Outcome, m.Note = LeaseSyncFailed, fmt.Sprintf("%s's record could not be read (%s), so zcp did not fast-forward it", m.Alias, errorNote(err))
		return false
	}
	switch v := readLeaseOwnership(ctx, d, rec); {
	case v.own.lost():
		m.Outcome = LeaseSyncLost
		m.Note = lostLeaseNote(m.Alias, v.why, "did not fast-forward it")
	case v.own == leaseUnverified:
		m.Outcome = LeaseSyncUnverified
		m.Note = fmt.Sprintf("%s: %s, so zcp did not fast-forward it; sync again once its pool's orchestrator answers: %s", m.Alias, v.why, LeaseSyncCall(source))
	default:
		return true
	}
	return false
}

// parkedMember records a member that is parked: its container is stopped, so
// zcp tried nothing; once it is unparked, the same sync from source again
// fast-forwards it.
func parkedMember(m *LeaseSyncMember, source string) {
	m.Outcome = LeaseSyncParked
	m.Note = fmt.Sprintf("%s is parked (its container is stopped), so zcp did not fast-forward it; %s starts it, then sync again: %s",
		m.Alias, LeaseUnparkCall(m.Alias), LeaseSyncCall(source))
}

// memberFailure records a member step ssh or git failed: unreachable when ssh
// could not reach the container, else failed with the checkout's
// classification (a missing or refused credential has its own code) and what
// git said.
func memberFailure(m *LeaseSyncMember, err error, onHQ bool) {
	if exitStatus(err) == leaseSSHUnreachableExit {
		m.Outcome = LeaseSyncUnreachable
		m.Note = fmt.Sprintf("zcp could not reach %s over ssh, so it was not fast-forwarded (%s); zerops_lease action=status shows whether its alias and container are in place", m.Alias, sshFailureText(err))
		return
	}
	code, suggestion := classifyCheckoutFailure(err, onHQ)
	m.Outcome, m.Code = LeaseSyncFailed, code
	m.Note = fmt.Sprintf("%s could not be fetched or fast-forwarded (%s). %s", m.Alias, sshFailureText(err), suggestion)
}

// blockedMember records a fast-forward git refused: local changes in the way
// when git says the merge would overwrite them — named with the release
// guard's own read of the checkout — else a failure in git's words.
func blockedMember(ctx context.Context, ssh SSHDeployer, m *LeaseSyncMember, a leaseSyncAnswer, again string) {
	said := strings.Join(a.rest, " ")
	if !strings.Contains(said, "would be overwritten by merge") {
		m.Outcome = LeaseSyncFailed
		m.Note = fmt.Sprintf("git did not fast-forward %s: %s", m.Alias, boundedText(said))
		return
	}
	m.Outcome = LeaseSyncLocalChangesInTheWay
	what := "uncommitted or untracked files"
	if w, err := readUnsavedWork(ctx, ssh, m.Alias); err == nil && len(w.files) > 0 {
		what = unsavedWork{files: w.files}.describe()
	} else {
		what += " (" + boundedText(said) + ")"
	}
	m.Note = fmt.Sprintf("%s holds %s the fast-forward would overwrite, so git left it as it was: commit them there (then integrate origin and sync from it) or discard them, then sync again: %s",
		m.Alias, what, again)
}

// unwatchedServers names the dev servers running on a lease that was just
// fast-forwarded which nothing zcp knows of reloads, and says how to restart
// each; fresh are the servers that started or restarted since the
// fast-forward, which run the new code. Every declared server reloads by
// itself, by the declaration's contract (docs/spec-mate.md §2.8a.1
// servers[].watch): with a watch the supervisor restarts it on a change under
// its paths, and without one its command's own watcher (vite, --watch) does —
// a sync that named such a server had the agent restart one that already
// served the new code. A runtime entry (`ctl run`) is covered by no
// declaration, so nothing reloads it that zcp knows of: it keeps the code it
// started with until restarted. A lease without a supervisor runs none; one
// zcp cannot read is a note.
func unwatchedServers(ctx context.Context, ssh SSHDeployer, alias string, fresh map[string]bool) ([]string, string) {
	ds, err := ReadDevServers(ctx, ssh, alias, "")
	if err != nil {
		var noSup *NoSupervisorError
		if errors.As(err, &noSup) {
			return nil, ""
		}
		return nil, fmt.Sprintf("zcp could not read the dev servers of %s, so it cannot say which keep the old code until restarted (%s)", alias, sshFailureText(err))
	}
	var names []string
	for name, st := range ds.State.Servers {
		if liveServer(st) && !fresh[name] && keepsCodeItStartedWith(st) {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil, ""
	}
	slices.Sort(names)
	calls := make([]string, 0, len(names))
	for _, name := range names {
		calls = append(calls, fmt.Sprintf("zerops_dev_server action=restart hostname=%q name=%q", alias, name))
	}
	return names, fmt.Sprintf("%s run(s) on %s as a runtime entry, which no declared watch covers, so it keeps the code it started with until restarted: %s",
		strings.Join(names, ", "), alias, strings.Join(calls, "; "))
}

// keepsCodeItStartedWith reports whether nothing zcp knows of reloads a
// running server when its checkout changes: true only for a runtime entry. A
// declared server reloads by itself — through the supervisor's watch, or,
// with no watch, through its command's own watcher.
func keepsCodeItStartedWith(st devsup.ServerState) bool {
	return st.Source == devsup.SourceRuntime
}

// takeMemberDeclaration has a lease's supervisor take the
// .zcp/dev-servers.json its fast-forward just changed, as an acquire's has it
// take the checkout's (DP-13): the sibling overlay first, when it changed
// (DP-22, waiting for the restarts it sets off), then a reload, which starts
// an added server, stops a removed one and restarts a changed one. It answers
// whether the supervisor reloaded, the servers that started or restarted on
// the new code, and a note saying what changed or why the supervisor did not
// take it, with the call that makes it. A lease without zcp-devsup has
// nothing to take; a declaration that cannot be read is never reloaded.
func takeMemberDeclaration(ctx context.Context, d LeaseDeps, alias, pool string) (bool, map[string]bool, string) {
	what := "the fast-forward changed " + devsup.DeclarationPath
	rec, err := loadLeaseRecord(d.StateDir, alias)
	if err != nil {
		return false, nil, fmt.Sprintf("%s, but zcp could not read its record of %s (%v), so its supervisor runs the old one until a start: zerops_dev_server action=start hostname=%q name=<server>", what, alias, err, alias)
	}
	ds, err := ReadDevServers(ctx, d.SSH, alias, "")
	var noSup *NoSupervisorError
	switch {
	case errors.As(err, &noSup) && !noSup.Installed:
		return false, nil, ""
	case errors.As(err, &noSup):
		return false, nil, fmt.Sprintf("%s, and no zcp-devsup runs on %s: zerops_dev_server action=start hostname=%q name=<server> starts it on the new one", what, alias, alias)
	case err != nil:
		return false, nil, fmt.Sprintf("%s, but zcp could not read the supervisor on %s (%s), so it runs the old one until a start: zerops_dev_server action=start hostname=%q name=<server>", what, alias, sshFailureText(err), alias)
	}
	ds.Service = pool
	if ds.DeclarationErr != nil {
		return false, nil, fmt.Sprintf("%s to one that cannot be read (%v), so the supervisor on %s keeps the old one: fix it in the lease you commit in, commit and sync again", what, ds.DeclarationErr, alias)
	}
	before := ds.State
	var did []string
	lease := rec.targetInfo()
	env, err := leaseSiblingEnv(d.StateDir, lease, siblingWiringOf(&ds.Declared), d.previewURLs(ctx))
	switch {
	case err != nil:
		did = append(did, fmt.Sprintf("sent no sibling overlay (%v)", err))
	case maps.Equal(ds.State.Env, env.Set):
	case !ds.Supervisor.Supports(devsup.CapabilityEnv):
		did = append(did, "sent no sibling overlay: "+olderSupervisorText(lease, ds.Supervisor, env)+". "+olderSupervisorRecovery(lease))
	default:
		if _, err := applyLeaseOverlay(ctx, d.SSH, alias, ds, env); err != nil {
			did = append(did, fmt.Sprintf("the supervisor refused the sibling overlay (%s)", sshFailureText(err)))
		} else if len(env.Set) == 0 {
			did = append(did, "cleared the sibling overlay")
		} else {
			did = append(did, "sent the sibling overlay ("+env.names()+")")
		}
	}
	st, err := supervisorCtl(ctx, d.SSH, alias, "reload")
	if err != nil {
		return false, startedSince(before, ds.State), fmt.Sprintf("%s, but the supervisor on %s did not take it (%s)%s; zerops_dev_server action=status hostname=%q names what is wrong", what, alias, sshFailureText(err), joinedClause(did), alias)
	}
	fresh := startedSince(before, st)
	did = append(did, "reloaded the supervisor on "+alias)
	if delta := reloadDelta(before, st, fresh); delta != "" {
		did[len(did)-1] += ", which " + delta
	}
	return true, fresh, what + ", so zcp " + strings.Join(did, " and ")
}

// joinedClause is "; " and the clauses, "" when there are none.
func joinedClause(clauses []string) string {
	if len(clauses) == 0 {
		return ""
	}
	return "; zcp " + strings.Join(clauses, " and ")
}

// startedSince names the servers live in after that were not live in before,
// or have restarted since (their restart count grew or their process
// changed): they run the code on disk now.
func startedSince(before, after devsup.State) map[string]bool {
	fresh := map[string]bool{}
	for name, s := range after.Servers {
		if !liveServer(s) {
			continue
		}
		was, ok := before.Servers[name]
		if !ok || !liveServer(was) || s.Restarts > was.Restarts || s.Pid != was.Pid {
			fresh[name] = true
		}
	}
	return fresh
}

// reloadDelta says what a reload and the overlay before it did to the
// servers: those started, restarted and stopped, "" when none.
func reloadDelta(before, after devsup.State, fresh map[string]bool) string {
	var started, restarted, stopped []string
	for _, name := range slices.Sorted(maps.Keys(fresh)) {
		if was, ok := before.Servers[name]; ok && liveServer(was) {
			restarted = append(restarted, name)
		} else {
			started = append(started, name)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(before.Servers)) {
		if now, ok := after.Servers[name]; liveServer(before.Servers[name]) && (!ok || !liveServer(now)) {
			stopped = append(stopped, name)
		}
	}
	var parts []string
	for _, p := range []struct {
		verb  string
		names []string
	}{{"started", started}, {"restarted", restarted}, {"stopped", stopped}} {
		if len(p.names) > 0 {
			parts = append(parts, p.verb+" "+strings.Join(p.names, ", "))
		}
	}
	return strings.Join(parts, ", ")
}
