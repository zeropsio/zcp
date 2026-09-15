package ops

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// GitNonFastForwardPattern matches git's rejection family for a
// non-fast-forward push — the remote ref carries commits the local push
// lacks ("fetch first" / "[rejected]" / "Updates were rejected" / the bare
// "non-fast-forward" phrasing). Single owner: both the deploy-failure
// classifier (deploy_failure_signals.go, transport:git-non-fast-forward)
// and the GIT_PUSH_NON_FAST_FORWARD structured-error path
// (tools.handleGitPush / handleLocalGitPush) match against this one regex
// so the two can never drift apart on what counts as "rejected".
var GitNonFastForwardPattern = regexp.MustCompile(`(?:\[rejected\]|fetch first|non-fast-forward|Updates were rejected)`)

// IsNonFastForwardRejection reports whether a failed push's combined
// output (stdout+stderr) matches git's non-fast-forward rejection family.
func IsNonFastForwardRejection(output string) bool {
	return GitNonFastForwardPattern.MatchString(output)
}

// gitRefNotFoundPattern matches git's rejection when fetching a ref that
// does not exist on the remote yet — the "empty" remote state (nothing has
// ever been pushed to this ref; the first push is always safe, never a
// rejection risk).
var gitRefNotFoundPattern = regexp.MustCompile(`(?i:couldn't find remote ref|fatal: couldn't find remote ref)`)

// GitRunner executes one `git <args...>` invocation (at whatever working
// directory / host the caller closed over) and returns combined
// stdout+stderr. On a non-zero exit, err is non-nil and its Error() text
// carries the command's stderr (or the best available diagnostic) — the
// same contract runGitWithEnv (local) and the SSH gitPushErrorDetail
// wrapper (container) already honor. Two callers supply this: the
// container path wraps ops.SSHDeployer.ExecSSH via
// BuildGitDivergenceStepCommand, the local path wraps exec.Command
// directly.
type GitRunner func(args ...string) (output string, err error)

// GitDivergenceProbe is the result of fetching a tracked ref from origin
// and comparing it against local HEAD — used both to populate the
// GIT_PUSH_NON_FAST_FORWARD error payload after a rejected push and to
// compute the git-push-setup probe-time early warning (remote.state).
type GitDivergenceProbe struct {
	// RemoteAhead is the count of commits on the fetched remote ref that
	// are not reachable from local HEAD.
	RemoteAhead int
	// LocalAhead is the count of commits on local HEAD that are not
	// reachable from the fetched remote ref.
	LocalAhead int
	// Unrelated is true when HEAD and the fetched remote ref share no
	// common ancestor (git merge-base finds none) — e.g. two independently
	// initialized repos pushed to the same URL.
	Unrelated bool
}

// ProbeGitDivergence fetches ref from origin via run, then computes how far
// local HEAD and the fetched remote ref have diverged. refExists is false
// (probe, nil error) when ref does not exist on the remote yet — the
// caller's "empty" state, not a probe failure. A non-nil error means the
// fetch itself could not be completed for any other reason (auth, network)
// and the divergence is unknown.
func ProbeGitDivergence(run GitRunner, ref string) (probe *GitDivergenceProbe, refExists bool, err error) {
	if ref == "" {
		ref = defaultBranch
	}
	if _, fetchErr := run("fetch", "origin", ref); fetchErr != nil {
		if gitRefNotFoundPattern.MatchString(fetchErr.Error()) {
			return &GitDivergenceProbe{}, false, nil
		}
		return nil, false, fmt.Errorf("git fetch origin %s: %w", ref, fetchErr)
	}

	remoteAheadOut, raErr := run("rev-list", "--count", "HEAD..FETCH_HEAD")
	if raErr != nil {
		return nil, true, fmt.Errorf("git rev-list --count HEAD..FETCH_HEAD: %w", raErr)
	}
	localAheadOut, laErr := run("rev-list", "--count", "FETCH_HEAD..HEAD")
	if laErr != nil {
		return nil, true, fmt.Errorf("git rev-list --count FETCH_HEAD..HEAD: %w", laErr)
	}
	remoteAhead, _ := strconv.Atoi(strings.TrimSpace(remoteAheadOut))
	localAhead, _ := strconv.Atoi(strings.TrimSpace(localAheadOut))

	// merge-base exits non-zero when HEAD and FETCH_HEAD share no common
	// ancestor — that IS the unrelated-histories signal, not a probe
	// failure, so its error is intentionally not propagated.
	_, mergeErr := run("merge-base", "HEAD", "FETCH_HEAD")

	return &GitDivergenceProbe{
		RemoteAhead: remoteAhead,
		LocalAhead:  localAhead,
		Unrelated:   mergeErr != nil,
	}, true, nil
}

