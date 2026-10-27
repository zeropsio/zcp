package ops

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// leaseActivityReadTimeout bounds the one read of a held lease's checkout a
// status call makes: a single ssh invocation that answers in tens of
// milliseconds (live: 62/65 ms median/p95; 118/156 ms on a 10,000-file
// checkout with 150 changes), so a container that does not answer costs a
// status call seconds, not ssh's own minutes.
const leaseActivityReadTimeout = 5 * time.Second

// The lines that frame the parts of the read's answer.
const (
	leaseActivityMarker = "@@zcp-lease activity "
	leaseStatusMarker   = "@@zcp-lease status"
	leaseUnpushedMarker = "@@zcp-lease unpushed"
	leasePreviewMarker  = "@@zcp-lease preview"
)

// leasePreviewReadSeconds bounds the supervisor's `ctl status` inside the
// read, where the container has `timeout`: a supervisor that does not answer
// costs the read seconds, never the checkout facts read before it.
const leasePreviewReadSeconds = 2

// leaseActivityCommand is the one ssh invocation that reads a held lease's
// checkout for status (DP-21). First the activity line: the epoch mtime of
// .git/logs/HEAD (0 when there is none), then the newest epoch mtime among
// the modified and untracked files git does not ignore — empty when there is
// none, so the line ends in a space. It never reads .git/index, which a git
// status after it may rewrite. Then the release guard's own two reads
// (leaseUnsavedFilesCommand, leaseUnpushedCommand), each after its marker, so
// status and release agree on what is unsaved. Every git call runs with
// GIT_OPTIONAL_LOCKS=0 (leaseGitRead): the read writes nothing into the
// checkout, and works while another git holds .git/index.lock. Last, after
// its marker and whatever the checkout part did, the supervisor's preview
// activity (leasePreviewCommand); the read exits with the checkout part's
// status, so a failed git read stays the read's failure.
func leaseActivityCommand() string {
	return strings.Join([]string{
		"cd " + shellQuote(leaseWorkDir),
		"printf " + shellQuote(leaseActivityMarker+"%s %s\\n") +
			` "$(stat -c %Y .git/logs/HEAD 2>/dev/null || echo 0)"` +
			` "$(GIT_OPTIONAL_LOCKS=0 git ls-files -z -m -o --exclude-standard 2>/dev/null | xargs -0 -r stat -c %Y -- 2>/dev/null | sort -n | tail -1)"`,
		"echo " + shellQuote(leaseStatusMarker),
		leaseUnsavedFilesCommand(),
		"echo " + shellQuote(leaseUnpushedMarker),
		leaseUnpushedCommand(),
	}, " && ") + "; rc=$?; echo " + shellQuote(leasePreviewMarker) + "; " + leasePreviewCommand() + "; exit $rc"
}

// leasePreviewCommand reads the container's zcp-devsup with two verbs every
// supervisor knows — `ctl version` and `ctl status`, the status bounded by
// leasePreviewReadSeconds — each closed by a devsup section with its exit
// (version 127 without the binary). Whether the status's preview fields are
// read is zcp's decision, from the version's capabilities (leasePreview).
func leasePreviewCommand() string {
	return `if command -v zcp-devsup >/dev/null 2>&1; then ` +
		`zcp-devsup ctl version --json 2>/dev/null; c=$?; echo; echo "@@zcp-devsup version $c"; ` +
		`if command -v timeout >/dev/null 2>&1; then b="timeout ` + strconv.Itoa(leasePreviewReadSeconds) + `"; else b=; fi; ` +
		`$b zcp-devsup ctl status --json 2>/dev/null; c=$?; echo; echo "@@zcp-devsup status $c"; ` +
		`else echo '@@zcp-devsup version 127'; fi`
}

