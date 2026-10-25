package ops

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// A lease's container stops answering when nothing zcp runs in it over ssh
// can start: ssh does not reach it (exit 255), or the container is at its
// process limit, so its ssh server cannot start the shell — fork answers
// EAGAIN, "resource temporarily unavailable" (live, 2026-10-25: a full
// production build in webpool-5 filled its process table; `ssh webpool-5 echo`
// answered "fork/exec /bin/bash: resource temporarily unavailable" with exit
// 127 while its dev server still served, a helper looped `until ssh webpool-5`
// for 20 minutes, the release was refused and the pool's restart failed for
// it). Status and every call that runs in a lease say so, with the reason
// (docs/spec-workflows.md §8 DP-21).

// leaseProcessLimitWhy is why a container at its process limit does not answer.
const leaseProcessLimitWhy = "it cannot start a new process: the container reached its process limit (fork answered \"resource temporarily unavailable\", EAGAIN), so nothing runs in it until the processes filling it end"

// leaseUnreachableWhy is why a container ssh does not reach does not answer.
const leaseUnreachableWhy = "ssh did not reach the container"

// leaseNotAnswering says why the lease's container did not answer a command
// zcp ran in it over ssh, when err says it did not: its process limit (the
// output carries fork's EAGAIN), or ssh not reaching it (exit 255); false
// when err says neither — the command ran and failed on its own.
func leaseNotAnswering(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	var sshErr *platform.SSHExecError
	if errors.As(err, &sshErr) {
		out := strings.ToLower(sshErr.Output)
		if strings.Contains(out, "resource temporarily unavailable") && strings.Contains(out, "fork") {
			return leaseProcessLimitWhy, true
		}
	}
	if exitStatus(err) == leaseSSHUnreachableExit {
		return leaseUnreachableWhy, true
	}
	return "", false
}

// leaseAnswered reports whether a command zcp ran in a lease's container over
// ssh reached it: it succeeded, or exited with a status of its own — not ssh's
// 255, not the process limit's refusal. A call cut short (its context ended,
// the process killed) says neither.
func leaseAnswered(err error) bool {
	if err == nil {
		return true
	}
	if _, silent := leaseNotAnswering(err); silent {
		return false
	}
	return exitStatus(err) >= 0
}

// noteLeaseAnswer records in rec's record what a call that ran a command in
// the lease's container over ssh found (err): not answering, and why, since
// now — kept as it is while a later call finds it so for the same reason — or,
// once a call reached it again (leaseAnswered), nothing. A call that says
// neither changes nothing. The record is read and written under its alias's
// record lock, only while it is still rec's lease's (leaseRecordReplaced), and
// only its own two fields change. The record is a hint for the envelope
// (docs/spec-mate.md §2.8a.3), never the call's outcome: one zcp cannot lock,
// read or write is left as it is.
func noteLeaseAnswer(stateDir string, rec leaseRecord, err error, now time.Time) {
	why, silent := leaseNotAnswering(err)
	if !silent && !leaseAnswered(err) {
		return
	}
	unlock, lerr := lockLeaseRecord(stateDir, rec.alias())
	if lerr != nil {
		return
	}
	defer unlock()
	if leaseRecordReplaced(stateDir, rec) {
		return
	}
	cur, lerr := loadLeaseRecord(stateDir, rec.alias())
	if lerr != nil || cur.NotAnswering == why {
		return
	}
	cur.NotAnswering, cur.NotAnsweringSince = why, ""
	if silent {
		cur.NotAnsweringSince = now.UTC().Format(time.RFC3339)
	}
	_ = writeLeaseRecordLocked(stateDir, cur)
}

// leaseNotAnsweringAdvice is what to do about a lease that stopped answering
// for why: report it up, never wait on it. A container at its process limit
// does not recover by itself while what fills it runs, and a pool restart
// restarts every container of it, the other leases' dev servers too.
func leaseNotAnsweringAdvice(alias, why string) string {
	if why == leaseProcessLimitWhy {
		return fmt.Sprintf("Tell the person now that %s stopped answering, rather than wait on it or retry in a loop: it does not recover by itself while what fills it runs. Restarting its pool restarts every container of it, the other leases' dev servers too", alias)
	}
	return fmt.Sprintf("Check that the pool's sshIsolation admits this service and that zerops_lease action=status shows %s's alias and container in place; one that stays unreachable is told to the person, never waited on", alias)
}

// leaseNotAnsweringError is the refusal of a call that ran nothing in alias's
// container because it did not answer (why): what was not done, and what to
// do; cause is the ssh error.
func leaseNotAnsweringError(alias, why, notDone string, cause error) *platform.PlatformError {
	pe := platform.NewPlatformError(platform.ErrSSHDeployFailed,
		fmt.Sprintf("Lease %s is not answering — %s — so %s", alias, why, notDone),
		leaseNotAnsweringAdvice(alias, why))
	pe.Cause = cause
	return pe
}
