package hq

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// ObservedEnvironment is a readable environment of this Mate's application,
// by project ID (never by a name used as a cross-project credential).
type ObservedEnvironment struct {
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Tier      string `json:"tier"`
}
type ObservedEnvironments struct {
	AppID        string                `json:"appId"`
	Environments []ObservedEnvironment `json:"environments"`
}
type ActiveVersion struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type ObservedService struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Status        string         `json:"status"`
	ActiveVersion *ActiveVersion `json:"activeVersion"`
}
type EnvironmentStatus struct {
	Environment ObservedEnvironment `json:"environment"`
	Services    []ObservedService   `json:"services"`
}
type ObservationLogEntry struct {
	Timestamp string `json:"timestamp"`
	Severity  string `json:"severity"`
	Message   string `json:"message"`
}
type EnvironmentLogs struct {
	ProjectID string                `json:"projectId"`
	ServiceID string                `json:"serviceId"`
	Entries   []ObservationLogEntry `json:"entries"`
}

// Each observation asks once. Unlike the legacy write transport it never
// waits and retries a 503: its failure belongs to the call.
func (c Client) Environments(ctx context.Context) (ObservedEnvironments, error) {
	var out ObservedEnvironments
	_, err := c.call.sendOnce(ctx, http.MethodGet, "/api/mate/environments", c.authorization(), "", nil, &out)
	if err != nil {
		return out, fmt.Errorf("HQ observation: %w", err)
	}
	return out, nil
}
func (c Client) EnvironmentStatus(ctx context.Context, projectID string) (EnvironmentStatus, error) {
	var out EnvironmentStatus
	_, err := c.call.sendOnce(ctx, http.MethodGet, "/api/mate/environments/"+url.PathEscape(projectID), c.authorization(), "", nil, &out)
	if err != nil {
		return out, fmt.Errorf("HQ observation: %w", err)
	}
	return out, nil
}
func (c Client) EnvironmentLogs(ctx context.Context, projectID, serviceID string, limit int) (EnvironmentLogs, error) {
	var out EnvironmentLogs
	_, err := c.call.sendOnce(ctx, http.MethodGet, "/api/mate/environments/"+url.PathEscape(projectID)+"/services/"+url.PathEscape(serviceID)+"/logs?limit="+strconv.Itoa(limit), c.authorization(), "", nil, &out)
	if err != nil {
		return out, fmt.Errorf("HQ observation: %w", err)
	}
	return out, nil
}
