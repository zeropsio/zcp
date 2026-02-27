package ops

import (
	"context"
	"net/http"
	"strings"
	"time"

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
	Name   string `json:"name"`             // "service_running", "no_error_logs", etc.
	Status string `json:"status"`           // "pass", "fail", "skip"
	Detail string `json:"detail,omitempty"` // human-readable detail on fail/skip
}

// statusResponse is the expected /status endpoint response (per bootstrap.md spec).
// Contract: coupled with bootstrap.md /status spec. See CLAUDE.md Maintenance table.
type statusResponse struct {
	Service     string                      `json:"service"`
	Status      string                      `json:"status"`
	Connections map[string]connectionStatus `json:"connections"`
}

type connectionStatus struct {
	Status string `json:"status"`
}

// managedBaseTypes identifies service types fully managed by Zerops.
var managedBaseTypes = map[string]bool{
	"postgresql":     true,
	"mariadb":        true,
	"valkey":         true,
	"keydb":          true,
	"elasticsearch":  true,
	"object-storage": true,
	"shared-storage": true,
	"kafka":          true,
	"nats":           true,
	"meilisearch":    true,
	"clickhouse":     true,
	"qdrant":         true,
	"typesense":      true,
	"rabbitmq":       true,
}

func isManagedType(serviceTypeVersion string) bool {
	base, _, _ := strings.Cut(serviceTypeVersion, "@")
	return managedBaseTypes[base]
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
	svc, err := resolveServiceID(services, hostname)
	if err != nil {
		return nil, err
	}

	typeName := svc.ServiceStackTypeInfo.ServiceStackTypeVersionName
	managed := isManagedType(typeName)

	result := &VerifyResult{
		Hostname: hostname,
		Type:     "runtime",
	}
	if managed {
		result.Type = "managed"
	}

	// Check 1: service_running
	runningCheck := checkServiceRunning(svc)
	result.Checks = append(result.Checks, runningCheck)

	// Managed services: only check service_running.
	if managed {
		result.Status = aggregateStatus(result.Checks)
		return result, nil
	}

	// If not running, skip remaining checks.
	if runningCheck.Status != CheckPass {
		skipDetail := "service not running"
		result.Checks = append(result.Checks,
			CheckResult{Name: "no_error_logs", Status: CheckSkip, Detail: skipDetail},
			CheckResult{Name: "startup_detected", Status: CheckSkip, Detail: skipDetail},
			CheckResult{Name: "no_recent_errors", Status: CheckSkip, Detail: skipDetail},
			CheckResult{Name: "http_health", Status: CheckSkip, Detail: skipDetail},
			CheckResult{Name: "http_status", Status: CheckSkip, Detail: skipDetail},
		)
		result.Status = aggregateStatus(result.Checks)
		return result, nil
	}

	// Get log access (single call, reused across log checks).
	logAccess, logErr := client.GetProjectLog(ctx, projectID)

	// Check 2: no_error_logs (5m)
	result.Checks = append(result.Checks,
		checkErrorLogs(ctx, fetcher, logAccess, logErr, svc.ID, 5*time.Minute))

	// Check 3: startup_detected
	result.Checks = append(result.Checks,
		checkStartupDetected(ctx, fetcher, logAccess, logErr, svc.ID))

	// Check 4: no_recent_errors (2m)
	result.Checks = append(result.Checks,
		checkErrorLogs2m(ctx, fetcher, logAccess, logErr, svc.ID))

	// Resolve subdomain URL for HTTP checks.
	subdomainURL := resolveSubdomainURL(ctx, client, projectID, svc)

	if subdomainURL == "" {
		skipDetail := "subdomain not enabled — call zerops_subdomain action=enable first"
		if svc.SubdomainAccess {
			skipDetail = "cannot resolve subdomain URL"
		}
		result.Checks = append(result.Checks,
			CheckResult{Name: "http_health", Status: CheckSkip, Detail: skipDetail},
			CheckResult{Name: "http_status", Status: CheckSkip, Detail: skipDetail},
		)
	} else {
		// Check 5: http_health
		result.Checks = append(result.Checks,
			checkHTTPHealth(ctx, httpClient, subdomainURL+"/health"))

		// Check 6: http_status
		result.Checks = append(result.Checks,
			checkHTTPStatus(ctx, httpClient, subdomainURL+"/status"))
	}

	result.Status = aggregateStatus(result.Checks)
	return result, nil
}
