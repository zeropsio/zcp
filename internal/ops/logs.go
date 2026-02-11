package ops

import (
	"context"
	"fmt"

	"github.com/zeropsio/zcp/internal/platform"
)

const defaultLogLimit = 100

// LogsResult contains the result of a log fetch operation.
type LogsResult struct {
	Entries []LogEntryOutput `json:"entries"`
	HasMore bool             `json:"hasMore"`
}

// LogEntryOutput is a single log entry in the response.
type LogEntryOutput struct {
	Timestamp string `json:"timestamp"`
	Severity  string `json:"severity"`
	Message   string `json:"message"`
	Container string `json:"container,omitempty"`
}

// FetchLogs retrieves logs for a service.
func FetchLogs(
	ctx context.Context,
	client platform.Client,
	fetcher platform.LogFetcher,
	projectID string,
	hostname string,
	severity string,
	since string,
	limit int,
	search string,
) (*LogsResult, error) {
	if limit <= 0 {
		limit = defaultLogLimit
	}

	sinceTime, err := parseSince(since)
	if err != nil {
		return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("Invalid since value: %v", err),
			"Use formats like 30m, 1h, 7d, or ISO 8601 (RFC3339)")
	}

	svc, err := resolveService(ctx, client, projectID, hostname)
	if err != nil {
		return nil, err
	}

	logAccess, err := client.GetProjectLog(ctx, projectID)
	if err != nil {
		return nil, err
	}

	entries, err := fetcher.FetchLogs(ctx, logAccess, platform.LogFetchParams{
		ServiceID: svc.ID,
		Severity:  severity,
		Since:     sinceTime,
		Limit:     limit,
		Search:    search,
	})
	if err != nil {
		return nil, err
	}

	result := &LogsResult{
		Entries: make([]LogEntryOutput, len(entries)),
		HasMore: len(entries) >= limit,
	}
	for i, e := range entries {
		result.Entries[i] = LogEntryOutput{
			Timestamp: e.Timestamp,
			Severity:  e.Severity,
			Message:   e.Message,
			Container: e.Container,
		}
	}

	return result, nil
}
