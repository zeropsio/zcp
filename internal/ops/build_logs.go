package ops

import (
	"context"

	"github.com/zeropsio/zcp/internal/platform"
)

// FetchBuildLogs fetches the last N lines of build pipeline output.
// Best-effort: returns nil on any error (don't break the deploy result).
func FetchBuildLogs(
	ctx context.Context,
	client platform.Client,
	fetcher platform.LogFetcher,
	projectID string,
	event *platform.AppVersionEvent,
	limit int,
) []string {
	if event.Build == nil || event.Build.ServiceStackID == nil {
		return nil
	}
	return fetchServiceLogs(ctx, client, fetcher, projectID, *event.Build.ServiceStackID, limit)
}

// FetchBuildWarnings fetches only warning/error-level lines from the build pipeline.
// Used on successful (ACTIVE) deploys to surface build issues without full log noise.
// Server-side severity filter: "warning" = syslog ≤4 (emergency..warning).
// Best-effort: returns nil on any error.
func FetchBuildWarnings(
	ctx context.Context,
	client platform.Client,
	fetcher platform.LogFetcher,
	projectID string,
	event *platform.AppVersionEvent,
	limit int,
) []string {
	if event.Build == nil || event.Build.ServiceStackID == nil {
		return nil
	}
	logAccess, err := client.GetProjectLog(ctx, projectID)
	if err != nil {
		return nil
	}
	entries, err := fetcher.FetchLogs(ctx, logAccess, platform.LogFetchParams{
		ServiceID: *event.Build.ServiceStackID,
		Severity:  "warning",
		Limit:     limit,
	})
	if err != nil {
		return nil
	}
	messages := make([]string, len(entries))
	for i, e := range entries {
		messages[i] = e.Message
	}
	return messages
}

// FetchRuntimeLogs fetches the last N lines from the target runtime container.
// Used on DEPLOY_FAILED (initCommand crashed at container start) where the
// stderr lives in the runtime service logs, not the build container logs.
// Best-effort: returns nil on any error.
func FetchRuntimeLogs(
	ctx context.Context,
	client platform.Client,
	fetcher platform.LogFetcher,
	projectID string,
	serviceID string,
	limit int,
) []string {
	if serviceID == "" {
		return nil
	}
	return fetchServiceLogs(ctx, client, fetcher, projectID, serviceID, limit)
}

func fetchServiceLogs(
	ctx context.Context,
	client platform.Client,
	fetcher platform.LogFetcher,
	projectID string,
	serviceID string,
	limit int,
) []string {
	logAccess, err := client.GetProjectLog(ctx, projectID)
	if err != nil {
		return nil
	}
	entries, err := fetcher.FetchLogs(ctx, logAccess, platform.LogFetchParams{
		ServiceID: serviceID,
		Limit:     limit,
	})
	if err != nil {
		return nil
	}
	messages := make([]string, len(entries))
	for i, e := range entries {
		messages[i] = e.Message
	}
	return messages
}
