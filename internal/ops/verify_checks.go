package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

func checkServiceRunning(svc *platform.ServiceStack) CheckResult {
	if svc.Status == "RUNNING" || svc.Status == "ACTIVE" {
		return CheckResult{Name: "service_running", Status: CheckPass}
	}
	return CheckResult{
		Name:   "service_running",
		Status: CheckFail,
		Detail: fmt.Sprintf("service status: %s", svc.Status),
	}
}

func checkErrorLogs(
	ctx context.Context, fetcher platform.LogFetcher,
	logAccess *platform.LogAccess, logErr error,
	serviceID string, since time.Duration,
) CheckResult {
	name := "no_error_logs"
	if logErr != nil {
		return CheckResult{Name: name, Status: CheckSkip, Detail: fmt.Sprintf("log backend unavailable: %v", logErr)}
	}
	entries, err := fetcher.FetchLogs(ctx, logAccess, platform.LogFetchParams{
		ServiceID: serviceID,
		Severity:  "error",
		Since:     time.Now().Add(-since),
		Limit:     1,
	})
	if err != nil {
		return CheckResult{Name: name, Status: CheckSkip, Detail: fmt.Sprintf("log backend unavailable: %v", err)}
	}
	if len(entries) > 0 {
		return CheckResult{Name: name, Status: CheckFail, Detail: entries[0].Message}
	}
	return CheckResult{Name: name, Status: CheckPass}
}

func checkErrorLogs2m(
	ctx context.Context, fetcher platform.LogFetcher,
	logAccess *platform.LogAccess, logErr error,
	serviceID string,
) CheckResult {
	c := checkErrorLogs(ctx, fetcher, logAccess, logErr, serviceID, 2*time.Minute)
	c.Name = "no_recent_errors"
	return c
}

func checkStartupDetected(
	ctx context.Context, fetcher platform.LogFetcher,
	logAccess *platform.LogAccess, logErr error,
	serviceID string,
) CheckResult {
	name := "startup_detected"
	if logErr != nil {
		return CheckResult{Name: name, Status: CheckSkip, Detail: fmt.Sprintf("log backend unavailable: %v", logErr)}
	}
	entries, err := fetcher.FetchLogs(ctx, logAccess, platform.LogFetchParams{
		ServiceID: serviceID,
		Search:    "listening|started|ready",
		Since:     time.Now().Add(-5 * time.Minute),
		Limit:     1,
	})
	if err != nil {
		return CheckResult{Name: name, Status: CheckSkip, Detail: fmt.Sprintf("log backend unavailable: %v", err)}
	}
	if len(entries) == 0 {
		return CheckResult{Name: name, Status: CheckFail, Detail: "no startup message found in last 5m"}
	}
	return CheckResult{Name: name, Status: CheckPass}
}

func checkHTTPHealth(ctx context.Context, httpClient HTTPDoer, url string) CheckResult {
	name := "http_health"
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return CheckResult{Name: name, Status: CheckFail, Detail: fmt.Sprintf("request failed: %v", err)}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return CheckResult{Name: name, Status: CheckFail, Detail: fmt.Sprintf("request failed: %v", err)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 201))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return CheckResult{Name: name, Status: CheckPass, HTTPStatus: resp.StatusCode}
	}
	detail := fmt.Sprintf("HTTP %d", resp.StatusCode)
	if len(body) > 0 {
		detail += ": " + truncateBody(body, 200)
	}
	return CheckResult{Name: name, Status: CheckFail, Detail: detail, HTTPStatus: resp.StatusCode}
}

func checkHTTPStatus(ctx context.Context, httpClient HTTPDoer, url string) CheckResult {
	name := "http_status"
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return CheckResult{Name: name, Status: CheckFail, Detail: fmt.Sprintf("request failed: %v", err)}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return CheckResult{Name: name, Status: CheckFail, Detail: fmt.Sprintf("request failed: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 201))
		detail := fmt.Sprintf("HTTP %d", resp.StatusCode)
		if len(body) > 0 {
			detail += ": " + truncateBody(body, 200)
		}
		return CheckResult{Name: name, Status: CheckFail, Detail: detail, HTTPStatus: resp.StatusCode}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return CheckResult{Name: name, Status: CheckFail, Detail: fmt.Sprintf("read body: %v", err)}
	}

	var sr statusResponse
	if err := json.Unmarshal(body, &sr); err != nil {
		excerpt := truncateBody(body, 200)
		return CheckResult{Name: name, Status: CheckFail, Detail: fmt.Sprintf("response not JSON (HTTP %d): %s", resp.StatusCode, excerpt), HTTPStatus: resp.StatusCode}
	}

	// If connections present, check each one.
	if len(sr.Connections) > 0 {
		for connName, conn := range sr.Connections {
			if conn.Status != "ok" {
				return CheckResult{Name: name, Status: CheckFail, Detail: fmt.Sprintf("connection '%s': %s", connName, conn.Status)}
			}
		}
		return CheckResult{Name: name, Status: CheckPass}
	}

	// No connections: check top-level status.
	if sr.Status == "ok" {
		return CheckResult{Name: name, Status: CheckPass}
	}
	return CheckResult{Name: name, Status: CheckFail, Detail: fmt.Sprintf("status: %s", sr.Status)}
}

// truncateBody returns a string-truncated body with "..." if over max bytes.
func truncateBody(b []byte, limit int) string {
	if len(b) <= limit {
		return string(b)
	}
	return string(b[:limit]) + "..."
}

// resolveSubdomainURL constructs the subdomain URL for a service (read-only, no enable call).
func resolveSubdomainURL(ctx context.Context, client platform.Client, projectID string, svc *platform.ServiceStack) string {
	if !svc.SubdomainAccess {
		return ""
	}
	if len(svc.Ports) == 0 {
		return ""
	}

	proj, err := client.GetProject(ctx, projectID)
	if err != nil || proj.SubdomainHost == "" {
		return ""
	}

	url := BuildSubdomainURL(svc.Name, proj.SubdomainHost, svc.Ports[0].Port)
	if url != "" {
		return url
	}

	// Bare prefix fallback.
	domain := ExtractDomainFromEnv(ctx, client, svc.ID)
	if domain == "" {
		return ""
	}
	if svc.Ports[0].Port == 80 {
		return fmt.Sprintf("https://%s-%s.%s", svc.Name, proj.SubdomainHost, domain)
	}
	return fmt.Sprintf("https://%s-%s-%d.%s", svc.Name, proj.SubdomainHost, svc.Ports[0].Port, domain)
}

// aggregateStatus computes overall status from checks.
func aggregateStatus(checks []CheckResult) string {
	hasFail := false
	serviceRunningFailed := false
	for _, c := range checks {
		if c.Status == CheckFail {
			hasFail = true
			if c.Name == "service_running" {
				serviceRunningFailed = true
			}
		}
	}
	if serviceRunningFailed {
		return StatusUnhealthy
	}
	if hasFail {
		return StatusDegraded
	}
	return StatusHealthy
}
