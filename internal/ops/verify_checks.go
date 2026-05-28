package ops

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

const (
	checkNameErrorLogs = "error_logs"
	checkNameHTTPRoot  = "http_root"
	runtimeStatic      = "static"
	runtimeNginx       = "nginx"
	runtimePHPApach    = "php-apache"
	runtimePHPNginx    = "php-nginx"
	// httpRootBodyReadCap bounds the bytes we read from the GET / response
	// body. Detail still calls truncateBody(body, 200) for envelope
	// compactness; the larger read budget exists so the browser-render
	// step (and any future caller that needs more raw content) does not
	// have to re-issue the request.
	httpRootBodyReadCap = 16 << 10
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
//
// `serviceType` is the live API `ServiceStackTypeVersionName` — post-
// Sunday-release that carries the composite OS-prefixed form
// (`alpine/php-nginx@8.4`). Strip the OS prefix and mode suffix via
// `topology.CanonicalBareForm` before the switch so the bare-name cases
// (`runtimePHPNginx`, `runtimeNginx`, etc.) match regardless of which
// shape the API returns.
func classifyRuntime(serviceType string, hasPorts bool) RuntimeClass {
	canonical := topology.CanonicalBareForm(serviceType)
	base, _, _ := strings.Cut(canonical, "@")
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

func checkServiceRunning(ctx context.Context, client platform.Client, fetcher platform.LogFetcher, projectID string, svc *platform.ServiceStack) CheckResult {
	if svc.Status == platform.ServiceStatusRunning || svc.Status == platform.ServiceStatusActive {
		return CheckResult{Name: "service_running", Status: CheckPass}
	}
	return CheckResult{
		Name:     "service_running",
		Status:   CheckFail,
		Detail:   fmt.Sprintf("service status: %s", svc.Status),
		Recovery: NonRunningRecovery(ctx, client, fetcher, projectID, svc.Name, svc.Status),
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

// checkHTTPRoot performs GET / and asks "is the root endpoint serving a
// real response?" — pass on 2xx/3xx, fail on 4xx/5xx with the HTTP status
// surfaced in detail.
//
// History: this check previously passed on any non-5xx response (the
// "is the server alive?" semantic) because flagging API-only services
// as degraded over a `/` 404 produced noise across showcase runs. Eval
// review 20260518-subset revealed the opposite trap: agents read
// `http_root: pass, httpStatus: 404` as proof their endpoints work and
// signed off broken services. Phase 3 of fix-plan inverted the rule —
// the agent's "did the deploy succeed at delivering the user's app?"
// question is what verify needs to answer; reachability-only probing
// stays in `WaitHTTPReady` (used during L7 propagation polling) where
// the "is the server up at all" semantic is still right.
//
// For API-only services where `/` is legitimately not served, the agent
// reads the fail detail ("HTTP 404: ...") and either verifies a real
// endpoint or accepts the cosmetic failure with a one-line note to the
// user — both are honest about the state. Workflow-specific endpoint-
// shape checks (recipe's feature-sweep, bootstrap's /status curl) live
// in the workflow that knows the path.
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
	// Read up to httpRootBodyReadCap bytes. Detail still truncates to 200
	// chars for compactness; the larger buffer exists so a future
	// caller can forward the full body without re-issuing the request.
	body, _ := io.ReadAll(io.LimitReader(resp.Body, httpRootBodyReadCap))

	// 2xx / 3xx — root path serves a real response. Pass.
	if resp.StatusCode < 400 {
		return CheckResult{Name: name, Status: CheckPass, HTTPStatus: resp.StatusCode}
	}
	// 4xx — server reachable but root path not served (404), auth-gated
	// (401), or rejecting GET (405). Fail with the status + body excerpt
	// so the agent can decide whether to verify a real endpoint or
	// accept the failure as cosmetic and surface to the user.
	// 5xx — server reachable but broken; same shape.
	detail := fmt.Sprintf("HTTP %d", resp.StatusCode)
	if len(body) > 0 {
		detail += ": " + truncateBody(body, 200)
	}
	if resp.StatusCode < 500 {
		detail += " (server reachable but root path not serving a 2xx/3xx — verify a real endpoint or accept as cosmetic)"
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

// ResolveSubdomainURL constructs the subdomain URL for a service's
// HTTP-serving port, identified by Port.Scheme (PreferredHTTPPort). Returns ""
// when subdomain access is disabled or no ports are exposed. A multi-port
// service (e.g. mailpit: SMTP 1025 + HTTP UI 8025) resolves to the http-scheme
// port, not Ports[0]. Scheme is populated at deploy time — verified live: a
// 2-port service reports scheme "http" on the httpSupport port and "tcp" on the
// other BEFORE subdomain enable — so no network probe is needed to pick it.
func ResolveSubdomainURL(ctx context.Context, client platform.Client, projectID string, svc *platform.ServiceStack) string {
	if !svc.SubdomainAccess || len(svc.Ports) == 0 {
		return ""
	}
	proj, err := client.GetProject(ctx, projectID)
	if err != nil || proj.SubdomainHost == "" {
		return ""
	}
	port, ok := PreferredHTTPPort(svc.Ports)
	if !ok {
		return ""
	}
	return subdomainURLForPort(ctx, client, proj.SubdomainHost, svc, port.Port)
}

// subdomainURLForPort builds the subdomain URL for one specific port, using the
// direct builder with a bare-prefix env fallback.
func subdomainURLForPort(ctx context.Context, client platform.Client, subdomainHost string, svc *platform.ServiceStack, port int) string {
	if url := BuildSubdomainURL(svc.Name, subdomainHost, port); url != "" {
		return url
	}
	domain := ExtractDomainFromEnv(ctx, client, svc.ID)
	if domain == "" {
		return ""
	}
	if port == 80 {
		return fmt.Sprintf("https://%s-%s.%s", svc.Name, subdomainHost, domain)
	}
	return fmt.Sprintf("https://%s-%s-%d.%s", svc.Name, subdomainHost, port, domain)
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
