package ops

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// maxRollbackCandidates caps how many app-version ids ReactivateAppVersion
// lists back to the caller on a not-found refusal — enough to orient an
// agent without dumping a service's whole history.
const maxRollbackCandidates = 5

// ReactivateAppVersion re-activates a SPECIFIC recorded appVersion of
// hostname via PUT /app-version/{id}/deploy (docs/spec-workflows.md §8 R2
// generalised, §12.6 GF-8) — the rollback path. Unlike
// RedeployLastAppVersion (which only ever targets the newest appVersion,
// for the R2 never-activated-service recovery), this accepts any id the
// caller names, letting an agent roll a service back to an OLDER
// artifact.
//
// Only a BACKUP appVersion (the status a previously-active version flips
// to once a newer one activates, live-verified 2026-09-14) is accepted:
//
//   - the id doesn't belong to hostname's app-version history ⇒ refuse,
//     listing the candidate ids/statuses/sequence (newest first, capped
//     at maxRollbackCandidates) instead of guessing;
//   - the id is the CURRENTLY active appVersion ⇒ refuse "already active,
//     nothing to do" (the platform would 400 appVersionInvalidStatus
//     anyway);
//   - any other status (WAITING_TO_BUILD, DEPLOY_FAILED, ...) ⇒ refuse
//     naming the status — none of those name a built, re-activatable
//     artifact the way BACKUP does.
//
// No platform call happens on any refusal.
//
// The request body is IGNORED by the platform for a BACKUP target
// (live-verified: null / {} / wrong setup all flip cleanly), so this
// sends an empty zerops.yaml + setup rather than resolving/resending real
// content the way RedeployLastAppVersion must for its DEPLOY_FAILED case.
func ReactivateAppVersion(
	ctx context.Context,
	client platform.Client,
	projectID, hostname, appVersionID string,
) (*DeployResult, error) {
	svc, err := LookupService(ctx, client, projectID, hostname)
	if err != nil {
		return nil, fmt.Errorf("reactivate app version: %w", err)
	}

	versions, err := client.ListServiceAppVersions(ctx, svc.ID)
	if err != nil {
		return nil, fmt.Errorf("reactivate app version: %w", err)
	}

	var target *platform.AppVersionEvent
	for i := range versions {
		if versions[i].ID == appVersionID {
			target = &versions[i]
			break
		}
	}
	if target == nil {
		return nil, platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("appVersion %s does not belong to %s. %s", appVersionID, hostname, rollbackCandidateList(versions)),
			"Pass one of the listed appVersion ids to zerops_deploy.",
		)
	}

	switch target.Status {
	case platform.ServiceStatusActive:
		return nil, platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("appVersion %s is already active, nothing to do", appVersionID),
			"",
		)
	case platform.BuildStatusBackup:
		// proceed
	default:
		return nil, platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("appVersion %s status is %s, want %s. %s", appVersionID, target.Status, platform.BuildStatusBackup, rollbackCandidateList(versions)),
			"Pass one of the listed appVersion ids to zerops_deploy.",
		)
	}

	proc, err := client.RedeployAppVersion(ctx, appVersionID, "", "")
	if err != nil {
		return nil, fmt.Errorf("reactivate app version: %w", err)
	}

	result := &DeployResult{
		TargetService:   hostname,
		TargetServiceID: svc.ID,
		AppVersionID:    appVersionID,
	}

	final, err := PollProcess(ctx, client, proc.ID, nil)
	if err != nil {
		result.TimedOut = true
		return result, nil //nolint:nilerr // timeout/cancel is reported on the result, not as an error
	}

	if final.Status == platform.ProcessStatusFinished {
		result.Status = platform.BuildStatusDeployed
		result.Message = fmt.Sprintf("re-activated appVersion %s on %s (stack.deploy.backup, no build)", appVersionID, hostname)
		return result, nil
	}

	result.Status = platform.BuildStatusDeployFailed
	result.FailedPhase = "init"
	result.FailureClassification = ClassifyDeployFailure(FailureInput{
		Phase:  PhaseInit,
		Status: platform.BuildStatusDeployFailed,
	})
	if result.FailureClassification != nil && result.FailureClassification.SuggestedAction != "" {
		result.Suggestion = result.FailureClassification.SuggestedAction
	}
	return result, nil
}

// rollbackCandidateList renders the ids/statuses/sequence a caller can
// choose from, newest first (ListServiceAppVersions' own ordering),
// capped at maxRollbackCandidates.
func rollbackCandidateList(versions []platform.AppVersionEvent) string {
	if len(versions) == 0 {
		return "no app-version history"
	}
	n := min(len(versions), maxRollbackCandidates)
	parts := make([]string, 0, n)
	for _, v := range versions[:n] {
		parts = append(parts, fmt.Sprintf("%s (%s, seq %d)", v.ID, v.Status, v.Sequence))
	}
	return "Candidates (newest first): " + strings.Join(parts, "; ")
}
