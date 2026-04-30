package ops

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// EventsResult contains the merged activity timeline.
type EventsResult struct {
	ProjectID string          `json:"projectId"`
	Events    []TimelineEvent `json:"events"`
	Summary   EventsSummary   `json:"summary"`
}

// EventsSummary contains event counts.
type EventsSummary struct {
	Total     int `json:"total"`
	Processes int `json:"processes"`
	Deploys   int `json:"deploys"`
}

// TimelineEvent represents a single event in the activity timeline.
//
// FailureClass + FailureCause carry the structured classification for
// failed appVersion events (BUILD_FAILED / DEPLOY_FAILED /
// PREPARING_RUNTIME_FAILED). They mirror DeployResult.FailureClassification
// from the synchronous deploy path so async webhook/actions builds get
// the same diagnostic vocabulary the agent already knows from default
// `zerops_deploy` (zcli) failures. Empty when the event is non-failure
// or a LogFetcher was not provided. C3 closure (audit 2026-04-29 +
// round-2 follow-up).
type TimelineEvent struct {
	Timestamp    string `json:"timestamp"`
	Type         string `json:"type"`
	Action       string `json:"action"`
	Status       string `json:"status"`
	Service      string `json:"service"`
	Detail       string `json:"detail,omitempty"`
	Duration     string `json:"duration,omitempty"`
	User         string `json:"user,omitempty"`
	ProcessID    string `json:"processId,omitempty"`
	FailReason   string `json:"failReason,omitempty"`
	FailureClass string `json:"failureClass,omitempty"`
	FailureCause string `json:"failureCause,omitempty"`
	Hint         string `json:"hint,omitempty"`
}

// Event type constants.
const (
	eventTypeProcess = "process"
	eventTypeDeploy  = "deploy"
	eventTypeBuild   = "build"
)

// actionNameMap normalizes Zerops API action names to human-readable forms.
// API returns "stack.*" format (verified 2026-03-23 against live Zerops API).
var actionNameMap = map[string]string{
	"stack.start":                  "start",
	"stack.stop":                   "stop",
	"stack.restart":                "restart",
	"stack.autoscaling":            "scale",
	"stack.updateAutoscaling":      "scale",
	"stack.import":                 "import",
	"stack.delete":                 "delete",
	"stack.build":                  "build",
	"stack.userDataFile":           "env-update",
	"stack.enableSubdomainAccess":  "subdomain-enable",
	"stack.disableSubdomainAccess": "subdomain-disable",
}

// processHintMap maps process statuses to interpretation hints.
var processHintMap = map[string]string{
	statusFinished: "COMPLETE: Process finished successfully.",
	statusFailed:   "FAILED: Process failed.",
	"RUNNING":      "IN_PROGRESS: Process still running.",
	"PENDING":      "IN_PROGRESS: Process queued.",
}

// internalActionPrefixes are action name prefixes for internal platform operations
// that should be excluded from user-facing timelines.
var internalActionPrefixes = []string{
	"zL7Master.",
}

// isInternalAction returns true for platform-internal actions not relevant to users.
func isInternalAction(actionName string) bool {
	for _, prefix := range internalActionPrefixes {
		if strings.HasPrefix(actionName, prefix) {
			return true
		}
	}
	return false
}

// appVersionHintMap maps app version statuses to interpretation hints.
var appVersionHintMap = map[string]string{
	statusActive:               "DEPLOYED: App version is deployed and running. Build pipeline complete. No further polling needed.",
	statusBuilding:             "IN_PROGRESS: Build is running. Continue polling.",
	statusBuildFailed:          "FAILED: Build failed. Read this event's `failureClass` + `failureCause` for the structured diagnosis (populated when LogFetcher is available — same shape as DeployResult.FailureClassification). Tail `zerops_logs serviceHostname={service} facility=application since=5m` for full build-container output. Don't re-call zerops_deploy until the cause is identified — re-running without a fix loops the failure.",
	"DEPLOYING":                "IN_PROGRESS: Deploy is running. Continue polling.",
	"PREPARING_RUNTIME_FAILED": "FAILED: run.prepareCommands exited non-zero. Check buildLogs for stderr. Common causes: missing sudo prefix (containers run as zerops user), wrong package name (Alpine PHP: php84-<ext>).",
	"DEPLOY_FAILED":            "FAILED: run.initCommands crashed the new container on startup (build succeeded). The deploy response 'error' field identifies the failing command. Fetch runtime stderr with zerops_logs serviceHostname={service} severity=ERROR since=5m — NOT buildLogs (that's build container output).",
	statusCanceled:             "CANCELED: Build was canceled.",
}

// statusHint returns an interpretation hint for the given status and hint map.
// Lookup is case-insensitive.
func statusHint(status string, hints map[string]string) string {
	upper := strings.ToUpper(status)
	return hints[upper]
}

const defaultEventsLimit = 50

