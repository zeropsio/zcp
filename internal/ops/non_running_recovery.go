package ops

import (
	"context"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// NonRunningRecovery returns the canonical Recovery hint for a service in a
// non-running terminal state, or nil when the status is intentional / pre-
// deploy / healthy. The discriminator for READY_TO_DEPLOY is "service has
// any prior deploy attempt" — never-deployed services point at logs (rare
// case, no diagnostic data yet); services that DID try to deploy (failed,
// stalled, queued) point at the failure timeline (zerops_events) to diagnose
// READ-FIRST before any reset — never an auto-destructive override, which
// wiped the buildFromGit source under diagnosis (Wave-1 data-loss fix).
//
//	READY_TO_DEPLOY + HasPriorDeployAttempt → zerops_events (READ FIRST:
//	    diagnose the failed/stalled build before any reset — the prior
//	    destructive override hint here wiped buildFromGit code under
//	    diagnosis, Wave-1 data-loss bug; override is now an explicit gated
//	    choice via gateOverrideOnFailedHistory, not the auto-recovery)
//	READY_TO_DEPLOY + no prior deploy → zerops_logs (never-deployed)
//	FAILED → zerops_events (classified failure via events timeline)
//	STOPPED / NEW / RUNNING / ACTIVE → nil (intentional / pre-deploy /
//	    healthy — no recovery candidate)
//
// Phase 2.2 of plans/eval-review-20260518-subset/fix-plan.md broadened the
// discriminator from "LatestFailedAppVersionContext != nil" (which silently
// filtered out queued/stalled appVersions because FailurePhaseFromStatus
// returns "" for them — Karel's 2026-05-16 WAITING_TO_BUILD reproducer hit
// this) to "HasPriorDeployAttempt" (any non-startWithoutCode appVersion). The
// launch-production path got a parallel fix via 33fb9358; this brings the
// develop-adopt path to parity.
//
// fetcher is currently unused (discriminator no longer needs log enrichment)
// but retained in the signature so callers don't have to change.
func NonRunningRecovery(
	ctx context.Context,
	client platform.Client,
	_ platform.LogFetcher,
	projectID, hostname, status string,
) *topology.Recovery {
	switch status {
	case platform.ServiceStatusReadyToDeploy:
		hasPrior, _ := HasPriorDeployAttempt(ctx, client, projectID, hostname)
		if hasPrior {
			// A real prior deploy/build attempt (failed, stalled, or queued) →
			// READ FIRST. The previous hint here was a destructive
			// zerops_import override=true startWithoutCode=true, which WIPED
			// the buildFromGit code + env the agent needed to diagnose (Wave-1
			// data-loss bug: recover-failed — a failed build at READY_TO_DEPLOY
			// got reset, destroying the very source under diagnosis). Point at
			// the failure timeline instead; the agent diagnoses the cause, then
			// fixes non-destructively (e.g. add the missing managed dependency +
			// redeploy). Override stays available as an EXPLICIT, gated choice —
			// gateOverrideOnFailedHistory now fires on any prior attempt and
			// surfaces the wouldDestroy payload + requires confirmDestructive.
			return &topology.Recovery{
				Tool:   "zerops_events",
				Action: "fetch",
				Args: map[string]string{
					"serviceHostname": hostname,
				},
			}
		}
		return &topology.Recovery{
			Tool:   "zerops_logs",
			Action: "fetch",
			Args: map[string]string{
				"serviceHostname": hostname,
				"facility":        logFacilityApplication,
				"since":           "15m",
			},
		}
	case platform.ServiceStatusFailed:
		return &topology.Recovery{
			Tool:   "zerops_events",
			Action: "fetch",
			Args: map[string]string{
				"serviceHostname": hostname,
			},
		}
	}
	return nil
}
