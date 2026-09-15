package tools

import (
	"fmt"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// GitPushRejectionOption is one of the three named choices offered on a
// GIT_PUSH_NON_FAST_FORWARD refusal. zcp classifies the rejection; it never
// executes any of these on the user's behalf (docs/spec-workflows.md §12
// GF-11).
type GitPushRejectionOption struct {
	// Name is one of "rebase", "merge", "replace-remote".
	Name        string `json:"name"`
	Description string `json:"description"`
	// Command is the exact command the agent (or user) would run to take
	// this option — zcp never runs it itself.
	Command string `json:"command"`
}

// GitPushRejectionPayload is the structured GIT_PUSH_NON_FAST_FORWARD error
// payload: the divergence between local HEAD and the tracked remote ref,
// plus the exactly-three named recovery options. Attached to the wire
// response via WithGitPushRejection.
type GitPushRejectionPayload struct {
	RemoteURL   string                   `json:"remoteUrl"`
	Ref         string                   `json:"ref"`
	RemoteAhead int                      `json:"remoteAhead"`
	LocalAhead  int                      `json:"localAhead"`
	Unrelated   bool                     `json:"unrelated"`
	Next        []GitPushRejectionOption `json:"next"`
}

// gitPushRejectionMessage is the suggestion text on every
// GIT_PUSH_NON_FAST_FORWARD error — stated once so it can never drift
// between the container and local paths. Names the actor explicitly: this
// is a user decision, never zcp's to make.
const gitPushRejectionMessage = "zcp will not choose for you and will never force-push or merge on your behalf. Read `next` for the three named options (rebase / merge / replace-remote) and their exact commands; surface the choice to the user (replace-remote discards the remote's ahead commits and needs their explicit say-so), then run the one they pick."

// buildGitPushRejectionOptions builds the exactly-three named options a
// GIT_PUSH_NON_FAST_FORWARD refusal offers, none of which zcp executes.
func buildGitPushRejectionOptions(ref string, remoteAhead int) []GitPushRejectionOption {
	return []GitPushRejectionOption{
		{
			Name:        "rebase",
			Description: "Replay your local commits on top of the remote's history, then re-push. Rewrites your local commit SHAs.",
			Command:     fmt.Sprintf("git pull --rebase origin %s", ref),
		},
		{
			Name:        "merge",
			Description: "Merge the remote's commits into your local branch with a merge commit, then re-push.",
			Command:     fmt.Sprintf("git pull --no-rebase origin %s", ref),
		},
		{
			Name: "replace-remote",
			Description: fmt.Sprintf(
				"Discard the remote's %d commit(s) and replace the ref with your local HEAD. Destructive to the remote's history — requires the user's explicit say-so before running.",
				remoteAhead,
			),
			Command: fmt.Sprintf("git push --force-with-lease origin %s", ref),
		},
	}
}

// classifyGitPushNonFastForward builds the GIT_PUSH_NON_FAST_FORWARD
// payload for a rejected push whose output matched
// ops.IsNonFastForwardRejection. run probes the remote (container: SSH via
// sshGitRunner; local: exec.Command via localGitRunner) to compute
// divergence. Best-effort: a probe failure (rare — the push being
// classified already proved the remote reachable) falls back to a payload
// with remoteAhead=localAhead=0 rather than blocking the error response.
func classifyGitPushNonFastForward(run ops.GitRunner, remoteURL, ref string) *GitPushRejectionPayload {
	payload := &GitPushRejectionPayload{
		RemoteURL: topology.CanonicalRepoURL(remoteURL),
		Ref:       ref,
	}
	if probe, refExists, err := ops.ProbeGitDivergence(run, ref); err == nil && refExists && probe != nil {
		payload.RemoteAhead = probe.RemoteAhead
		payload.LocalAhead = probe.LocalAhead
		payload.Unrelated = probe.Unrelated
	}
	payload.Next = buildGitPushRejectionOptions(ref, payload.RemoteAhead)
	return payload
}

// newGitPushNonFastForwardError builds the GIT_PUSH_NON_FAST_FORWARD
// PlatformError. detail carries the real git stderr (non-fast-forward /
// "fetch first" / "Updates were rejected") so the agent sees the exact
// rejection text, not just the structured payload.
func newGitPushNonFastForwardError(hostname, detail string) *platform.PlatformError {
	return platform.NewPlatformError(
		platform.ErrGitPushNonFastForward,
		fmt.Sprintf("git-push from %s was rejected as non-fast-forward: %s", hostname, detail),
		gitPushRejectionMessage,
	)
}
