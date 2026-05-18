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
// stalled, queued) point at the import override (replace-and-redeploy is
// the recovery).
//
//	READY_TO_DEPLOY + HasPriorDeployAttempt → zerops_import
//	    override=true startWithoutCode=true (the agent's first call hits
//	    the Phase 3.2 confirmDestructive gate, gets a structured loss
//	    payload back, then re-calls with the acknowledgment)
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
			return &topology.Recovery{
				Tool:   "zerops_import",
				Action: "import",
				Args: map[string]string{
					"override":         "true",
					"startWithoutCode": "true",
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
