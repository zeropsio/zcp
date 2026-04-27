package ops

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/zeropsio/zcp/internal/platform"
)

// Verify status constants.
const (
	StatusHealthy   = "healthy"
	StatusDegraded  = "degraded"
	StatusUnhealthy = "unhealthy"
	CheckPass       = "pass"
	CheckFail       = "fail"
	CheckSkip       = "skip"
	CheckInfo       = "info" // advisory — LLM sees the data but aggregateStatus ignores it
)

// HTTPDoer executes HTTP requests (satisfied by *http.Client).
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// VerifyResult is the verification result for a single service.
type VerifyResult struct {
	Hostname string        `json:"hostname"`
	Type     string        `json:"type"`   // "runtime" or "managed"
	Status   string        `json:"status"` // "healthy", "degraded", "unhealthy"
	Checks   []CheckResult `json:"checks"`
}

// CheckResult is the result of a single verification check.
type CheckResult struct {
	Name       string `json:"name"`                 // "service_running", "error_logs", etc.
	Status     string `json:"status"`               // "pass", "fail", "skip"
	Detail     string `json:"detail,omitempty"`     // human-readable detail on fail/skip
	HTTPStatus int    `json:"httpStatus,omitempty"` // HTTP status code (0 = N/A)

	// BodyText is document.body.innerText captured by agent-browser after
	// the HTTP probe connected. Best-effort: populated only when an actual
	// browser walk succeeded (so SPA-rendered pages reach a real DOM
	// instead of an unhydrated shell, and framework error pages like
	// Laravel Ignition surface their actual content). Capped at
	// browserBodyTextCap. Absent on connect-failure, when agent-browser
	// is missing, or when the walk fork-recovered / wedged. Never an
	// error signal — its absence is normal in local dev.
	BodyText string `json:"bodyText,omitempty"`

	// ConsoleErrors is at most browserConsoleMax recent console.error /
	// uncaught-exception messages from the same agent-browser walk that
	// produced BodyText. Each entry is capped at browserConsoleEntryCap
	// chars. Absent under the same conditions as BodyText.
	ConsoleErrors []string `json:"consoleErrors,omitempty"`
}

// isManagedCategory returns true if the API category represents a managed service.
func isManagedCategory(categoryName string) bool {
	switch categoryName {
	case "STANDARD", "SHARED_STORAGE", "OBJECT_STORAGE":
		return true
	default:
		return false
	}
}

// Verify runs health verification checks for a single service.
func Verify(
	ctx context.Context,
	client platform.Client,
	fetcher platform.LogFetcher,
	httpClient HTTPDoer,
	projectID string,
	hostname string,
) (*VerifyResult, error) {
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return nil, err
	}
	svc, err := FindService(services, hostname)
	if err != nil {
		return nil, err
	}
	return verifyService(ctx, client, fetcher, httpClient, projectID, svc)
}

// verifyService runs health verification checks for a pre-resolved service.
// Used by Verify (after resolution) and VerifyAll (with pre-fetched list).
func verifyService(
	ctx context.Context,
	client platform.Client,
	fetcher platform.LogFetcher,
	httpClient HTTPDoer,
	projectID string,
	svc *platform.ServiceStack,
) (*VerifyResult, error) {
	managed := isManagedCategory(svc.ServiceStackTypeInfo.ServiceStackTypeCategoryName)

	result := &VerifyResult{
		Hostname: svc.Name,
		Type:     "runtime",
	}
	if managed {
		result.Type = "managed"
	}

	// Check 1: service_running (must pass first).
	runningCheck := checkServiceRunning(svc)
	result.Checks = append(result.Checks, runningCheck)

	// Managed services: only check service_running.
	if managed {
		result.Status = aggregateStatus(result.Checks)
		return result, nil
	}

	// Classify runtime for check dispatch.
	rc := classifyRuntime(
		svc.ServiceStackTypeInfo.ServiceStackTypeVersionName,
		len(svc.Ports) > 0,
	)

	// If not running, skip remaining checks based on runtime class.
	if runningCheck.Status != CheckPass {
		result.Checks = append(result.Checks, skipChecksForClass(rc)...)
		result.Status = aggregateStatus(result.Checks)
		return result, nil
	}

	// Run checks in parallel groups.
	var (
		mu         sync.Mutex
		logChecks  []CheckResult
		httpChecks []CheckResult
		wg         sync.WaitGroup
	)

	// Group A: log checks (single API call).
	needLogs := rc == RuntimeDynamic || rc == RuntimeImplicit || rc == RuntimeWorker
	if needLogs {
		wg.Go(func() {
			logAccess, logErr := client.GetProjectLog(ctx, projectID)
			checks := batchLogChecks(ctx, fetcher, logAccess, logErr, svc.ID)
			mu.Lock()
			logChecks = checks
			mu.Unlock()
		})
	}

	// Group B: HTTP check (single probe — "is the HTTP server alive?").
	// verify is a generic aliveness tool: it does NOT curl workflow-
	// specific health paths because those paths are framework-dependent
	// (recipes live at /api/status, bootstrap at /status, Laravel at
	// /up, etc.). Workflow layers that know their paths iterate them
	// themselves: the recipe workflow's feature-sweep-dev sub-step
	// iterates plan.Features and curls each health path with a content-
	// type contract; bootstrap's workflow guidance explicitly curls
	// its /status endpoint. Those checks belong to the workflows, not
	// to a generic "does this service respond?" probe. http_root asks
	// the single question verify is qualified to answer.
	needHTTP := rc == RuntimeDynamic || rc == RuntimeImplicit || rc == RuntimeStatic
	if needHTTP {
		wg.Go(func() {
			subdomainURL := ResolveSubdomainURL(ctx, client, projectID, svc)
			var checks []CheckResult
			if subdomainURL == "" {
				skipDetail := "subdomain not enabled — call zerops_subdomain action=enable first"
				if svc.SubdomainAccess {
					skipDetail = "cannot resolve subdomain URL"
				}
				checks = append(checks, CheckResult{Name: "http_root", Status: CheckSkip, Detail: skipDetail})
			} else {
				probeURL := subdomainURL + "/"
				check := checkHTTPRoot(ctx, httpClient, probeURL)
				// Render augmentation runs once per service-verify call
				// AFTER the connect probe succeeded (HTTPStatus > 0). Both
				// 2xx (documents the rendered state) and 5xx (surfaces the
				// framework error page) benefit. Connect failures skip the
				// browser walk — a wedged TCP path won't render anyway and
				// the agent-browser timeout would just delay the verdict.
				if check.HTTPStatus > 0 {
					bodyText, consoleErrors := renderHTTPRoot(ctx, probeURL)
					check.BodyText = bodyText
					check.ConsoleErrors = consoleErrors
				}
				checks = append(checks, check)
			}
			mu.Lock()
			httpChecks = checks
			mu.Unlock()
		})
	}

	wg.Wait()

	// Assemble checks in deterministic order: logs, then HTTP.
	result.Checks = append(result.Checks, logChecks...)
	result.Checks = append(result.Checks, httpChecks...)

	result.Status = aggregateStatus(result.Checks)
	return result, nil
}

