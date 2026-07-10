// Package tools — launch-token staging (single-token launch lifecycle,
// plans/archive/launch-single-token-lifecycle-2026-06-11.md).
//
// The protocol: the launch-window token is resolved ONCE per mutation —
// either the user's integration token enters the conversation exactly
// once (the launchKey-bearing mutation call), or, on the delegated path
// (plans/token-delegation-implementation-spec-2026-07-10.md), it enters
// zero times because ZCP mints it itself from a one-time platform
// delegation. Either way the mutation immediately stages the resolved
// token as a SERVICE-scope SECRET (ops.LaunchTokenEnvKey =
// ZCP_LAUNCH_TOKEN) on the source push service — staged strictly BEFORE
// the irreversible project create, so a staging failure aborts with
// nothing to clean up. From then on every launch-window operation
// (prod-ops, pipeline resume, reset, confirm-production) resolves the
// token from the staged secret instead of re-asking; the GitHub Actions
// repo-secret conveyance reads the same env over ssh. confirm-production
// deletes the env — the window closes physically, not by policy.
package tools

import (
	"context"
	"fmt"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// stageLaunchToken writes the launch-window token as a service-scope
// SECRET on the source push service (pair-keyed dev half — the same
// hostname the prodCD conveyance and the later staged reads use).
// Upsert semantics come from ops.EnvSetSecretService (delete existing,
// recreate), so a retried mutation re-stages idempotently.
//
// P-LP-1 holds: the value flows platform-ward only; the returned error
// never echoes it (EnvSetSecretService error paths name the key, not
// the value).
func stageLaunchToken(ctx context.Context, client platform.Client, projectID, pushHostname, token string) error {
	svc, err := ops.LookupService(ctx, client, projectID, pushHostname)
	if err != nil {
		return fmt.Errorf("locate push-source service %q: %w", pushHostname, err)
	}
	if _, err := ops.EnvSetSecretService(ctx, client, svc.ID, ops.LaunchTokenEnvKey, token); err != nil {
		return fmt.Errorf("write %s secret on %q: %w", ops.LaunchTokenEnvKey, pushHostname, err)
	}
	return nil
}

// launchKeyFromStage resolves the launch-window token from the staged
// service secret — the T2 read every launch-window operation (prod-ops,
// pipeline resume, reset, confirm-production) uses when the call
// carries no explicit launchKey. Reads the source project's env store
// through the platform API (works in container AND local mode, and
// even when the dev container is stopped — recovery paths need both),
// returns "" when the stage location or the key is absent (window
// closed, never staged, or service gone).
//
// The returned value is IN-REQUEST ONLY: callers hand it to the admin
// client factory and drop it — never logged, persisted, or echoed
// (P-LP-1 extension, pinned by the staged-token sentinel tests).
func launchKeyFromStage(ctx context.Context, client platform.Client, projectID string, state *launchState) (string, error) {
	if client == nil || state == nil || state.TargetServiceHostname == "" {
		return "", nil
	}
	svc, err := ops.LookupService(ctx, client, projectID, state.TargetServiceHostname)
	if err != nil {
		return "", fmt.Errorf("locate stage service %q: %w", state.TargetServiceHostname, err)
	}
	envs, err := ops.FetchServiceEnv(ctx, client, svc.ID)
	if err != nil {
		return "", fmt.Errorf("read staged secret on %q: %w", state.TargetServiceHostname, err)
	}
	for _, e := range envs {
		if e.Key == ops.LaunchTokenEnvKey {
			return e.Content, nil
		}
	}
	return "", nil
}

// resolveLaunchWindowToken resolves the launch-window token for a window
// operation (prod-ops, pipeline resume, reset, confirm-production). Per the
// single-token lifecycle (P-LP-14) the staged ZCP_LAUNCH_TOKEN secret is THE
// working copy, so it is preferred (spec §10.2 / P-LP-14: "the staged secret
// first, explicit launchKey as fallback"); an explicit launchKey is accepted
// ONLY when the stage is gone (window closed, staging never ran). A stage-READ
// error is returned distinctly (non-nil error) so a transient read failure
// surfaces as "retry the read" instead of masquerading as "token absent" and
// pushing the agent to re-ask the user for the value (Codex #1).
func resolveLaunchWindowToken(ctx context.Context, client platform.Client, projectID string, state *launchState, explicit string) (string, error) {
	staged, err := launchKeyFromStage(ctx, client, projectID, state)
	if err != nil {
		// Stage read failed. An explicit launchKey is a deliberate user
		// override — honor it rather than failing. Surface the read error
		// only when there is no fallback, so the caller can say "retry the
		// read" instead of "token absent".
		if explicit != "" {
			return explicit, nil
		}
		return "", err
	}
	if staged != "" {
		return staged, nil
	}
	return explicit, nil
}

// launchTokenStageFailedMessage is the shared abort message when
// staging fails. mintedName is empty on every existing-project call site
// and on the new-project explicit-launchKey path (D-5): the message
// there is the byte-for-byte original — no project was created, no
// state persisted, the same launchKey can be re-supplied once the cause
// is fixed. mintedName is non-empty ONLY on the new-project delegated
// path (token-delegation spec §4.4 outcome-table row 3): the one-time
// delegation was already consumed minting that token, so this uses the
// shared D-7 consumed-delegation narrative instead — and phrases the
// staging failure as "not confirmed" rather than "failed", since the
// write may have already committed before the error returned.
func launchTokenStageFailedMessage(stageErr error, pushHostname, mintedName string) string {
	if mintedName == "" {
		return fmt.Sprintf(
			"Staging the launch token as a %s service secret on %q failed: %v. "+
				"Nothing was created — the staged secret is the single working copy every later launch-window call reads, so the launch refuses to proceed without it. "+
				"Fix the cause (service reachable? env write permitted?) and re-call with the same launchKey.",
			ops.LaunchTokenEnvKey, pushHostname, stageErr)
	}
	return delegationConsumedNarrative(mintedName, fmt.Sprintf(
		"staging it as the %s service secret on %q was not confirmed (the write may have already committed before the error): %v",
		ops.LaunchTokenEnvKey, pushHostname, stageErr))
}
