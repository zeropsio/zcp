package ops

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// maxRollbackCandidates caps how many app-version ids ReactivateAppVersion
// lists back to the caller on a not-found refusal — enough to orient an
// agent without dumping a service's whole history. AppVersionCandidates
// itself is uncapped — the cap only applies to the inline error text.
const maxRollbackCandidates = 5

// AppVersionCandidate is one row of a service's app-version history — the
// SAME shape both ReactivateAppVersion's "does not belong" refusal
// (rollbackCandidateList) and zerops_events' per-service `appVersions`
// section (docs/spec-workflows.md §8 R2 / §12.6 GF-8) render from. The two
// are the candidate sources an agent reads BEFORE calling
// `zerops_deploy appVersion=<id>` — never by probing with a fake id to
// harvest the list from the refusal error.
type AppVersionCandidate struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Sequence int    `json:"sequence"`
	Created  string `json:"created,omitempty"`
	// Active marks the CURRENTLY active appVersion (platform.ServiceStatusActive).
	Active bool `json:"active,omitempty"`
	// Backup marks a rollback-eligible appVersion (platform.BuildStatusBackup)
	// — the only status ReactivateAppVersion accepts as a target.
	Backup bool `json:"backup,omitempty"`
}

// AppVersionCandidates reads serviceID's app-version history via the
// DIRECT (non-Elasticsearch) ListServiceAppVersions endpoint — the same
// lag-free source ReactivateAppVersion already used for its "does not
// belong" refusal, now exported so zerops_events and the status envelope
// can surface the identical rows without an agent needing to probe
// zerops_deploy with a fake appVersion id to harvest them from an error
// message. Unlike the ES-backed SearchAppVersions this package's Events()
// otherwise reads (CLAUDE.md's search-lag note), this DIRECT endpoint is
// NOT ES-backed and does not carry that lag — but a version created in
// the last moments of an in-flight build/deploy can still be a beat
// behind other platform state settling; a caller reading this right
// after a mutation should re-query rather than treat a short list as
// final.
//
// Newest first (Sequence descending, ListServiceAppVersions' own ordering).
func AppVersionCandidates(ctx context.Context, client platform.Client, serviceID string) ([]AppVersionCandidate, error) {
	versions, err := client.ListServiceAppVersions(ctx, serviceID)
	if err != nil {
		return nil, fmt.Errorf("app version candidates: %w", err)
	}
	out := make([]AppVersionCandidate, 0, len(versions))
	for _, v := range versions {
		out = append(out, AppVersionCandidate{
			ID:       v.ID,
			Status:   v.Status,
			Sequence: v.Sequence,
			Created:  v.Created,
			Active:   v.Status == platform.ServiceStatusActive,
			Backup:   v.Status == platform.BuildStatusBackup,
		})
	}
	return out, nil
}

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

	candidates, err := AppVersionCandidates(ctx, client, svc.ID)
	if err != nil {
		return nil, fmt.Errorf("reactivate app version: %w", err)
	}

	var target *AppVersionCandidate
	for i := range candidates {
		if candidates[i].ID == appVersionID {
			target = &candidates[i]
			break
		}
	}
	if target == nil {
		return nil, platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("appVersion %s does not belong to %s. %s", appVersionID, hostname, rollbackCandidateList(hostname, candidates)),
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
			fmt.Sprintf("appVersion %s status is %s, want %s. %s", appVersionID, target.Status, platform.BuildStatusBackup, rollbackCandidateList(hostname, candidates)),
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
// choose from, newest first (AppVersionCandidates' own ordering), capped
// at maxRollbackCandidates, and points at zerops_events for the full,
// uncapped list — the source an agent should read BEFORE calling
// zerops_deploy, not a probe with a fake id.
func rollbackCandidateList(hostname string, candidates []AppVersionCandidate) string {
	if len(candidates) == 0 {
		return fmt.Sprintf("no app-version history for %s", hostname)
	}
	n := min(len(candidates), maxRollbackCandidates)
	parts := make([]string, 0, n)
	for _, c := range candidates[:n] {
		parts = append(parts, fmt.Sprintf("%s (%s, seq %d)", c.ID, c.Status, c.Sequence))
	}
	return fmt.Sprintf("Candidates (newest first): %s. Full history: zerops_events serviceHostname=%s.", strings.Join(parts, "; "), hostname)
}
