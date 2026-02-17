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
type TimelineEvent struct {
	Timestamp   string `json:"timestamp"`
	Type        string `json:"type"`
	Action      string `json:"action"`
	Status      string `json:"status"`
	Service     string `json:"service"`
	ServiceType string `json:"serviceType,omitempty"`
	Detail      string `json:"detail,omitempty"`
	Duration    string `json:"duration,omitempty"`
	User        string `json:"user,omitempty"`
	ProcessID   string `json:"processId,omitempty"`
	Hint        string `json:"hint,omitempty"`
}

// actionNameMap normalizes Zerops action names to human-readable forms.
var actionNameMap = map[string]string{
	"serviceStackStart":                  "start",
	"serviceStackStop":                   "stop",
	"serviceStackRestart":                "restart",
	"serviceStackAutoscaling":            "scale",
	"serviceStackImport":                 "import",
	"serviceStackDelete":                 "delete",
	"serviceStackUserDataFile":           "env-update",
	"serviceStackEnableSubdomainAccess":  "subdomain-enable",
	"serviceStackDisableSubdomainAccess": "subdomain-disable",
}

// processHintMap maps process statuses to interpretation hints.
var processHintMap = map[string]string{
	statusFinished: "COMPLETE: Process finished successfully.",
	"RUNNING":      "IN_PROGRESS: Process still running.",
	statusFailed:   "FAILED: Process failed.",
	"PENDING":      "IN_PROGRESS: Process queued.",
}

// appVersionHintMap maps app version statuses to interpretation hints.
var appVersionHintMap = map[string]string{
	statusActive:      "DEPLOYED: App version is deployed and running. Build pipeline complete. No further polling needed.",
	statusBuilding:    "IN_PROGRESS: Build is running. Continue polling.",
	statusBuildFailed: "FAILED: Build failed. Check build logs with zerops_logs severity=error.",
	"DEPLOYING":       "IN_PROGRESS: Deploy is running. Continue polling.",
}

// statusHint returns an interpretation hint for the given status and hint map.
// Lookup is case-insensitive.
func statusHint(status string, hints map[string]string) string {
	upper := strings.ToUpper(status)
	return hints[upper]
}

const defaultEventsLimit = 50

// Events fetches and merges the project activity timeline.
// Parallel fetch of processes, app versions, and services.
func Events(
	ctx context.Context,
	client platform.Client,
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
	processCount := 0
	deployCount := 0

	// Map process events.
	for _, p := range processes {
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

		events = append(events, TimelineEvent{
			Timestamp: p.Created,
			Type:      "process",
			Action:    action,
			Status:    p.Status,
			Service:   svcName,
			Duration:  calcDuration(p.Started, p.Finished),
			User:      user,
			ProcessID: p.ID,
			Hint:      statusHint(p.Status, processHintMap),
		})
		processCount++
	}

	// Map app version events.
	for _, av := range appVersions {
		svcName := svcMap[av.ServiceStackID]
		eventType := "deploy"
		if av.Build != nil && av.Build.PipelineStart != nil {
			eventType = "build"
		}

		events = append(events, TimelineEvent{
			Timestamp: av.Created,
			Type:      eventType,
			Action:    eventType,
			Status:    av.Status,
			Service:   svcName,
			Hint:      statusHint(av.Status, appVersionHintMap),
		})
		deployCount++
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
func calcDuration(started, finished *string) string {
	if started == nil || finished == nil {
		return ""
	}
	s, err := time.Parse(time.RFC3339, *started)
	if err != nil {
		return ""
	}
	f, err := time.Parse(time.RFC3339, *finished)
	if err != nil {
		return ""
	}
	d := f.Sub(s)
	if d < 0 {
		return ""
	}
	return formatDuration(d)
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
