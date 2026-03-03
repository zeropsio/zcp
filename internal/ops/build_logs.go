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

	logAccess, err := client.GetProjectLog(ctx, projectID)
	if err != nil {
		return nil
	}

	entries, err := fetcher.FetchLogs(ctx, logAccess, platform.LogFetchParams{
		ServiceID: *event.Build.ServiceStackID,
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