// skipChecksForClass returns skip results for all checks applicable to the runtime class.
func skipChecksForClass(rc RuntimeClass) []CheckResult {
	skipDetail := "service not running"
	var checks []CheckResult

	switch rc {
	case RuntimeDynamic:
		checks = append(checks,
			CheckResult{Name: "error_logs", Status: CheckSkip, Detail: skipDetail},
			CheckResult{Name: "http_root", Status: CheckSkip, Detail: skipDetail},
		)
	case RuntimeImplicit:
		checks = append(checks,
			CheckResult{Name: "error_logs", Status: CheckSkip, Detail: skipDetail},
			CheckResult{Name: "http_root", Status: CheckSkip, Detail: skipDetail},
		)
	case RuntimeStatic:
		checks = append(checks,
			CheckResult{Name: "http_root", Status: CheckSkip, Detail: skipDetail},
		)
	case RuntimeWorker:
		checks = append(checks,
			CheckResult{Name: "error_logs", Status: CheckSkip, Detail: skipDetail},
		)
	case RuntimeManaged:
		// Managed services only get service_running check — no extra skips needed.
	}

	return checks
}

// VerifyAllResult is the verification result for all services in a project.
type VerifyAllResult struct {
	Summary  string         `json:"summary"`
	Status   string         `json:"status"` // healthy/degraded/unhealthy
	Services []VerifyResult `json:"services"`
}

// VerifyAll runs health verification for all non-system services in a project.
func VerifyAll(
	ctx context.Context,
	client platform.Client,
	fetcher platform.LogFetcher,
	httpClient HTTPDoer,
	projectID string,
) (*VerifyAllResult, error) {
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return nil, err
	}

	// Filter to user-facing services.
	var targets []platform.ServiceStack
	for _, svc := range services {
		if !svc.IsSystem() {
			targets = append(targets, svc)
		}
	}

	if len(targets) == 0 {
		return &VerifyAllResult{
			Summary:  "0/0 healthy",
			Status:   StatusHealthy,
			Services: []VerifyResult{},
		}, nil
	}

	// Run verifyService per service with bounded concurrency.
	// Uses pre-fetched service data — no additional ListServices calls.
	results := make([]VerifyResult, len(targets))
	sem := make(chan struct{}, 5) // max 5 concurrent
	var wg sync.WaitGroup

	for i := range targets {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			r, verifyErr := verifyService(ctx, client, fetcher, httpClient, projectID, &targets[idx])
			if verifyErr != nil {
				results[idx] = VerifyResult{
					Hostname: targets[idx].Name,
					Type:     "unknown",
					Status:   StatusUnhealthy,
					Checks: []CheckResult{
						{Name: "verify_error", Status: CheckFail, Detail: verifyErr.Error()},
					},
				}
				return
			}
			results[idx] = *r
		}(i)
	}
	wg.Wait()

	// Aggregate.
	healthy := 0
	hasUnhealthy := false
	for i := range results {
		if results[i].Status == StatusHealthy {
			healthy++
		} else {
			hasUnhealthy = true
		}
	}

	overall := StatusHealthy
	if hasUnhealthy {
		if healthy == 0 {
			overall = StatusUnhealthy
		} else {
			overall = StatusDegraded
		}
	}

	return &VerifyAllResult{
		Summary:  fmt.Sprintf("%d/%d healthy", healthy, len(targets)),
		Status:   overall,
		Services: results,
	}, nil
}
