package ops

import (
	"context"
	"fmt"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

const defaultLogLimit = 100

// The logs zerops_logs reads (its facility).
const (
	// LogFacilityApplication is the running app's log, the default.
	LogFacilityApplication = "application"
	// LogFacilityWebserver is the web server's log (php and static runtimes).
	LogFacilityWebserver = "webserver"
	// LogFacilityBuild is the log of the service's newest build, a running
	// one included (fetchServiceBuildLog).
	LogFacilityBuild = "build"
)

// LogsResult contains the result of a log fetch operation.
type LogsResult struct {
	Entries []LogEntryOutput `json:"entries"`
	HasMore bool             `json:"hasMore"`
	// Build names the build a facility=build read came from.
	Build *BuildLogSource `json:"build,omitempty"`
	// WindowStart is the timestamp of the oldest line a search read, set only
	// when the read filled the backend's newest-lines window inside since:
	// older lines, if any, were not searched, so HasMore is true.
	WindowStart string `json:"windowStart,omitempty"`
	// The three fields below are populated ONLY when Entries is empty, from
	// state FetchLogs already holds — so a 0-entry result explains WHY it is
	// empty and points at the surface that does carry the answer, instead of a
	// bare {entries:[],hasMore:false} the agent gropes at with random filters
	// (B7). ServiceStatus is the live status; EmptyReason is its plain-English
	// reading; Recovery points at zerops_events for never-started/failed
	// services whose diagnosis is in the event timeline, not the log stream.
	ServiceStatus string             `json:"serviceStatus,omitempty"`
	EmptyReason   string             `json:"emptyReason,omitempty"`
	Recovery      *topology.Recovery `json:"recovery,omitempty"`
}

// LogEntryOutput is a single log entry in the response.
type LogEntryOutput struct {
	Timestamp string `json:"timestamp"`
	Severity  string `json:"severity"`
	Message   string `json:"message"`
	Container string `json:"container,omitempty"`
}

// FetchLogs retrieves a service's logs: facility application (the default,
// "" too) or webserver reads that log stream; build reads the service's
// newest build's log (fetchServiceBuildLog), where since does not apply.
func FetchLogs(
	ctx context.Context,
	client platform.Client,
	fetcher platform.LogFetcher,
	projectID string,
	hostname string,
	facility string,
	severity string,
	since string,
	limit int,
	search string,
) (*LogsResult, error) {
	if limit <= 0 {
		limit = defaultLogLimit
	}
	switch facility {
	case "":
		facility = LogFacilityApplication
	case LogFacilityApplication, LogFacilityWebserver, LogFacilityBuild:
	default:
		return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("facility %q is not a log zerops_logs reads", facility),
			fmt.Sprintf("Use facility=%q (the running app's log, the default), %q (the web server's) or %q (the service's newest build, while it runs too).",
				LogFacilityApplication, LogFacilityWebserver, LogFacilityBuild))
	}

	sinceTime, err := parseSince(since)
	if err != nil {
		return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("Invalid since value: %v", err),
			"Use formats like 30s, 5m, 1h, 7d, or ISO 8601 (RFC3339)")
	}

	svc, err := resolveService(ctx, client, projectID, hostname)
	if err != nil {
		return nil, err
	}
	if facility == LogFacilityBuild {
		return fetchServiceBuildLog(ctx, client, fetcher, projectID, svc, severity, limit, search)
	}

	logAccess, err := client.GetProjectLog(ctx, projectID)
	if err != nil {
		return nil, err
	}

	// Fetch limit+1 to detect if more entries exist beyond the requested limit.
	// The facility is the app log stream unless the caller asked for the web
	// server's — never daemon noise (sshfs, systemd).
	params := platform.LogFetchParams{
		ServiceID: svc.ID,
		Severity:  severity,
		Facility:  facility,
		Since:     sinceTime,
		Limit:     limit + 1,
	}
	if search != "" {
		// The backend serves only the newest lines, so a search reads the
		// widest window it serves and matches here, where a full window shows
		// that the read ended inside since.
		params.Limit = platform.LogLimitMax
	}
	entries, err := fetcher.FetchLogs(ctx, logAccess, params)
	if err != nil {
		return nil, err
	}

	windowStart := ""
	if search != "" {
		if len(entries) == platform.LogLimitMax {
			windowStart = entries[0].Timestamp
		}
		entries = platform.SearchLogs(entries, search)
	}

	// Entries are oldest first; keep the newest limit.
	hasMore := len(entries) > limit || windowStart != ""
	if len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}

	result := &LogsResult{
		Entries:     make([]LogEntryOutput, len(entries)),
		HasMore:     hasMore,
		WindowStart: windowStart,
	}
	for i, e := range entries {
		result.Entries[i] = LogEntryOutput{
			Timestamp: e.Timestamp,
			Severity:  e.Severity,
			Message:   e.Message,
			Container: e.Container,
		}
	}

	if len(result.Entries) == 0 {
		enrichEmptyLogs(ctx, client, projectID, hostname, svc.Status, since, result)
	}

	return result, nil
}

// enrichEmptyLogs explains an empty log result from the service status (already
// in hand) so the agent doesn't blind-grope. A never-started or failed service
// has NO runtime log stream — its diagnosis lives in zerops_events (the
// build/process failure timeline), so those branches point Recovery there.
func enrichEmptyLogs(
	ctx context.Context,
	client platform.Client,
	projectID, hostname, status, sinceInput string,
	result *LogsResult,
) {
	result.ServiceStatus = status
	eventsRecovery := func() *topology.Recovery {
		return &topology.Recovery{Tool: "zerops_events", Action: "fetch", Args: map[string]string{"serviceHostname": hostname}}
	}
	switch status {
	case platform.ServiceStatusNew, "CREATING", "STARTING":
		result.EmptyReason = "service is still provisioning — no container exists yet, so no logs can exist. Re-check once it reaches RUNNING/ACTIVE."
	case platform.ServiceStatusReadyToDeploy:
		// READY_TO_DEPLOY with a prior (real) deploy attempt = a build/deploy
		// that never produced a running process → events, not logs. Without a
		// prior attempt = nothing deployed yet.
		hadPrior, err := HasPriorDeployAttempt(ctx, client, projectID, hostname)
		if err == nil && hadPrior {
			result.EmptyReason = "no runtime logs exist — the service never started an app process. The build/deploy failure timeline (failureClass + cause) is in zerops_events, not this stream."
			result.Recovery = eventsRecovery()
		} else {
			result.EmptyReason = "nothing has been deployed yet — no process has ever run, so no logs exist. Deploy first, then read logs."
		}
	case platform.ServiceStatusFailed:
		result.EmptyReason = "service is FAILED — read the failure timeline (failureClass + cause) in zerops_events."
		result.Recovery = eventsRecovery()
	case platform.ServiceStatusStopped:
		result.EmptyReason = "service is stopped — no new logs are produced; entries from before the stop may sit outside the effective window. Widen since."
	default:
		// ACTIVE / RUNNING and anything else live: the stream exists but the
		// filters matched nothing. Name them, including the silent 1h default.
		window := sinceInput
		if window == "" {
			window = "last 1h (default — since was omitted)"
		}
		if result.WindowStart != "" {
			result.EmptyReason = fmt.Sprintf("the search read the newest %d lines, back to %s, and none matched; older lines inside since=%s, if any, were not searched.", platform.LogLimitMax, result.WindowStart, window)
			return
		}
		result.EmptyReason = fmt.Sprintf("the service is live but no entries matched the applied filters (since=%s). Widen since or drop severity/search.", window)
	}
}