// readLeaseActivities fills in, for each lease of views zcp holds — held or
// unmounted, confirmed by its orchestrator, its ssh alias installed — what
// one read of its checkout found (readLeaseActivity): every lease at once,
// each within its own budget. A lease zcp holds but does not read says why.
// Without ssh (the envelope's lease reader) nothing is read or said. It
// changes nothing anywhere: no alias is restored, no orchestrator called.
func readLeaseActivities(ctx context.Context, d LeaseDeps, records []leaseRecord, views []LeaseView) {
	if d.SSH == nil {
		return
	}
	byAlias := make(map[string]leaseRecord, len(records))
	for _, r := range records {
		byAlias[r.alias()] = r
	}
	var reads sync.WaitGroup
	for i := range views {
		v := &views[i]
		rec, recorded := byAlias[v.Alias]
		if !recorded || (v.State != LeaseStateHeld && v.State != LeaseStateUnmounted) {
			continue
		}
		if why := leaseActivitySkip(d, *v); why != "" {
			v.Activity = &topology.LeaseActivity{AcquiredAt: rec.AcquiredAt, Note: why}
			continue
		}
		reads.Go(func() {
			r := readLeaseFacts(ctx, d, rec)
			v.Activity, v.Unsaved, v.overlay, v.NotAnswering = r.activity, r.unsaved, r.overlay, r.notAnswering
		})
	}
	reads.Wait()
}

// leaseActivitySkip says why a lease zcp holds is not read, "" when it is: a
// lease its orchestrator did not confirm may no longer own its container, and
// one without its ssh alias cannot be reached by it.
func leaseActivitySkip(d LeaseDeps, v LeaseView) string {
	switch {
	case v.Unverified:
		return "not read: the pool's orchestrator did not confirm this lease, so its container may no longer be this lease's"
	case d.Host.Run != nil && !d.Host.aliasInstalled(v.Alias):
		return "not read: its ssh alias is not installed on this host; zcp init restores it after a redeploy"
	}
	return ""
}

// readLeaseActivity runs the one read of rec's checkout (leaseActivityCommand)
// within the activity budget and says what it found: the activity, always —
// with what it could not read and why in its note — and the unsaved work, nil
// unless the guard's two reads both answered. A part that failed keeps what
// the parts before it read; nothing unread is ever zero.
func readLeaseActivity(ctx context.Context, d LeaseDeps, rec leaseRecord) (*topology.LeaseActivity, *topology.LeaseUnsaved) {
	r := readLeaseFacts(ctx, d, rec)
	return r.activity, r.unsaved
}

// leaseFacts is what one read of a lease's checkout found
// (readLeaseActivity) and the overlay its supervisor part carried (overlayOf,
// DP-26).
type leaseFacts struct {
	activity *topology.LeaseActivity
	unsaved  *topology.LeaseUnsaved
	overlay  *leaseOverlay
	// notAnswering is why the container did not answer the read
	// (leaseNotAnswering), "" when it did.
	notAnswering string
}

// readLeaseFacts is readLeaseActivity with whether the container has no
// preview to observe and its overlay.
func readLeaseFacts(ctx context.Context, d LeaseDeps, rec leaseRecord) leaseFacts {
	budget := d.ActivityTimeout
	if budget <= 0 {
		budget = leaseActivityReadTimeout
	}
	readCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	out, err := d.SSH.ExecSSH(readCtx, rec.alias(), leaseActivityCommand())
	noteLeaseAnswer(d.StateDir, rec, err, d.now())
	act := &topology.LeaseActivity{AcquiredAt: rec.AcquiredAt, ReadAt: d.now().UTC().Format(time.RFC3339)}
	answer := string(out)
	var sshErr *platform.SSHExecError
	if answer == "" && errors.As(err, &sshErr) {
		answer = sshErr.Output
	}
	r := splitCheckoutRead(answer)

	var notes []string
	var overlay *leaseOverlay
	if r.activity {
		change, note := lastChangeAt(r.activityFields)
		act.LastChangeAt = change
		notes = append(notes, note)
	}
	if r.previewSeen {
		var note string
		act.PreviewLastConnAt, act.PreviewOpenConns, note = leasePreview(r.preview)
		notes = append(notes, note)
		overlay = overlayOf(r.preview)
	}
	var unsaved *topology.LeaseUnsaved
	switch {
	case err != nil:
		notes = append(notes, checkoutReadFailure(ctx, readCtx, budget, err, r))
	case !r.activity:
		notes = append(notes, fmt.Sprintf("the read answered %q, with no activity line, so nothing was read", boundedText(answer)))
	case !r.unpushedSeen:
		notes = append(notes, "the read answered no unsaved-work part, so the unsaved work was not read")
	default:
		n, perr := parseUnpushedCount(r.unpushed)
		if perr != nil {
			notes = append(notes, "the unsaved work was not read: "+perr.Error())
			break
		}
		unsaved = unsavedWork{files: parseUnsavedFiles(r.status), unpushed: n}.view()
	}
	act.Note = joinNotes(notes...)
	why, _ := leaseNotAnswering(err)
	return leaseFacts{activity: act, unsaved: unsaved, overlay: overlay, notAnswering: why}
}

