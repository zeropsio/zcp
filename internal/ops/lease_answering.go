package ops

import (
	"errors"
	"fmt"
	"strings"

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
// zcp ran in it over ssh, when err's class (platform.ClassOf) says it did not
// (unreachable): its process limit (the output carries fork's EAGAIN), or ssh
// not reaching it (its own 255); false when err says neither — the command
// ran and failed on its own.
func leaseNotAnswering(err error) (string, bool) {
	switch platform.ClassOf(err) {
	case platform.ClassUnreachable:
		var sshErr *platform.SSHExecError
		if errors.As(err, &sshErr) {
			out := strings.ToLower(sshErr.Output)
			if strings.Contains(out, "resource temporarily unavailable") && strings.Contains(out, "fork") {
				return leaseProcessLimitWhy, true
			}
		}
		if platform.SSHExitCode(err) >= 0 {
			return leaseUnreachableWhy, true
		}
	case platform.ClassRefused, platform.ClassFailed:
		// An answer: the container was reached.
	}
	return "", false
}

// leaseNotAnsweringAdvice is what to do about a lease that stopped answering
// for why: report it up, never wait on it in a loop. A container at its
// process limit does not recover by itself while what fills it runs, and a
// pool restart restarts every container of it, the other leases' dev servers
// too. One ssh does not reach may be restarting — ssh's own words are in the
// diagnostic — or not admitted, or gone.
func leaseNotAnsweringAdvice(alias, why string) string {
	if why == leaseProcessLimitWhy {
		return fmt.Sprintf("Tell the person now that %s stopped answering, rather than wait on it or retry in a loop: it does not recover by itself while what fills it runs. Restarting its pool restarts every container of it, the other leases' dev servers too", alias)
	}
	return fmt.Sprintf("ssh's own words are in the diagnostic: a container restarting answers again within its start; otherwise check that the pool's sshIsolation admits this service and that zerops_lease action=status shows %s's alias and container in place. One that stays unreachable is told to the person, never waited on in a loop", alias)
}

// leaseNotAnsweringError is the refusal of a call that ran nothing in alias's
// container because it did not answer (why): what was not done, and what to
// do; cause is the ssh error.
func leaseNotAnsweringError(alias, why, notDone string, cause error) *platform.PlatformError {
	pe := platform.NewPlatformError(platform.ErrSSHDeployFailed,
		fmt.Sprintf("Lease %s is not answering — %s — so %s", alias, why, notDone),
		leaseNotAnsweringAdvice(alias, why))
	pe.Cause = cause
	var sshErr *platform.SSHExecError
	if errors.As(cause, &sshErr) {
		pe.Diagnostic = sshErr.Output
	}
	return pe
}
