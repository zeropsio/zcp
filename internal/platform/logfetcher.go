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
		return nil, NewPlatformError(ErrAPIError,
			"log access is nil",
			"Refresh log credentials via the project endpoint and retry; the access token may have expired.")
	}

	rawURL := access.URL
	rawURL = strings.TrimPrefix(rawURL, "GET ")
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		rawURL = "https://" + rawURL
	}

	logURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, NewPlatformError(ErrAPIError,
			fmt.Sprintf("invalid log URL: %v", err),
			"The log endpoint returned by the project API is malformed; refresh log access (the project's log token) and retry.")
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
		return nil, NewPlatformError(ErrAPIError,
			fmt.Sprintf("failed to create log request: %v", err),
			"Internal request construction failed — usually a malformed URL field. Retry; if persistent, this is a ZCP bug.")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := f.httpClient.Do(req)
	if err != nil {
		code, isNet := MapNetworkError(err)
		if isNet {
			return nil, NewPlatformError(code,
				fmt.Sprintf("log backend unreachable: %v", err),
				"Check VPN / network connectivity and DNS resolution of the log host. The log subdomain is project-scoped — re-fetch project log access if the URL stale.")
		}
		// Reaching here means the HTTP roundtrip failed but it doesn't
		// match any of the network signatures MapNetworkError knows. Tag
		// it as API-side rather than network — the request shape is at
		// fault (TLS rejection, request body issue, server hangup) and
		// retrying without changing inputs won't help.
		return nil, NewPlatformError(ErrAPIError,
			fmt.Sprintf("log fetch failed: %v", err),
			"The log backend rejected the HTTP request. Verify project log access is current and retry; if persistent, capture the error and report.")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorResponseBytes))
		return nil, NewPlatformError(ErrAPIError,
			fmt.Sprintf("log backend returned HTTP %d: %s", resp.StatusCode, string(respBody)),
			"The log backend responded with a non-200 status. 401/403 means refresh project log access; 5xx means transient — retry. Capture the body for the report.")
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxLogResponseBytes))
	if err != nil {
		return nil, NewPlatformError(ErrAPIError,
			fmt.Sprintf("failed to read log response: %v", err),
			"Network glitch mid-stream; retry. If reproducible, the response may exceed maxLogResponseBytes — narrow the time window with from=/to=.")
	}

	entries, err := parseLogResponse(bodyBytes)
	if err != nil {
		return nil, NewPlatformError(ErrAPIError,
			fmt.Sprintf("failed to parse log response: %v", err),
			"The log backend returned a payload ZCP couldn't decode. Likely an API contract drift — capture the response body for the report.")
	}

	return filterEntries(entries, params, effectiveLimit), nil
}

// filterEntries applies the shared post-fetch pipeline that both the real
// fetcher and the MockLogFetcher use. Order:
//  1. Sort ascending by parsed timestamp. RFC3339 fractional precision
//     varies (the backend strips trailing zeros, 3–9 digits), so a
//     lexicographic compare misorders entries at the '.' vs 'Z' boundary
//     within a same-second cluster — and the tail-trim in step 4 would then
//     drop the wrong "newest". Parse-compare is the invariant (CLAUDE.md
//     "Log time comparison is parse-compare, never lexicographic"; the
//     failure is demonstrated by logfetcher_build_contract_test.go).
//  2. Drop entries whose timestamp is before params.Since (and malformed
//     timestamps, forward-compatibly) when Since is set.
//  3. Drop entries whose Message does not contain params.Search (the backend
//     silently ignores `search=` — we apply client-side).
//  4. Tail-trim to effectiveLimit to return the newest N.
func filterEntries(entries []LogEntry, params LogFetchParams, effectiveLimit int) []LogEntry {
	// Parse each timestamp ONCE into a decorated slice, then sort on the
	// parsed instant. Malformed timestamps parse to the zero time (sort
	// oldest) and are dropped by the Since filter when it runs.
	type tsEntry struct {
		e  LogEntry
		t  time.Time
		ok bool
	}
	decorated := make([]tsEntry, len(entries))
	for i, e := range entries {
		t, err := time.Parse(time.RFC3339, e.Timestamp)
		decorated[i] = tsEntry{e: e, t: t, ok: err == nil}
	}
	sort.SliceStable(decorated, func(i, j int) bool {
		if decorated[i].t.Equal(decorated[j].t) {
			// Deterministic tie-break for identical instants.
			return decorated[i].e.Timestamp < decorated[j].e.Timestamp
		}
		return decorated[i].t.Before(decorated[j].t)
	})

	out := make([]LogEntry, 0, len(decorated))
	for _, d := range decorated {
		if !params.Since.IsZero() {
			// Drop malformed timestamps and anything strictly before Since.
			if !d.ok || d.t.Before(params.Since) {
				continue
			}
		}
		if params.Search != "" && !strings.Contains(d.e.Message, params.Search) {
			continue
		}
		out = append(out, d.e)
	}

	if effectiveLimit > 0 && len(out) > effectiveLimit {
		out = out[len(out)-effectiveLimit:]
	}

	return out
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