// checkoutRead is the read's answer split at its markers.
type checkoutRead struct {
	activity       bool   // the activity line came
	activityFields string // its two fields, as answered
	statusSeen     bool   // the guard's git status began
	unpushedSeen   bool   // the guard's unpushed count began
	previewSeen    bool   // the supervisor part began
	// before, status and unpushed are what came before the activity line (or
	// between it and git status), from git status, and from the count —
	// stderr included, as ssh hands them over together; preview is the
	// supervisor part, wherever the checkout part stopped.
	before, status, unpushed, preview string
}

func splitCheckoutRead(out string) checkoutRead {
	var r checkoutRead
	var before, status, unpushed, preview []string
	part := &before
	for line := range strings.SplitSeq(out, "\n") {
		trimmed := strings.TrimRight(line, "\r")
		switch {
		case r.previewSeen:
			preview = append(preview, line)
		case trimmed == leasePreviewMarker:
			r.previewSeen = true
		case !r.activity && strings.HasPrefix(trimmed, leaseActivityMarker):
			r.activity, r.activityFields = true, strings.TrimPrefix(trimmed, leaseActivityMarker)
		case r.activity && !r.statusSeen && trimmed == leaseStatusMarker:
			r.statusSeen, part = true, &status
		case r.statusSeen && !r.unpushedSeen && trimmed == leaseUnpushedMarker:
			r.unpushedSeen, part = true, &unpushed
		default:
			*part = append(*part, line)
		}
	}
	r.before, r.status, r.unpushed = strings.Join(before, "\n"), strings.Join(status, "\n"), strings.Join(unpushed, "\n")
	r.preview = strings.Join(preview, "\n")
	return r
}

