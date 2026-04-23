package ops

import (
	"context"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// buildTagForEvent returns the exact log tag the Zerops builder emits for a
// given app-version event. Every entry on the build service-stack carries
// this tag (verified 2026-04-23 against api.app-prg1.zerops.io).
// Scoping a log query by this tag is the canonical way to surface only the
// current build's entries, bypassing the stale-warning leak that would occur
// if we queried the persistent build service-stack without identity.
func buildTagForEvent(event *platform.AppVersionEvent) string {
	if event == nil || event.ID == "" {
		return ""
	}
	return "zbuilder@" + event.ID
}

// FetchBuildLogs fetches the last N lines of build pipeline output for the
// given app-version event. Scoped to this build via tag identity and the
// application facility so daemon noise is excluded.
// Best-effort: returns nil on any error.
func FetchBuildLogs(
	ctx context.Context,
	client platform.Client,
	fetcher platform.LogFetcher,
	projectID string,
	event *platform.AppVersionEvent,
	limit int,
) []string {
	if event == nil || event.Build == nil || event.Build.ServiceStackID == nil {
		return nil
	}
	tag := buildTagForEvent(event)
	if tag == "" {
		return nil
	}
	logAccess, err := client.GetProjectLog(ctx, projectID)
	if err != nil {
		return nil
	}
	entries, err := fetcher.FetchLogs(ctx, logAccess, platform.LogFetchParams{
		ServiceID: *event.Build.ServiceStackID,
		Facility:  "application",
		Tags:      []string{tag},
		Limit:     limit,
	})
	if err != nil {
		return nil
	}
	return messagesOf(entries)
}

// FetchBuildWarnings fetches only warning-and-above lines from the build
// pipeline, scoped to this build's tag identity. Used on successful (ACTIVE)
// deploys to surface build issues without full log noise. Previous-build
// warnings are physically excluded — they have a different tag.
// Best-effort: returns nil on any error.
func FetchBuildWarnings(
	ctx context.Context,
	client platform.Client,
	fetcher platform.LogFetcher,
	projectID string,
	event *platform.AppVersionEvent,
	limit int,
) []string {
	if event == nil || event.Build == nil || event.Build.ServiceStackID == nil {
		return nil
	}
	tag := buildTagForEvent(event)
	if tag == "" {
		return nil
	}
	logAccess, err := client.GetProjectLog(ctx, projectID)
	if err != nil {
		return nil
	}
	entries, err := fetcher.FetchLogs(ctx, logAccess, platform.LogFetchParams{
		ServiceID: *event.Build.ServiceStackID,
		Severity:  "warning",
		Facility:  "application",
		Tags:      []string{tag},
		Limit:     limit,
	})
	if err != nil {
		return nil
	}
	return messagesOf(entries)
}

// FetchRuntimeLogs fetches the last N lines from the target runtime container
// whose creation time is containerCreationStart. The Since anchor is essential
// because the runtime service-stack persists across deploys — without it,
// stale crash output from the previous container bleeds into this deploy's
// failure context. When containerCreationStart is zero (event mapper hasn't
// populated it), the filter is omitted and the caller receives unanchored
// logs — a best-effort fallback.
// Best-effort: returns nil on any error.
func FetchRuntimeLogs(
	ctx context.Context,
	client platform.Client,
	fetcher platform.LogFetcher,
	projectID string,
	serviceID string,
	containerCreationStart time.Time,
	limit int,
) []string {
	if serviceID == "" {
		return nil
	}
	logAccess, err := client.GetProjectLog(ctx, projectID)
	if err != nil {
		return nil
	}
	entries, err := fetcher.FetchLogs(ctx, logAccess, platform.LogFetchParams{
		ServiceID: serviceID,
		Facility:  "application",
		Since:     containerCreationStart,
		Limit:     limit,
	})
	if err != nil {
		return nil
	}
	return messagesOf(entries)
}

func messagesOf(entries []platform.LogEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Message
	}
	return out
}
