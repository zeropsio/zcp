package ops

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

const (
	checkNameErrorLogs = "error_logs"
	checkNameHTTPRoot  = "http_root"
	runtimeStatic      = "static"
	runtimeNginx       = "nginx"
	runtimePHPApach    = "php-apache"
	runtimePHPNginx    = "php-nginx"
)

// RuntimeClass categorizes services for verify check dispatch.
type RuntimeClass int

const (
	RuntimeDynamic  RuntimeClass = iota // nodejs, go, bun, python, rust, java, deno, dotnet
	RuntimeImplicit                     // php-apache, php-nginx
	RuntimeStatic                       // static, nginx
	RuntimeWorker                       // any runtime with no run.ports
	RuntimeManaged                      // postgresql, valkey, etc.
)

// classifyRuntime determines the runtime class from service type and port presence.
func classifyRuntime(serviceType string, hasPorts bool) RuntimeClass {
	base, _, _ := strings.Cut(serviceType, "@")
	switch base {
	case runtimePHPApach, runtimePHPNginx:
		return RuntimeImplicit
	case runtimeStatic, runtimeNginx:
		return RuntimeStatic
	}
	// Dynamic runtimes become workers when they have no ports.
	if !hasPorts {
		return RuntimeWorker
	}
	return RuntimeDynamic
}

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

// batchLogChecks fetches error logs once and produces a single "error_logs" check.
func batchLogChecks(
	ctx context.Context, fetcher platform.LogFetcher,
	logAccess *platform.LogAccess, logErr error,
	serviceID string,
) []CheckResult {
	name := checkNameErrorLogs
	if logErr != nil {
		return []CheckResult{{Name: name, Status: CheckSkip, Detail: fmt.Sprintf("log backend unavailable: %v", logErr)}}
	}
	entries, err := fetcher.FetchLogs(ctx, logAccess, platform.LogFetchParams{
		ServiceID: serviceID,
		Severity:  "error",
		Since:     time.Now().Add(-5 * time.Minute),
		Limit:     5,
	})
	if err != nil {
		return []CheckResult{{Name: name, Status: CheckSkip, Detail: fmt.Sprintf("log backend unavailable: %v", err)}}
	}
	if len(entries) > 0 {
		msgs := make([]string, 0, 3)
		for i := range entries {
			if i >= 3 {
				break
			}
			msgs = append(msgs, entries[i].Message)
		}
		return []CheckResult{{Name: name, Status: CheckInfo, Detail: strings.Join(msgs, " | ")}}
	}
	return []CheckResult{{Name: name, Status: CheckPass}}
}

// checkHTTPRoot performs GET / and asks the question "is the HTTP server
// responding at all?" — NOT "does / return a 2xx?". Any HTTP response
// (2xx/3xx/4xx) proves the server is listening, binding the port, and
// handling requests; only 5xx or a connection error means the server
// is broken. Older versions of this check treated 404 as a failure,
// which flagged every API-only service that only routes /api/* as
// "degraded" in every single run — the root path is legitimately not
// served by API scaffolds, and downgrading the whole service for it is
// noise, not signal. Workflow-specific endpoint-shape checks
// (recipe's feature-sweep, bootstrap's /status curl) live in the
// workflow that knows the path — verify stays generic and only asks
// the single question it's qualified to answer.
func checkHTTPRoot(ctx context.Context, httpClient HTTPDoer, url string) CheckResult {
	name := checkNameHTTPRoot
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

	// 2xx / 3xx / 4xx — any response proves the HTTP server is up and
	// serving. 4xx means "you asked for something I don't have" which
	// is still proof of life.
	if resp.StatusCode < 500 {
		return CheckResult{Name: name, Status: CheckPass, HTTPStatus: resp.StatusCode}
	}
	// 5xx — the server is reachable but broken. This is the only
	// http_root outcome that downgrades the service to degraded.
	detail := fmt.Sprintf("HTTP %d", resp.StatusCode)
	if len(body) > 0 {
		detail += ": " + truncateBody(body, 200)
	}
	return CheckResult{Name: name, Status: CheckFail, Detail: detail, HTTPStatus: resp.StatusCode}
}

// truncateBody returns a string-truncated body with "..." if over max bytes.
func truncateBody(b []byte, limit int) string {
	if len(b) <= limit {
		return string(b)
	}
	return string(b[:limit]) + "..."
}

// ResolveSubdomainURL constructs the subdomain URL for a service in
// read-only mode (no enable call). Returns "" when subdomain access is
// disabled or no ports are exposed. Used by verify, eval probes, and any
// caller that wants the canonical URL without mutating platform state.
func ResolveSubdomainURL(ctx context.Context, client platform.Client, projectID string, svc *platform.ServiceStack) string {
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