// leasePreview reads the supervisor part of the read: the preview server's
// last connection time (RFC 3339 UTC, "" when none came in the supervisor's
// life) and its open connections, from a supervisor whose `ctl version`
// lists devsup.CapabilityActivity — from any other or from none, both are
// absent and note says why the preview was not observed. One that lists it
// and runs no dev server on a preview port has no preview forward, so no
// preview connection can be open: openConns is an observed 0 (DP-21).
func leasePreview(part string) (lastConnAt string, openConns *int, note string) {
	const notObserved = "preview not observed: "
	sections, err := splitDevsupSections(part)
	if err != nil {
		return "", nil, fmt.Sprintf(notObserved+"zcp could not read the supervisor's answer (%v)", err)
	}
	version, ok := sections["version"]
	switch {
	case !ok:
		return "", nil, fmt.Sprintf(notObserved+"the supervisor part answered %q, with no zcp-devsup version", boundedText(part))
	case version.code == devsupCodeAbsent:
		return "", nil, notObserved + "zcp-devsup is not installed in the container"
	case version.code != devsupCodeOK:
		return "", nil, fmt.Sprintf(notObserved+"zcp-devsup ctl version exited %d", version.code)
	}
	var v SupervisorVersion
	if err := json.Unmarshal([]byte(version.body), &v); err != nil {
		return "", nil, fmt.Sprintf(notObserved+"zcp-devsup ctl version answered %q", boundedText(version.body))
	}
	if !v.Supports(devsup.CapabilityActivity) {
		return "", nil, fmt.Sprintf(notObserved+"the container's zcp-devsup %s predates preview activity (its ctl version lists no %q); one from a newer zcp release reports it",
			v.Version, devsup.CapabilityActivity)
	}
	status, ok := sections["status"]
	switch {
	case !ok:
		return "", nil, notObserved + "zcp-devsup ctl status did not answer"
	case status.code == devsupCodeNoSupervisor:
		return "", nil, notObserved + "no zcp-devsup runs in the container (its dev setup's run.start is something else)"
	case status.code == devsupCodeUnresponsive || status.code == timeoutExit:
		return "", nil, fmt.Sprintf(notObserved+"the dev-server supervisor did not answer within %d s (the container may be out of memory or CPU)", leasePreviewReadSeconds)
	case status.code != devsupCodeOK:
		note = fmt.Sprintf(notObserved+"zcp-devsup ctl status exited %d", status.code)
		var failure *SupervisorCtlError
		if errors.As(ctlFailure(status), &failure) && failure.Message != "" {
			note += ": " + boundedText(failure.Message)
		}
		return "", nil, note
	}
	var st devsup.State
	if err := json.Unmarshal([]byte(status.body), &st); err != nil {
		return "", nil, fmt.Sprintf(notObserved+"zcp-devsup ctl status answered %q", boundedText(status.body))
	}
	var last time.Time
	open, found := 0, false
	for _, ss := range st.Servers {
		if ss.PreviewPort == 0 || ss.PreviewOpenConns == nil {
			continue
		}
		found = true
		open += *ss.PreviewOpenConns
		if ss.PreviewLastConnAt != nil && ss.PreviewLastConnAt.After(last) {
			last = *ss.PreviewLastConnAt
		}
	}
	if !found {
		return "", &open, ""
	}
	if !last.IsZero() {
		lastConnAt = last.UTC().Format(time.RFC3339)
	}
	return lastConnAt, &open, ""
}

// timeoutExit is the exit status of a command `timeout` ended.
const timeoutExit = 124

// lastChangeAt reads the activity line's two fields — the .git/logs/HEAD
// mtime (0: none) and the newest changed file's mtime ("": none) — as the
// checkout's last change, the newer of the two, "" when it has neither, with
// a note saying what was missing.
func lastChangeAt(fields string) (at, note string) {
	head, files, _ := strings.Cut(fields, " ")
	headAt, err := strconv.ParseInt(head, 10, 64)
	var filesAt int64
	if files = strings.TrimSpace(files); err == nil && files != "" {
		filesAt, err = strconv.ParseInt(files, 10, 64)
	}
	if err != nil || headAt < 0 || filesAt < 0 {
		return "", fmt.Sprintf("the activity read answered %q, so the checkout's last change is unknown", fields)
	}
	switch {
	case headAt == 0 && filesAt == 0:
		return "", "the checkout has no .git/logs/HEAD and no modified file, so its last change is unknown"
	case headAt == 0:
		note = "the checkout has no .git/logs/HEAD, so only its files' changes were read"
	}
	return time.Unix(max(headAt, filesAt), 0).UTC().Format(time.RFC3339), note
}

// checkoutReadFailure says which part of a failed read failed and why, in
// the remote's own words (an ssh error's Error() drops them): the read cut
// at its budget, the call ending first, ssh not reaching the container, or
// the checkout, git status or the unpushed count failing.
func checkoutReadFailure(callCtx, readCtx context.Context, budget time.Duration, err error, r checkoutRead) string {
	var what, said string
	switch {
	case callCtx.Err() != nil:
		return "the read was cancelled: the status call ended before it finished"
	case errors.Is(readCtx.Err(), context.DeadlineExceeded):
		return fmt.Sprintf("the read did not finish within %s, so nothing more was read", budget)
	case exitStatus(err) == leaseSSHUnreachableExit:
		what, said = "zcp could not reach the container over ssh", r.before
	case !r.activity:
		what, said = "the checkout at "+leaseWorkDir+" could not be read", r.before
	case !r.unpushedSeen:
		what, said = "git status failed, so the unsaved work was not read", r.status
	default:
		what, said = "counting the commits on no remote branch failed, so the unsaved work was not read", r.unpushed
	}
	msg := what + " (" + err.Error() + ")"
	if said = strings.TrimSpace(said); said != "" {
		msg += ": " + boundedTail(said)
	}
	return msg
}

