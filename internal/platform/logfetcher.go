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

// mapSeverityToNumeric converts severity label to syslog numeric value.
// The log backend uses syslog severity: 0=Emergency .. 7=Debug.
// minimumSeverity=N means "show messages with severity <= N".
func mapSeverityToNumeric(severity string) string {
	switch strings.ToLower(severity) {
	case "emergency":
		return "0"
	case "alert":
		return "1"
	case "critical":
		return "2"
	case "error":
		return "3"
	case "warning":
		return "4"
	case "notice":
		return "5"
	case "info", "informational":
		return "6"
	case "debug":
		return "7"
	default:
		return "6" // default to informational
	}
}

// mapFacilityToNumeric converts facility label to syslog numeric value that
// the Zerops log backend expects. Empty or unknown values return "" so the
// caller can omit the query param entirely — that corresponds to "no filter".
func mapFacilityToNumeric(facility string) string {
	switch strings.ToLower(facility) {
	case "application":
		return "16"
	case "webserver":
		return "17"
	default:
		return ""
	}
}

const (
	maxLogResponseBytes   = 50 << 20 // 50 MB
	maxErrorResponseBytes = 1 << 20  // 1 MB

	// LogLimitMin, LogLimitMax, LogLimitDefault are the clamp bounds for
	// LogFetchParams.Limit. Matches zcli conventions and the empirical
	// backend behaviour probed on 2026-04-23 — `limit>=50000` silently
	// returns zero items. Exported so the mock and tests can share.
	LogLimitMin     = 1
	LogLimitMax     = 1000
	LogLimitDefault = 100
)

// clampLimit returns the effective Limit to send and post-trim by.
func clampLimit(requested int) int {
	if requested <= 0 {
		return LogLimitDefault
	}
	if requested > LogLimitMax {
		return LogLimitMax
	}
	return requested
}

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

	effectiveLimit := clampLimit(params.Limit)

	q := logURL.Query()
	if params.ServiceID != "" {
		q.Set("serviceStackId", params.ServiceID)
	}
	q.Set("limit", fmt.Sprintf("%d", effectiveLimit))
	q.Set("desc", "1")
	if params.Severity != "" && params.Severity != "all" {
		q.Set("minimumSeverity", mapSeverityToNumeric(params.Severity))
	}
	if fac := mapFacilityToNumeric(params.Facility); fac != "" {
		q.Set("facility", fac)
	}
	if len(params.Tags) > 0 {
		q.Set("tags", strings.Join(params.Tags, ","))
	}
	if params.ContainerID != "" {
		q.Set("containerId", params.ContainerID)
	}
	// params.Search is kept out of the query string — the Zerops log backend
	// silently ignores `search=` (verified 2026-04-23 via live probes). We
	// apply it client-side in filterEntries below instead.
	logURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, logURL.String(), nil)
	if err != nil {
		return nil, NewPlatformError(ErrAPIError, fmt.Sprintf("failed to create log request: %v", err), "")
	}
	req.Header.Set("Content-Type", "application/json")

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

	return filterEntries(entries, params, effectiveLimit), nil
}

// filterEntries applies the shared post-fetch pipeline that both the real
// fetcher and the MockLogFetcher use. Order:
//  1. Sort ascending by Timestamp (string sort is fine for relative ordering
//     within a single backend response — the precise compare below handles
//     the cross-response case).
//  2. Drop entries whose timestamp is before params.Since (parsed compare —
//     string compare is wrong at sub-second boundaries, see
//     internal/platform/logfetcher_build_contract_test.go).
//  3. Drop entries whose Message does not contain params.Search (the backend
//     silently ignores `search=` — we apply client-side).
//  4. Tail-trim to effectiveLimit to return the newest N.
func filterEntries(entries []LogEntry, params LogFetchParams, effectiveLimit int) []LogEntry {
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp < entries[j].Timestamp
	})

	if !params.Since.IsZero() {
		filtered := entries[:0]
		for _, e := range entries {
			et, err := time.Parse(time.RFC3339, e.Timestamp)
			if err != nil {
				// Malformed timestamp — drop it. Forward-compatible default.
				continue
			}
			if !et.Before(params.Since) {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	if params.Search != "" {
		filtered := entries[:0]
		for _, e := range entries {
			if strings.Contains(e.Message, params.Search) {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	if effectiveLimit > 0 && len(entries) > effectiveLimit {
		entries = entries[len(entries)-effectiveLimit:]
	}

	return entries
}

// logAPIResponse matches the Zerops log backend JSON structure.
type logAPIResponse struct {
	Items []logAPIItem `json:"items"`
}

type logAPIItem struct {
	ID            string `json:"id"`
	Timestamp     string `json:"timestamp"`
	Hostname      string `json:"hostname"`
	ContainerID   string `json:"containerId"`
	Message       string `json:"message"`
	SeverityLabel string `json:"severityLabel"`
	FacilityLabel string `json:"facilityLabel"`
	Tag           string `json:"tag"`
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
			ID:          item.ID,
			Timestamp:   item.Timestamp,
			Severity:    item.SeverityLabel,
			Facility:    item.FacilityLabel,
			Tag:         item.Tag,
			Message:     item.Message,
			Container:   item.Hostname,
			ContainerID: item.ContainerID,
		})
	}

	return entries, nil
}
