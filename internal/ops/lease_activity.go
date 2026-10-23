package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

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
)

// leaseActivityCommand is the one ssh invocation that reads a held lease's
// checkout for status (DP-21). First the activity line: the epoch mtime of
// .git/logs/HEAD (0 when there is none), then the newest epoch mtime among
// the modified and untracked files git does not ignore — empty when there is
// none, so the line ends in a space. It never reads .git/index, which a git
// status after it may rewrite. Then the release guard's own two reads
// (leaseUnsavedFilesCommand, leaseUnpushedCommand), each after its marker, so
// status and release agree on what is unsaved. Every git call runs with
// GIT_OPTIONAL_LOCKS=0 (leaseGitRead): the read writes nothing into the
// checkout, and works while another git holds .git/index.lock.
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
	}, " && ")
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
	// before, status and unpushed are what came before the activity line (or
	// between it and git status), from git status, and from the count —
	// stderr included, as ssh hands them over together.
	before, status, unpushed string
}

func splitCheckoutRead(out string) checkoutRead {
	var r checkoutRead
	var before, status, unpushed []string
	part := &before
	for line := range strings.SplitSeq(out, "\n") {
		trimmed := strings.TrimRight(line, "\r")
		switch {
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
	return r
}

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