// view is the unsaved work as status reports it: the first files a refused
// release would name, all of them counted, and the commits on no remote
// branch.
func (w unsavedWork) view() *topology.LeaseUnsaved {
	shown := w.files
	if len(shown) > leaseGuardFileLimit {
		shown = shown[:leaseGuardFileLimit]
	}
	return &topology.LeaseUnsaved{Files: slices.Clone(shown), FileCount: len(w.files), UnpushedCommits: w.unpushed}
}

// leaseIdleAfter is how long a lease zcp holds goes without activity before
// status marks it idle (DP-25): 24 h, the owner's answer. Idleness is a label:
// nothing releases, stops or deletes a lease because it is idle
// (docs/spec-mate.md §0 rule 4).
const leaseIdleAfter = 24 * time.Hour

// leaseReleaseTakes is what every release takes that no guard reads.
const leaseReleaseTakes = "Files git ignores and stashes go with any release."

// LeaseIdle marks a lease zcp holds that nothing has happened in for
// leaseIdleAfter (DP-25).
type LeaseIdle struct {
	// Since is the lease's last activity (RFC 3339 UTC): the newest of its
	// checkout's last change, its preview's last connection and its acquire —
	// of every lease of its group, for a lease of a group.
	Since string `json:"since"`
	// Next is what frees it — the release, when it holds nothing a release
	// would lose — or what a release would lose and how to keep it. It never
	// names a forced release.
	Next string `json:"next"`
}

// leaseLiveness is what one read of a lease says of its last activity.
type leaseLiveness struct {
	// last is its newest activity — the checkout's last change, the
	// preview's last connection, the acquire — and zero when the checkout's
	// last change was not read: an acquire alone tells nothing.
	last time.Time
	// inUse: the preview holds a connection open now, however long ago it
	// was opened — someone may be looking at it.
	inUse bool
	// previewSeen: the preview was observed (a container with no preview
	// server reads an observed 0 open connections).
	previewSeen bool
	// unsavedRead: the release guard's reads answered.
	unsavedRead bool
}

// livenessOf reads v's activity as its liveness.
func livenessOf(v LeaseView) leaseLiveness {
	a := v.Activity
	if a == nil || a.LastChangeAt == "" {
		return leaseLiveness{unsavedRead: v.Unsaved != nil}
	}
	l := leaseLiveness{
		inUse:       a.PreviewOpenConns != nil && *a.PreviewOpenConns > 0,
		previewSeen: a.PreviewOpenConns != nil,
		unsavedRead: v.Unsaved != nil,
	}
	for _, at := range []string{a.LastChangeAt, a.PreviewLastConnAt, a.AcquiredAt} {
		if t, err := time.Parse(time.RFC3339, at); err == nil && t.After(l.last) {
			l.last = t
		}
	}
	return l
}

// idle reports whether the lease is idle at now: every fact read, no
// preview connection open, and its last activity leaseIdleAfter or more ago.
// A fact that was not read is never taken for no activity.
func (l leaseLiveness) idle(now time.Time) bool {
	return !l.last.IsZero() && l.previewSeen && l.unsavedRead && !l.inUse && now.Sub(l.last) >= leaseIdleAfter
}

// leaseGroup is the leases zcp holds of one repository and branch (DP-17),
// earliest acquired first; a lease whose repository zcp knows neither way is
// a group of its own.
type leaseGroup struct {
	branch, repo string
	records      []leaseRecord
	// ambiguous: zcp leases the branch for another repository too, so the
	// branch's release names repo.
	ambiguous bool
}

