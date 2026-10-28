package ops

import (
	"context"
	"fmt"

	"github.com/zeropsio/zcp/internal/platform"
)

// buildLogSearchLimit is how many of the project's newest app versions the
// activity search is asked for, to find the build facts of the one read.
const buildLogSearchLimit = 50

// BuildLogSource names the build a zerops_logs facility=build read came from.
type BuildLogSource struct {
	AppVersionID string `json:"appVersionId"`
	Status       string `json:"status"`
	// Running is a build still on its way (uploading, building, preparing,
	// deploying): a later read shows its newer lines.
	Running bool `json:"running,omitempty"`
}

// fetchServiceBuildLog is zerops_logs facility=build: the log of svc's newest
// build, a running one included, read the way a deploy reads its failed
// build's log (fetchBuildLog — the build's zbuilder tag, the runtime prepare
// phase logged under it, and the build container's alerts). since does not
// apply: a build's log belongs to its build. search and limit do, over the
// newest lines.
func fetchServiceBuildLog(
	ctx context.Context,
	client platform.Client,
	fetcher platform.LogFetcher,
	projectID string,
	svc *platform.ServiceStack,
	severity string,
	limit int,
	search string,
) (*LogsResult, error) {
	event, found, err := newestServiceBuild(ctx, client, projectID, svc.ID)
	if err != nil {
		return nil, err
	}
	if !found {
		return &LogsResult{
			Entries:       []LogEntryOutput{},
			ServiceStatus: svc.Status,
			EmptyReason:   "no build of this service exists yet — nothing has been deployed to it, so there is no build log.",
		}, nil
	}

	query := limit + 1
	if search != "" {
		query = platform.LogLimitMax
	}
	entries, _, err := fetchBuildLog(ctx, client, fetcher, projectID, &event, severity, query)
	if err != nil {
		return nil, err
	}
	if search != "" {
		entries = platform.SearchLogs(entries, search)
	}
	hasMore := len(entries) > limit
	if hasMore {
		entries = entries[len(entries)-limit:]
	}

	result := &LogsResult{
		Entries: make([]LogEntryOutput, len(entries)),
		HasMore: hasMore,
		Build: &BuildLogSource{
			AppVersionID: event.ID,
			Status:       event.Status,
			Running:      isBuildInProgress(event.Status),
		},
	}
	for i, e := range entries {
		result.Entries[i] = LogEntryOutput{Timestamp: e.Timestamp, Severity: e.Severity, Message: e.Message, Container: e.Container}
	}
	if len(result.Entries) == 0 {
		result.ServiceStatus = svc.Status
		result.EmptyReason = fmt.Sprintf("build %s (%s) has logged nothing that matched yet.", event.ID, event.Status)
		if result.Build.Running {
			result.EmptyReason += " It is still on its way — an upload or a queued build logs nothing; read again shortly."
		}
	}
	return result, nil
}

// newestServiceBuild is the service's newest app version that came from a
// build — not the empty one a service started without code holds. Picked from
// the direct app-version list, which no search lag hides (docs/spec-
// workflows.md §3.5); its build facts come from the activity search once it
// has indexed the build, and without them the build's own lines are still
// read by its tag.
func newestServiceBuild(ctx context.Context, client platform.Client, projectID, serviceID string) (platform.AppVersionEvent, bool, error) {
	versions, err := client.ListServiceAppVersions(ctx, serviceID)
	if err != nil {
		return platform.AppVersionEvent{}, false, fmt.Errorf("list app versions: %w", err)
	}
	var newest *platform.AppVersionEvent
	for i := range versions {
		// The direct list carries no build facts, so this is its source alone.
		if !isStartWithoutCodeEvent(&versions[i]) {
			newest = &versions[i]
			break
		}
	}
	if newest == nil {
		return platform.AppVersionEvent{}, false, nil
	}
	event := *newest
	event.Build = &platform.BuildInfo{}
	if indexed, err := client.SearchAppVersions(ctx, projectID, buildLogSearchLimit); err == nil {
		for _, av := range indexed {
			if av.ID == event.ID && av.Build != nil {
				event.Build = av.Build
				break
			}
		}
	}
	return event, true, nil
}
