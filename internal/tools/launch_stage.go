// Package tools — launch-token staging (single-token launch lifecycle,
// plans/launch-single-token-lifecycle-2026-06-11.md).
//
// The protocol: the user's integration token enters the conversation
// exactly ONCE (the launchKey-bearing mutation call). The mutation
// immediately stages it as a SERVICE-scope SECRET (ops.LaunchTokenEnvKey
// = ZEROPS_TOKEN_PROD) on the source push service — staged strictly
// BEFORE the irreversible project create, so a staging failure aborts
// with nothing to clean up. From then on every launch-window operation
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

// launchTokenStageFailedMessage is the shared abort message when
// staging fails: no project was created, no state persisted — the same
// launchKey can be re-supplied once the cause is fixed.
func launchTokenStageFailedMessage(stageErr error, pushHostname string) string {
	return fmt.Sprintf(
		"Staging the launch token as a %s service secret on %q failed: %v. "+
			"Nothing was created — the staged secret is the single working copy every later launch-window call reads, so the launch refuses to proceed without it. "+
			"Fix the cause (service reachable? env write permitted?) and re-call with the same launchKey.",
		ops.LaunchTokenEnvKey, pushHostname, stageErr)
}