// leaseGroupsOf groups records as a release of a branch reads them
// (branchLeaseRecords): by repository identity and branch.
func leaseGroupsOf(stateDir string, records []leaseRecord) []leaseGroup {
	var groups []leaseGroup
	at := map[string]int{}
	repos := map[string]map[string]bool{} // branch → the repository keys it is leased for
	for _, r := range records {
		branch, repo := recordBranch(r), recordRepo(stateDir, r)
		key := "\x00" + r.alias()
		if repo != "" {
			key = strings.ToLower(repo) + "\x00" + branch
		}
		if repos[branch] == nil {
			repos[branch] = map[string]bool{}
		}
		repos[branch][key] = true
		i, seen := at[key]
		if !seen {
			i = len(groups)
			at[key] = i
			groups = append(groups, leaseGroup{branch: branch, repo: repo})
		}
		groups[i].records = append(groups[i].records, r)
	}
	for i := range groups {
		g := &groups[i]
		g.ambiguous = len(repos[g.branch]) > 1
		slices.SortFunc(g.records, func(a, b leaseRecord) int {
			return cmp.Or(cmp.Compare(a.AcquiredAt, b.AcquiredAt), cmp.Compare(a.alias(), b.alias()))
		})
	}
	return groups
}

// markIdleLeases marks the idle leases among views, which this status call
// read (DP-25). A lease is idle when every fact of it was read, no preview
// connection is open, and nothing has happened in it — no checkout change, no
// preview connection, no acquire — for leaseIdleAfter. The leases of a group
// (DP-17) are idle together or not at all: only when this call read every
// lease of the group — a status of one pool reads none of its leases on
// others — and each held one is idle, since the newest activity of any; they
// name the branch's release. Without ssh nothing was read and nothing is
// marked. It changes nothing.
func markIdleLeases(d LeaseDeps, records []leaseRecord, views []LeaseView) {
	if d.SSH == nil {
		return
	}
	now := d.now()
	at := make(map[string]int, len(views))
	for i, v := range views {
		at[v.Alias] = i
	}
	for _, g := range leaseGroupsOf(d.StateDir, records) {
		var held []int
		read := true
		for _, r := range g.records {
			i, ok := at[r.alias()]
			if !ok {
				read = false
				break
			}
			if s := views[i].State; s == LeaseStateHeld || s == LeaseStateUnmounted {
				held = append(held, i)
			}
		}
		if !read || len(held) == 0 {
			continue
		}
		var since time.Time
		idle := true
		for _, i := range held {
			l := livenessOf(views[i])
			if !l.idle(now) {
				idle = false
				break
			}
			if l.last.After(since) {
				since = l.last
			}
		}
		if !idle {
			continue
		}
		next := idleNext(d, g, views, held, now.Sub(since))
		for _, i := range held {
			views[i].Idle = &LeaseIdle{Since: since.UTC().Format(time.RFC3339), Next: next}
		}
	}
}

