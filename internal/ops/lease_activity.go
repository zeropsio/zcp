package ops

import (
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
		reads.Go(func() { v.Activity, v.Unsaved = readLeaseActivity(ctx, d, rec) })
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
	budget := d.ActivityTimeout
	if budget <= 0 {
		budget = leaseActivityReadTimeout
	}
	readCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	out, err := d.SSH.ExecSSH(readCtx, rec.alias(), leaseActivityCommand())
	act := &topology.LeaseActivity{AcquiredAt: rec.AcquiredAt, ReadAt: time.Now().UTC().Format(time.RFC3339)}
	answer := string(out)
	var sshErr *platform.SSHExecError
	if answer == "" && errors.As(err, &sshErr) {
		answer = sshErr.Output
	}
	r := splitCheckoutRead(answer)

	var notes []string
	if r.activity {
		change, note := lastChangeAt(r.activityFields)
		act.LastChangeAt = change
		notes = append(notes, note)
	}
	if r.previewSeen {
		var note string
		act.PreviewLastConnAt, act.PreviewOpenConns, note = leasePreview(r.preview)
		notes = append(notes, note)
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
	return act, unsaved
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
// lists devsup.CapabilityActivity — from any other, from none, or from one
// with no preview server, both are absent and note says why the preview was
// not observed.
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
		return "", nil, notObserved + "no dev server in the container has a preview port"
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