// RemoteRefState is the git-push-setup probe-time classification of a
// tracked ref's relationship to local HEAD — named from the REMOTE's
// perspective (mirrors the divergence counts): "ahead" means the remote
// carries commits local lacks (a push would be rejected), "behind" means
// local carries commits the remote lacks (a normal push fast-forwards
// cleanly).
type RemoteRefState string

const (
	// RemoteRefEmpty: the tracked ref does not exist on the remote yet —
	// the first push, always safe.
	RemoteRefEmpty RemoteRefState = "empty"
	// RemoteRefInSync: remote and local HEAD are identical.
	RemoteRefInSync RemoteRefState = "in-sync"
	// RemoteRefAhead: the remote carries commits local HEAD lacks — a
	// push would be rejected non-fast-forward.
	RemoteRefAhead RemoteRefState = "ahead"
	// RemoteRefBehind: local HEAD carries commits the remote lacks and
	// the remote carries none local lacks — a normal push fast-forwards.
	RemoteRefBehind RemoteRefState = "behind"
	// RemoteRefDiverged: both sides carry commits the other lacks.
	RemoteRefDiverged RemoteRefState = "diverged"
	// RemoteRefUnrelated: HEAD and the remote ref share no common
	// ancestor.
	RemoteRefUnrelated RemoteRefState = "unrelated"
)

// ClassifyRemoteRefState maps a divergence probe's raw counts to the
// canonical RemoteRefState. refExists=false always yields RemoteRefEmpty
// regardless of the other arguments (there is nothing to diverge from).
func ClassifyRemoteRefState(refExists bool, remoteAhead, localAhead int, unrelated bool) RemoteRefState {
	switch {
	case !refExists:
		return RemoteRefEmpty
	case unrelated:
		return RemoteRefUnrelated
	case remoteAhead > 0 && localAhead > 0:
		return RemoteRefDiverged
	case remoteAhead > 0:
		return RemoteRefAhead
	case localAhead > 0:
		return RemoteRefBehind
	default:
		return RemoteRefInSync
	}
}

// NeedsPushDecision reports whether a RemoteRefState carries non-fast-
// forward risk that the agent must resolve BEFORE the first push — the
// git-push-setup probe-time early warning fires on exactly these three
// states (§ spec-workflows.md §4.4).
func (s RemoteRefState) NeedsPushDecision() bool {
	return s == RemoteRefAhead || s == RemoteRefDiverged || s == RemoteRefUnrelated
}

// BuildGitDivergenceStepCommand builds one step of the divergence probe
// (fetch / rev-list / merge-base) as a shell command runnable over SSH at
// workingDir. Only the `fetch` step authenticates — via the session-env
// credential helper, parity with BuildGitPushCommand — every other git
// subcommand here is a pure local graph read needing no network/auth.
func BuildGitDivergenceStepCommand(workingDir string, args []string) string {
	quotedArgs := make([]string, len(args))
	for i, a := range args {
		quotedArgs[i] = shellQuote(a)
	}
	joined := strings.Join(quotedArgs, " ")
	if len(args) > 0 && args[0] == "fetch" {
		return fmt.Sprintf("cd %s && GIT_TERMINAL_PROMPT=0 git %s %s", shellQuote(workingDir), gitCredentialHelperArgs(), joined)
	}
	return fmt.Sprintf("cd %s && git %s", shellQuote(workingDir), joined)
}