// idleNext is what an idle lease — or an idle group, held — names: with
// nothing any of them would lose, the release (the branch's, for a group of
// several held leases); otherwise what each holds and how to keep it. Never a
// forced release.
func idleNext(d LeaseDeps, g leaseGroup, views []LeaseView, held []int, quiet time.Duration) string {
	onHQ := false
	for _, r := range g.records {
		onHQ = onHQ || IsHQRemote(r.Repo, d.hqAddress())
	}
	ago := agoText(quiet)
	if len(held) == 1 {
		v := views[held[0]]
		if u := v.Unsaved; u.FileCount > 0 || u.UnpushedCommits > 0 {
			return fmt.Sprintf("Nothing has happened in %s for %s, but a release would lose %s. %s. Or park it, to stop its container and keep everything in it: %s.",
				v.Alias, ago, describeUnsaved(u.Files, u.FileCount, u.UnpushedCommits), saveWorkHint(onHQ, "the container"), LeaseParkCall(v.Alias))
		}
		return fmt.Sprintf("Nothing has happened in %s for %s and it holds no uncommitted, untracked or unpushed work: zerops_lease action=release lease=%s frees its container. %s",
			v.Alias, ago, v.Alias, leaseReleaseTakes)
	}
	aliases := make([]string, 0, len(held))
	states := make([]string, 0, len(held))
	unsaved := false
	for _, i := range held {
		v := views[i]
		aliases = append(aliases, v.Alias)
		if u := v.Unsaved; u.FileCount > 0 || u.UnpushedCommits > 0 {
			unsaved = true
			states = append(states, v.Alias+" holds "+describeUnsaved(u.Files, u.FileCount, u.UnpushedCommits))
			continue
		}
		states = append(states, v.Alias+" holds no unsaved work")
	}
	if unsaved {
		return fmt.Sprintf("Nothing has happened in branch %s's leases (%s) for %s, but a release would lose what they hold: %s. %s. Or park each, to stop its container and keep everything in it: %s for each of them.",
			g.branch, joinAnd(aliases), ago, strings.Join(states, "; "), saveWorkHint(onHQ, "each container that holds it"), LeaseParkCall("<alias>"))
	}
	repo := ""
	if g.ambiguous {
		repo = g.repo
	}
	return fmt.Sprintf("Nothing has happened in branch %s's leases (%s) for %s and none holds uncommitted, untracked or unpushed work: %s releases them all. %s",
		g.branch, joinAnd(aliases), ago, LeaseReleaseBranchCall(g.branch, repo, false), leaseReleaseTakes)
}

// agoText says how long d is: under an hour, whole hours below two days,
// whole days from then.
func agoText(d time.Duration) string {
	switch {
	case d < time.Hour:
		return "less than an hour"
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h", int(d.Hours()))
	}
	return fmt.Sprintf("%d d", int(d.Hours()/24))
}

// byOldestActivity orders leases oldest activity first: those whose last
// activity was read, longest ago first; then those in use now; then those
// whose activity was not read; each by container number on a tie.
func byOldestActivity(a, b LeaseView) int {
	la, lb := livenessOf(a), livenessOf(b)
	return cmp.Or(cmp.Compare(la.rank(), lb.rank()), la.last.Compare(lb.last), cmp.Compare(leaseNumber(a.Alias), leaseNumber(b.Alias)))
}

// rank is where a lease sorts by its activity (byOldestActivity).
func (l leaseLiveness) rank() int {
	switch {
	case l.last.IsZero():
		return 2
	case l.inUse:
		return 1
	}
	return 0
}

// activitySummary is a lease as a full pool's refusal lists it: how recently
// it was active — idle, when, in use now, or not read — and what a release
// would lose.
func activitySummary(v LeaseView, now time.Time) string {
	l := livenessOf(v)
	var when string
	switch {
	case l.last.IsZero():
		when = "activity not read"
	case l.inUse:
		when = "in use now (a preview connection is open)"
	case !l.previewSeen:
		when = "last checkout change " + agoText(now.Sub(l.last)) + " ago, its preview not observed"
	case l.idle(now):
		when = "idle, last active " + agoText(now.Sub(l.last)) + " ago"
	default:
		when = "last active " + agoText(now.Sub(l.last)) + " ago"
	}
	what := "unsaved work not read"
	if u := v.Unsaved; u != nil {
		var parts []string
		if u.FileCount > 0 {
			parts = append(parts, fmt.Sprintf("%d uncommitted or untracked file(s)", u.FileCount))
		}
		if u.UnpushedCommits > 0 {
			parts = append(parts, fmt.Sprintf("%d commit(s) on no remote branch", u.UnpushedCommits))
		}
		what = cmp.Or(strings.Join(parts, " and "), "nothing unsaved")
	}
	return v.Alias + " — " + when + ", " + what
}

// releasable reports a lease a release would free without losing anything
// the guard reads: read whole, holding nothing unsaved, not in use now.
func releasable(v LeaseView) bool {
	l := livenessOf(v)
	return !l.last.IsZero() && !l.inUse && v.Unsaved != nil && v.Unsaved.FileCount == 0 && v.Unsaved.UnpushedCommits == 0
}
