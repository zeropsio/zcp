package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	maxLogResponseBytes   = 50 << 20 // 50 MB
	maxErrorResponseBytes = 1 << 20  // 1 MB
)

// ZeropsLogFetcher fetches logs from the Zerops log backend (separate HTTP service).
type ZeropsLogFetcher struct {
	httpClient *http.Client
}

// NewLogFetcher creates a LogFetcher with a default HTTP client.
func NewLogFetcher() *ZeropsLogFetcher {
	return &ZeropsLogFetcher{
		httpClient: &http.Client{
			Timeout: DefaultAPITimeout,
		},
	}
}

// Compile-time interface check.
var _ LogFetcher = (*ZeropsLogFetcher)(nil)

// FetchLogs retrieves log entries from the Zerops log backend.
func (f *ZeropsLogFetcher) FetchLogs(ctx context.Context, access *LogAccess, params LogFetchParams) ([]LogEntry, error) {
	if access == nil {
		return nil, NewPlatformError(ErrAPIError, "log access is nil", "")
	}

	rawURL := access.URL
	rawURL = strings.TrimPrefix(rawURL, "GET ")
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		rawURL = "https://" + rawURL
	}

	logURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, NewPlatformError(ErrAPIError, fmt.Sprintf("invalid log URL: %v", err), "")
	}

	q := logURL.Query()
	if params.ServiceID != "" {
		q.Set("serviceStackId", params.ServiceID)
	}
	if params.Limit > 0 {
		q.Set("tail", fmt.Sprintf("%d", params.Limit))
	} else {
		q.Set("tail", "100")
	}
	if !params.Since.IsZero() {
		q.Set("since", params.Since.Format(time.RFC3339))
	}
	if params.Severity != "" && params.Severity != "all" {
		q.Set("severity", params.Severity)
	}
	if params.Search != "" {
		q.Set("search", params.Search)
	}
	logURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, logURL.String(), nil)
	if err != nil {
		return nil, NewPlatformError(ErrAPIError, fmt.Sprintf("failed to create log request: %v", err), "")
	}
	req.Header.Set("Authorization", "Bearer "+access.AccessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := f.httpClient.Do(req)
	if err != nil {
		code, isNet := MapNetworkError(err)
		if isNet {
			return nil, NewPlatformError(code, fmt.Sprintf("log backend unreachable: %v", err), "Check network connectivity")
		}
		return nil, NewPlatformError(ErrAPIError, fmt.Sprintf("log fetch failed: %v", err), "")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorResponseBytes))
		return nil, NewPlatformError(ErrAPIError,
			fmt.Sprintf("log backend returned HTTP %d: %s", resp.StatusCode, string(respBody)), "")
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxLogResponseBytes))
	if err != nil {
		return nil, NewPlatformError(ErrAPIError, fmt.Sprintf("failed to read log response: %v", err), "")
	}

	entries, err := parseLogResponse(bodyBytes)
	if err != nil {
		return nil, NewPlatformError(ErrAPIError, fmt.Sprintf("failed to parse log response: %v", err), "")
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp < entries[j].Timestamp
	})

	if params.Limit > 0 && len(entries) > params.Limit {
		entries = entries[len(entries)-params.Limit:]
	}

	return entries, nil
}

// logAPIResponse matches the Zerops log backend JSON structure.
type logAPIResponse struct {
	Items []logAPIItem `json:"items"`
}

type logAPIItem struct {
	ID            string `json:"id"`
	Timestamp     string `json:"timestamp"`
	Hostname      string `json:"hostname"`
	Message       string `json:"message"`
	SeverityLabel string `json:"severityLabel"`
}

// parseLogResponse parses the JSON response from the log backend.
func parseLogResponse(data []byte) ([]LogEntry, error) {
	var resp logAPIResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	entries := make([]LogEntry, 0, len(resp.Items))
	for _, item := range resp.Items {
		entries = append(entries, LogEntry{
			ID:        item.ID,
			Timestamp: item.Timestamp,
			Severity:  item.SeverityLabel,
			Message:   item.Message,
			Container: item.Hostname,
		})
	}

	return entries, nil
}
