package ops

import (
	"context"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// buildTagForEvent returns the exact log tag the Zerops builder emits for a
// given app-version event. Every entry of the build carries this tag
// (verified 2026-04-23 against api.app-prg1.zerops.io), and so does the
// runtime prepare phase's output, which is logged on its own prepare service
// stack, not on the build stack (verified 2026-10-16 on a
// PREPARING_RUNTIME_FAILED appVersion). Scoping a log query by this tag, and
// not by a service stack, surfaces exactly this build's entries from both.
func buildTagForEvent(event *platform.AppVersionEvent) string {
	if event == nil || event.ID == "" {
		return ""
	}
	return "zbuilder@" + event.ID
}

// FetchBuildLogs fetches the last N lines of build pipeline output for the
// given app-version event, the runtime prepare phase's included. Scoped to
// this build via tag identity and the application facility so daemon noise is
// excluded.
// Best-effort: returns nil on any error.
func FetchBuildLogs(
	ctx context.Context,
	client platform.Client,
	fetcher platform.LogFetcher,
	projectID string,
	event *platform.AppVersionEvent,
	limit int,
) []string {
	if event == nil || event.Build == nil {
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
		Facility: "application",
		Tags:     []string{tag},
		Limit:    limit,
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
	if event == nil || event.Build == nil {
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
		Severity: "warning",
		Facility: "application",
		Tags:     []string{tag},
		Limit:    limit,
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