// Events fetches and merges the project activity timeline.
// Parallel fetch of processes, app versions, and services. When fetcher
// is non-nil, failed appVersion events are enriched with structured
// FailureClass + FailureCause via the deploy-failure classifier — same
// classification taxonomy the synchronous deploy path produces, so the
// agent gets a unified diagnostic vocabulary across sync and async
// build paths. fetcher=nil keeps the call lean (no log round-trip), used
// by tests and any callsite that only needs metadata.
func Events(
	ctx context.Context,
	client platform.Client,
	fetcher platform.LogFetcher,
	projectID string,
	serviceHostname string,
	limit int,
) (*EventsResult, error) {
	if limit <= 0 {
		limit = defaultEventsLimit
	}

	var (
		processes     []platform.ProcessEvent
		appVersions   []platform.AppVersionEvent
		services      []platform.ServiceStack
		processErr    error
		appVersionErr error
		serviceErr    error
	)

	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		processes, processErr = client.SearchProcesses(ctx, projectID, limit)
	}()
	go func() {
		defer wg.Done()
		appVersions, appVersionErr = client.SearchAppVersions(ctx, projectID, limit)
	}()
	go func() {
		defer wg.Done()
		services, serviceErr = client.ListServices(ctx, projectID)
	}()

	wg.Wait()

	if processErr != nil {
		return nil, processErr
	}
	if appVersionErr != nil {
		return nil, appVersionErr
	}
	if serviceErr != nil {
		return nil, serviceErr
	}

	// Build serviceID -> hostname map.
	svcMap := make(map[string]string, len(services))
	for _, s := range services {
		svcMap[s.ID] = s.Name
	}

	var events []TimelineEvent

	// Map process events (skip internal platform actions).
	for _, p := range processes {
		if isInternalAction(p.ActionName) {
			continue
		}

		svcName := ""
		if len(p.ServiceStacks) > 0 {
			svcName = svcMap[p.ServiceStacks[0].ID]
			if svcName == "" {
				svcName = p.ServiceStacks[0].Name
			}
		}

		action := mapActionName(p.ActionName)
		user := ""
		if p.CreatedByUser != nil {
			user = p.CreatedByUser.FullName
		}

		failReason := ""
		if p.FailReason != nil {
			failReason = *p.FailReason
		}

		events = append(events, TimelineEvent{
			Timestamp:  p.Created,
			Type:       eventTypeProcess,
			Action:     action,
			Status:     p.Status,
			Service:    svcName,
			Duration:   calcDuration(p.Started, p.Finished),
			User:       user,
			ProcessID:  p.ID,
			FailReason: failReason,
			Hint:       statusHint(p.Status, processHintMap),
		})
	}

	// Map app version events.
	for i := range appVersions {
		av := appVersions[i]
		svcName := svcMap[av.ServiceStackID]
		if svcName == "" {
			svcName = av.ServiceStackID
		}
		eventType := eventTypeDeploy
		if av.Build != nil && av.Build.PipelineStart != nil {
			eventType = eventTypeBuild
		}

		te := TimelineEvent{
			Timestamp: av.Created,
			Type:      eventType,
			Action:    eventType,
			Status:    av.Status,
			Service:   svcName,
			Hint:      statusHint(av.Status, appVersionHintMap),
		}

		// C3 closure: classify failed async builds. The deploy-failure
		// classifier already handles the synchronous deploy path
		// (deploy_poll.go); wiring it here gives webhook/actions builds
		// the same `failureClass` + `failureCause` shape the agent
		// already knows from sync deploys, so atoms can read one field
		// regardless of whether the build was ZCP-driven or external.
		// Skipped when fetcher is nil (test/mock callsites that only
		// need metadata) or when status isn't a failure.
		if fetcher != nil {
			if phase := FailurePhaseFromStatus(av.Status); phase != "" {
				logs := FetchBuildLogs(ctx, client, fetcher, projectID, &av, 200)
				if cls := ClassifyDeployFailure(FailureInput{
					Phase:     phase,
					Status:    av.Status,
					BuildLogs: logs,
				}); cls != nil {
					te.FailureClass = string(cls.Category)
					te.FailureCause = cls.LikelyCause
				}
			}
		}

		events = append(events, te)
	}

	// Filter by service if specified.
	if serviceHostname != "" {
		filtered := make([]TimelineEvent, 0, len(events))
		for _, e := range events {
			if e.Service == serviceHostname {
				filtered = append(filtered, e)
			}
		}
		events = filtered
	}

	// Sort by timestamp descending.
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp > events[j].Timestamp
	})

	// Trim to limit.
	if len(events) > limit {
		events = events[:limit]
	}

	// Compute counts from filtered+trimmed events.
	processCount := 0
	deployCount := 0
	for _, e := range events {
		switch e.Type {
		case eventTypeProcess:
			processCount++
		case eventTypeDeploy, eventTypeBuild:
			deployCount++
		}
	}

	return &EventsResult{
		ProjectID: projectID,
		Events:    events,
		Summary: EventsSummary{
			Total:     len(events),
			Processes: processCount,
			Deploys:   deployCount,
		},
	}, nil
}

// mapActionName normalizes a Zerops action name.
func mapActionName(name string) string {
	if mapped, ok := actionNameMap[name]; ok {
		return mapped
	}
	return name
}

// calcDuration calculates human-readable duration between two RFC3339 timestamps.
// Accepts both RFC3339 and RFC3339Nano formats.
func calcDuration(started, finished *string) string {
	if started == nil || finished == nil {
		return ""
	}
	s, err := parseTimestamp(*started)
	if err != nil {
		return ""
	}
	f, err := parseTimestamp(*finished)
	if err != nil {
		return ""
	}
	d := f.Sub(s)
	if d < 0 {
		return ""
	}
	return formatDuration(d)
}

// parseTimestamp parses a timestamp in RFC3339 or RFC3339Nano format.
func parseTimestamp(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
	}
	return t, err
}

// formatDuration returns a human-readable duration string.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		if s > 0 {
			return fmt.Sprintf("%dm%ds", m, s)
		}
		return fmt.Sprintf("%dm", m)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if m > 0 {
		return fmt.Sprintf("%dh%dm", h, m)
	}
	return fmt.Sprintf("%dh", h)
}
