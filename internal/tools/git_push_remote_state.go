package tools

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
)

// gitPushRemoteStateWire is the git-push-setup success result's `remote`
// block (docs/spec-workflows.md §4.4): the tracked ref's divergence state,
// computed the same way as the GIT_PUSH_NON_FAST_FORWARD classification
// (ops.ProbeGitDivergence) so the agent sees non-fast-forward risk BEFORE
// the first push, not only after a rejected one.
type gitPushRemoteStateWire struct {
	Ref   string `json:"ref"`
	State string `json:"state"`
}

// probeGitPushRemoteState computes the `remote` block for a git-push-setup
// success response, against ref (the GF-7 trackedRef this same confirm
// call just resolved/stamped — see resolveContainerTrackedRef /
// localGitBranchDetector). Best-effort: a probe transport error (fetch
// fails for a reason other than "ref absent") returns nil rather than
// failing the otherwise-successful setup call — the wiring itself already
// succeeded (origin synced, token/credentials proven); this is additional
// advisory state.
func probeGitPushRemoteState(run ops.GitRunner, ref string) *gitPushRemoteStateWire {
	if ref == "" {
		ref = defaultTrackedRef
	}
	probe, refExists, err := ops.ProbeGitDivergence(run, ref)
	if err != nil {
		return nil
	}
	var remoteAhead, localAhead int
	var unrelated bool
	if probe != nil {
		remoteAhead, localAhead, unrelated = probe.RemoteAhead, probe.LocalAhead, probe.Unrelated
	}
	state := ops.ClassifyRemoteRefState(refExists, remoteAhead, localAhead, unrelated)
	return &gitPushRemoteStateWire{Ref: ref, State: string(state)}
}

// gitPushRemoteStateWarning names the three GIT_PUSH_NON_FAST_FORWARD
// options up front when the probe-time state already carries non-fast-
// forward risk (ahead/diverged/unrelated) — the agent resolves it BEFORE
// the first push. zcp still never runs any of the three itself.
func gitPushRemoteStateWarning(s *gitPushRemoteStateWire) string {
	if s == nil || !ops.RemoteRefState(s.State).NeedsPushDecision() {
		return ""
	}
	opts := buildGitPushRejectionOptions(s.Ref, 0)
	names := make([]string, 0, len(opts))
	for _, o := range opts {
		names = append(names, fmt.Sprintf("%s (%s)", o.Name, o.Command))
	}
	return fmt.Sprintf(
		"The tracked ref %q is already %q relative to this checkout — the FIRST push would be rejected non-fast-forward unless you resolve it first. %s Three options, none run automatically: %s.",
		s.Ref, s.State, gitPushRejectionMessage, strings.Join(names, "; "),
	)
}
