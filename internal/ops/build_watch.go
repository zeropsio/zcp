package ops

import (
	"context"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// Build-watch tuning (spec-git-delivery-target §6.1). Initialized package
// vars so tests can narrow them; production values cover the integration
// latency envelope: a platform webhook fires in seconds, a GitHub Actions
// runner adds ~1–3 min of spin-up + zcli before the platform even sees
// the build.
//
//nolint:gochecknoglobals // tuning knobs, initialized; test-narrowed
var (
	// BuildWatchDiscoverBudget bounds the wait for the integration-
	// triggered build to APPEAR on the build target.
	BuildWatchDiscoverBudget = 4 * time.Minute
	// BuildWatchFollowBudget bounds the observed build's run to terminal.
	BuildWatchFollowBudget = 10 * time.Minute
	// BuildWatchPollInterval spaces the SearchAppVersions polls.
	BuildWatchPollInterval = 5 * time.Second
)

// IntegrationBuildResult is the outcome of WatchIntegrationBuild.
type IntegrationBuildResult struct {
	// Observed: an integration build for the target appeared at-or-after
	// the push. False = nothing fired within the discovery budget
	// (integration missing/broken/slow) — the push itself still landed.
	Observed bool
	// Event is the LATEST read of the observed build (terminal when
	// TimedOut=false). Nil when !Observed.
	Event *platform.AppVersionEvent
	// TimedOut: the build was observed but did not reach a terminal
	// status within the follow budget.
	TimedOut bool
}

// buildTerminalStatuses mirror the platform appVersion lifecycle terminals
// the deploy poll path already keys on.
func buildStatusTerminal(status string) bool {
	switch status {
	case "ACTIVE", "FAILED", "CANCELED", "CANCELLED":
		return true
	}
	return false
}

// WatchIntegrationBuild discovers the integration-triggered build on the
// build target (a new appVersion created at-or-after pushedAt) and follows
// it to a terminal status — the L1 monitoring primitive
// (spec-git-delivery-target §6.1): in repo delivery the PUSH is the
// deploy, so it gets the same synchronous treatment ZCP's own deploys
// have (the old PUSHED-and-walk-away left watching to agent memory).
//
// Timestamp comparison is parse-compare, never lexicographic (the
// logfetcher invariant — RFC3339 fractional precision varies).
//
// onProgress (nilable) is invoked between polls only — every return path
// runs BEFORE the next progress emit, per the no-progress-before-response
// transport invariant.
func WatchIntegrationBuild(
	ctx context.Context,
	client platform.Client,
	projectID, serviceID string,
	pushedAt time.Time,
	onProgress ProgressCallback,
) (IntegrationBuildResult, error) {
	res := IntegrationBuildResult{}
	discoverDeadline := time.Now().Add(BuildWatchDiscoverBudget)
	followDeadline := time.Time{}

	for {
		events, err := client.SearchAppVersions(ctx, projectID, 30)
		if err != nil {
			return res, err
		}
		if !res.Observed {
			if ev := newestBuildSince(events, serviceID, pushedAt); ev != nil {
				res.Observed = true
				res.Event = ev
				followDeadline = time.Now().Add(BuildWatchFollowBudget)
			}
		} else if ev := refreshEvent(events, res.Event.ID); ev != nil {
			res.Event = ev
		}

		if res.Observed && buildStatusTerminal(res.Event.Status) {
			return res, nil
		}
		now := time.Now()
		if !res.Observed && now.After(discoverDeadline) {
			return res, nil
		}
		if res.Observed && now.After(followDeadline) {
			res.TimedOut = true
			return res, nil
		}

		// Progress AFTER every return path above (transport invariant).
		if onProgress != nil {
			if res.Observed {
				onProgress("integration build "+res.Event.Status+" on "+serviceID, 0, 0)
			} else {
				onProgress("waiting for the integration build to appear on "+serviceID, 0, 0)
			}
		}
		select {
		case <-ctx.Done():
			return res, ctx.Err()
		case <-time.After(BuildWatchPollInterval):
		}
	}
}

// newestBuildSince returns the newest appVersion event for serviceID whose
// Created is at-or-after pushedAt (parse-compare).
func newestBuildSince(events []platform.AppVersionEvent, serviceID string, pushedAt time.Time) *platform.AppVersionEvent {
	var newest *platform.AppVersionEvent
	var newestAt time.Time
	for i := range events {
		ev := &events[i]
		if ev.ServiceStackID != serviceID {
			continue
		}
		created, err := time.Parse(time.RFC3339Nano, ev.Created)
		if err != nil {
			if created, err = time.Parse(time.RFC3339, ev.Created); err != nil {
				continue
			}
		}
		if created.Before(pushedAt) {
			continue
		}
		if newest == nil || created.After(newestAt) {
			newest = ev
			newestAt = created
		}
	}
	return newest
}

// refreshEvent re-reads the tracked build from a fresh search page.
func refreshEvent(events []platform.AppVersionEvent, id string) *platform.AppVersionEvent {
	for i := range events {
		if events[i].ID == id {
			return &events[i]
		}
	}
	return nil
}
