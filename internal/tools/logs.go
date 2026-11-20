package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// LogsInput is the input type for zerops_logs.
type LogsInput struct {
	ServiceHostname string `json:"serviceHostname"    jsonschema:"Hostname of the service to fetch logs from."`
	Facility        string `json:"facility,omitempty" jsonschema:"application (default), webserver, or build: the newest build's log, also while it runs."`
	Severity        string `json:"severity,omitempty" jsonschema:"Filter by log severity: WARNING or ERROR. Omit for all severities."`
	Since           string `json:"since,omitempty"    jsonschema:"Fetch logs since this time. RFC3339 format (e.g. 2024-01-15T10:00:00Z) or relative duration (e.g. 30s, 5m, 1h, 7d). Omitted = last 1 hour ONLY — widen this to see older entries."`
	Limit           int    `json:"limit,omitempty"    jsonschema:"Maximum number of log entries to return. Default: 100."`
	Search          string `json:"search,omitempty"   jsonschema:"Full-text search filter applied to log messages."`
}

// logsTextCap bounds the entries the model reads, summed as marshalled JSON:
// limit reaches platform.LogLimitMax and a line can be long, so a result ran
// past the client's output cap.
const logsTextCap = 20000

// RegisterLogs registers the zerops_logs tool.
func RegisterLogs(srv *mcp.Server, client platform.Client, fetcher platform.LogFetcher, projectID string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "zerops_logs",
		Description: "Fetch a service's logs: its running app's (default), its web server's, or its newest build's (facility=\"build\") — a build's log while the build still runs too. Filter by severity, time range, and search text. " +
			fmt.Sprintf("Results are capped at %d bytes, newest kept.", logsTextCap),
		Annotations: &mcp.ToolAnnotations{
			Title:          "Fetch service logs",
			ReadOnlyHint:   true,
			IdempotentHint: true,
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input LogsInput) (*mcp.CallToolResult, any, error) {
		result, err := ops.FetchLogs(ctx, client, fetcher, projectID,
			input.ServiceHostname, input.Facility, input.Severity, input.Since, input.Limit, input.Search)
		if err != nil {
			return convertError(err), nil, nil
		}
		capLogs(result)
		return jsonResult(result), nil, nil
	})
}

// capLogs keeps the newest entries (Entries are oldest first) whose summed
// marshalled bytes fit logsTextCap, dropping older ones first. When the
// newest entry alone is over the cap, its message is cut to fit. A cut sets
// HasMore and Cut, which names the filters that reach the dropped lines: the
// tool has no until, so a larger limit or a wider since returns the same cut.
func capLogs(result *ops.LogsResult) {
	read := len(result.Entries)
	kept, total := 0, 0
	for i := read - 1; i >= 0; i-- {
		n := marshalledLen(result.Entries[i])
		if total+n > logsTextCap {
			break
		}
		total += n
		kept++
	}
	if kept == read {
		return
	}
	what := fmt.Sprintf("kept the newest %d of the %d entries read", kept, read)
	if kept == 0 {
		newest := cutLogEntry(result.Entries[read-1])
		result.Entries = []ops.LogEntryOutput{newest}
		what = fmt.Sprintf("kept only the newest of the %d entries read, its message cut,", read)
	} else {
		result.Entries = result.Entries[read-kept:]
	}
	result.HasMore = true
	result.Cut = fmt.Sprintf("%s to stay under %d bytes. A larger limit or a wider since returns the same cut; "+
		"search, severity or a later since narrows it to reach the dropped lines.", what, logsTextCap)
}

// cutLogEntry cuts the entry's message on a UTF-8 boundary so the marshalled
// entry, ending "… (cut)", fits logsTextCap. JSON escapes make the marshalled
// size of a prefix unpredictable from its length, so it searches for the
// longest prefix that fits.
func cutLogEntry(entry ops.LogEntryOutput) ops.LogEntryOutput {
	const note = "… (cut)"
	msg := entry.Message
	runeStart := func(end int) int {
		for end > 0 && end < len(msg) && !utf8.RuneStart(msg[end]) {
			end--
		}
		return end
	}
	fits := func(end int) bool {
		entry.Message = msg[:end] + note
		return marshalledLen(entry) <= logsTextCap
	}
	lo, hi := 0, len(msg)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if fits(runeStart(mid)) {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	entry.Message = msg[:runeStart(lo)] + note
	return entry
}

// marshalledLen is the entry's size as jsonResult writes it.
func marshalledLen(entry ops.LogEntryOutput) int {
	b, err := json.Marshal(entry)
	if err != nil {
		return 0
	}
	return len(b)
}
