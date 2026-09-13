package ops

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// Verify status constants.
const (
	StatusHealthy   = "healthy"
	StatusDegraded  = "degraded"
	StatusUnhealthy = "unhealthy"
	CheckPass       = "pass"
	CheckFail       = "fail"
	CheckSkip       = "skip"
	CheckInfo       = "info"    // advisory — LLM sees the data but aggregateStatus ignores it
	CheckPending    = "pending" // in-flight state change (e.g. subdomain enable) — never degrades, like skip
)

// HTTPDoer executes HTTP requests (satisfied by *http.Client).
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// VerifyResult is the verification result for a single service.
type VerifyResult struct {
	Hostname string `json:"hostname"`
	Type     string `json:"type"` // "runtime" or "managed"
	// TypeVersion is the live stack type-version (e.g. "ubuntu/bun@1.3.9",
	// "php-nginx@8.4"). Carried so callers can classify the RuntimeClass
	// without a second API read — the verify tool uses it to annotate the
	// durability of a deferred-start (dev-mode dynamic) pass (RC-A′).
	TypeVersion           string        `json:"typeVersion,omitempty"`
	RuntimeClass          string        `json:"runtimeClass,omitempty"`
	RuntimeClassification string        `json:"runtimeClassification,omitempty"`
	Status                string        `json:"status"` // "healthy", "degraded", "unhealthy"
	Checks                []CheckResult `json:"checks"`
}

// PassedForLifecycle reports whether this verify result should count as a
// successful verify for work-session auto-close. Healthy always passes. A
// DEGRADED result passes ONLY when its sole failing checks are cosmetic
// http_public 4xx responses — the server is reachable and routing, and a 404 at
// `/` is normal for a REST API with no root route, so the deploy genuinely
// delivered a working app. A 5xx, a connection error, a failed service_running
// check, or any other failing check is a real problem and does NOT pass.
//
// This decouples the auto-close GATE from the cosmetic http_public fail: the
// verify RESPONSE still reports status=degraded (the agent stays honest about
// the 404 and is nudged to verify a real endpoint), but a REST API whose `/`
// 404s no longer blocks the session from auto-closing — which previously
// forced agents to add a throwaway root route just to clear the gate.
func (r *VerifyResult) PassedForLifecycle() bool {
	if r == nil {
		return false
	}
	if r.Status == StatusHealthy {
		return true
	}
	if r.Status != StatusDegraded {
		return false
	}
	for _, c := range r.Checks {
		if c.Status != CheckFail {
			continue
		}
		if c.Name == checkNameHTTPRoot && c.HTTPStatus >= 400 && c.HTTPStatus < 500 {
			continue // cosmetic: server reachable, / just not a 2xx route
		}
		return false // a non-cosmetic failure blocks
	}
	return true
}

// RuntimeMeta carries local deploy metadata into verify without coupling ops to
// workflow's ServiceMeta persistence layer.
type RuntimeMeta struct {
	ServesHTTP bool
	Recorded   bool
	Setup      string
}

func (m RuntimeMeta) recordedServesHTTP() *bool {
	if !m.Recorded {
		return nil
	}
	return &m.ServesHTTP
}

// Recovery is the ops-layer alias of topology.Recovery — promoted to layer-2
// vocabulary so workflow.StepCheck and tools.CheckWire both reference the
// same struct without per-layer mirrors.
type Recovery = topology.Recovery

// PublicAccessInput carries the user's persisted public-access intent plus
// whether the runtime is deferred-start into verify, without coupling ops to
// workflow's ServiceMeta persistence layer (mirrors RuntimeMeta's
// decoupling pattern). DeferredStart is topology.IsDeferredStart(mode,
// class) as computed by the caller — ops has no Mode of its own (topology.
// Mode is a ZCP concept persisted in ServiceMeta, which ops must not
// import per the architecture's layering rule).
type PublicAccessInput struct {
	Record        topology.PublicAccessRecord
	DeferredStart bool
}

// defaultPublicAccessInput is what meta-less callers (Verify, VerifyAll, and
// any caller not wired to workflow.ServiceMeta) pass: auto intent, not
// deferred-start. This reproduces the historical single-rule verify
// behavior for a service with no recorded intent — subdomain off ⇒ fail +
// enable recovery (docs/spec-workflows.md §8 O3 PA-4's auto∧listener∧off
// case).
func defaultPublicAccessInput() PublicAccessInput {
	return PublicAccessInput{Record: topology.PublicAccessRecord{Intent: topology.PublicAccessAuto}}
}

// CheckResult is the result of a single verification check.
type CheckResult struct {
	Name       string    `json:"name"`                 // "service_running", "error_logs", etc.
	Status     string    `json:"status"`               // "pass", "fail", "skip"
	Detail     string    `json:"detail,omitempty"`     // human-readable detail on fail/skip
	HTTPStatus int       `json:"httpStatus,omitempty"` // HTTP status code (0 = N/A)
	Recovery   *Recovery `json:"recovery,omitempty"`

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
	return VerifyWithRuntimeMeta(ctx, client, fetcher, httpClient, projectID, hostname, RuntimeMeta{})
}

// VerifyWithRuntimeMeta runs health verification for one service with optional
// local deploy metadata that records whether the deployed setup serves HTTP.
// Public-access intent defaults to auto/not-deferred-start (see
// defaultPublicAccessInput) — callers that know the service's persisted
// public-access record use VerifyWithMeta directly.
func VerifyWithRuntimeMeta(
	ctx context.Context,
	client platform.Client,
	fetcher platform.LogFetcher,
	httpClient HTTPDoer,
	projectID string,
	hostname string,
	runtimeMeta RuntimeMeta,
) (*VerifyResult, error) {
	return VerifyWithMeta(ctx, client, fetcher, httpClient, projectID, hostname, runtimeMeta, defaultPublicAccessInput())
}

// VerifyWithMeta runs health verification for one service with both local
// deploy metadata and the caller-supplied public-access input (§8 O3 PA-4).
func VerifyWithMeta(
	ctx context.Context,
	client platform.Client,
	fetcher platform.LogFetcher,
	httpClient HTTPDoer,
	projectID string,
	hostname string,
	runtimeMeta RuntimeMeta,
	publicAccess PublicAccessInput,
) (*VerifyResult, error) {
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return nil, err
	}
	svc, err := FindService(services, hostname)
	if err != nil {
		return nil, err
	}
	return verifyService(ctx, client, fetcher, httpClient, projectID, svc, runtimeMeta, publicAccess)
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
	runtimeMeta RuntimeMeta,
	publicAccess PublicAccessInput,
) (*VerifyResult, error) {
	managed := isManagedCategory(svc.ServiceStackTypeInfo.ServiceStackTypeCategoryName)

	result := &VerifyResult{
		Hostname:    svc.Name,
		Type:        "runtime",
		TypeVersion: svc.ServiceStackTypeInfo.ServiceStackTypeVersionName,
	}
	if managed {
		result.Type = runtimeClassManaged
	}

	// Check 1: service_running (must pass first).
	runningCheck := checkServiceRunning(ctx, client, fetcher, projectID, svc)
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
		runtimeMeta.recordedServesHTTP(),
	)
	result.RuntimeClass = rc.String()
	result.RuntimeClassification = runtimeClassificationProvenance(rc, runtimeMeta)

	// If not running, skip remaining checks based on runtime class — but
	// preserve the subdomain Recovery emission. Subdomain access is
	// independent of running state (the agent can enable it any time);
	// short-circuiting here would hide it until service_running passes,
	// forcing serial recovery instead of parallel. Plan v4 §2.1 side fix.
	if runningCheck.Status != CheckPass {
		result.Checks = append(result.Checks, skipChecksForClass(rc)...)
		// Replace the http_public skip with a subdomain-disabled fail when
		// the service has neither subdomain access nor a running container,
		// so the agent sees BOTH actionable hints in one verify pass. Only
		// for the auto/subdomain intents (§8 O3 PA-4) — a `none` or
		// `domain` intent means the user opted out of subdomain reachability
		// or is served by a custom domain instead, so no subdomain recovery
		// applies there even while the container is down.
		intent := publicAccess.Record.Intent
		wantsSubdomain := intent == topology.PublicAccessAuto || intent == topology.PublicAccessSubdomain
		if !svc.SubdomainAccess && wantsSubdomain && (rc == RuntimeDynamic || rc == RuntimeImplicit || rc == RuntimeStatic) {
			replaceCheck(result.Checks, checkNameHTTPRoot, CheckResult{
				Name:     checkNameHTTPRoot,
				Status:   CheckFail,
				Detail:   "subdomain access not enabled — service is not reachable via HTTP (independent of service_running)",
				Recovery: &Recovery{Tool: "zerops_subdomain", Action: subdomainActionEnable, Args: map[string]string{"serviceHostname": svc.Name}},
			})
		}
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

	// Group B: HTTP checks — http_internal (project-network reachability,
	// every HTTP-class runtime) + http_public (subdomain/domain
	// reachability, gated by the user's public-access intent). verify is a
	// generic aliveness tool: it does NOT curl workflow-specific health
	// paths because those paths are framework-dependent (recipes live at
	// /api/status, bootstrap at /status, Laravel at /up, etc.). Workflow
	// layers that know their paths iterate them themselves: the recipe
	// workflow's feature-sweep-dev sub-step iterates plan.Features and
	// curls each health path with a content-type contract; bootstrap's
	// workflow guidance explicitly curls its /status endpoint. Those checks
	// belong to the workflows, not to a generic "does this service
	// respond?" probe. http_internal/http_public ask the two questions
	// verify is qualified to answer (docs/spec-workflows.md §8 O3 PA-4).
	needHTTP := rc == RuntimeDynamic || rc == RuntimeImplicit || rc == RuntimeStatic
	if needHTTP {
		wg.Go(func() {
			listener := !publicAccess.DeferredStart
			var checks []CheckResult
			checks = append(checks, checkHTTPInternal(ctx, httpClient, svc, publicAccess.DeferredStart))

			obs, obsErr := ObservePublicAccess(ctx, client, projectID, svc)
			if obsErr != nil {
				checks = append(checks, CheckResult{
					Name:   checkNameHTTPRoot,
					Status: CheckFail,
					Detail: fmt.Sprintf("observe public access: %v", obsErr),
				})
			} else {
				checks = append(checks, buildHTTPPublicChecks(ctx, client, httpClient, projectID, svc, publicAccess.Record.Intent, obs, listener)...)
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

// replaceCheck overwrites the named check in-place with the replacement.
// No-op when the check is absent. Used by the service-not-running branch
// to upgrade an http_public skip to a subdomain-disabled fail with Recovery
// (plan v4 §2.1 side fix).
func replaceCheck(checks []CheckResult, name string, replacement CheckResult) {
	for i := range checks {
		if checks[i].Name == name {
			checks[i] = replacement
			return
		}
	}
}

func runtimeClassificationProvenance(rc RuntimeClass, runtimeMeta RuntimeMeta) string {
	if !runtimeMeta.Recorded {
		return ""
	}
	if runtimeMeta.Setup != "" {
		if runtimeMeta.ServesHTTP {
			return fmt.Sprintf("classified HTTP runtime from deployed setup %q (has HTTP)", runtimeMeta.Setup)
		}
		return fmt.Sprintf("classified worker from deployed setup %q (no HTTP)", runtimeMeta.Setup)
	}
	if runtimeMeta.ServesHTTP {
		return "classified HTTP runtime from recorded deployed setup (has HTTP)"
	}
	if rc == RuntimeWorker {
		return "classified worker from recorded deployed setup (no HTTP)"
	}
	return "classified from recorded deployed setup"
}

// skipChecksForClass returns skip results for all checks applicable to the runtime class.
func skipChecksForClass(rc RuntimeClass) []CheckResult {
	skipDetail := "service not running"
	var checks []CheckResult

	switch rc {
	case RuntimeDynamic:
		checks = append(checks,
			CheckResult{Name: checkNameErrorLogs, Status: CheckSkip, Detail: skipDetail},
			CheckResult{Name: checkNameHTTPInternal, Status: CheckSkip, Detail: skipDetail},
			CheckResult{Name: checkNameHTTPRoot, Status: CheckSkip, Detail: skipDetail},
		)
	case RuntimeImplicit:
		checks = append(checks,
			CheckResult{Name: checkNameErrorLogs, Status: CheckSkip, Detail: skipDetail},
			CheckResult{Name: checkNameHTTPInternal, Status: CheckSkip, Detail: skipDetail},
			CheckResult{Name: checkNameHTTPRoot, Status: CheckSkip, Detail: skipDetail},
		)
	case RuntimeStatic:
		checks = append(checks,
			CheckResult{Name: checkNameHTTPInternal, Status: CheckSkip, Detail: skipDetail},
			CheckResult{Name: checkNameHTTPRoot, Status: CheckSkip, Detail: skipDetail},
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
	return VerifyAllWithRuntimeMeta(ctx, client, fetcher, httpClient, projectID, nil)
}

// RuntimeMetaResolver returns optional local deploy metadata by hostname.
type RuntimeMetaResolver func(hostname string) RuntimeMeta

// PublicAccessResolver returns the public-access input for a hostname. A nil
// resolver (or a hostname it has no opinion on) falls back to
// defaultPublicAccessInput.
type PublicAccessResolver func(hostname string) PublicAccessInput

// VerifyAllWithRuntimeMeta runs health verification for all services with
// optional per-service local deploy metadata. Public-access intent defaults
// to auto/not-deferred-start for every service (see defaultPublicAccessInput)
// — callers that know per-service persisted public-access records use
// VerifyAllWithMeta directly.
func VerifyAllWithRuntimeMeta(
	ctx context.Context,
	client platform.Client,
	fetcher platform.LogFetcher,
	httpClient HTTPDoer,
	projectID string,
	runtimeMetaFor RuntimeMetaResolver,
) (*VerifyAllResult, error) {
	return VerifyAllWithMeta(ctx, client, fetcher, httpClient, projectID, runtimeMetaFor, nil)
}

// VerifyAllWithMeta runs health verification for all services with both
// per-service local deploy metadata and per-service public-access input.
func VerifyAllWithMeta(
	ctx context.Context,
	client platform.Client,
	fetcher platform.LogFetcher,
	httpClient HTTPDoer,
	projectID string,
	runtimeMetaFor RuntimeMetaResolver,
	publicAccessFor PublicAccessResolver,
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

			runtimeMeta := RuntimeMeta{}
			if runtimeMetaFor != nil {
				runtimeMeta = runtimeMetaFor(targets[idx].Name)
			}
			publicAccess := defaultPublicAccessInput()
			if publicAccessFor != nil {
				publicAccess = publicAccessFor(targets[idx].Name)
			}
			r, verifyErr := verifyService(ctx, client, fetcher, httpClient, projectID, &targets[idx], runtimeMeta, publicAccess)
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

	overall, healthy := aggregateAllStatus(results)
	return &VerifyAllResult{
		Summary:  fmt.Sprintf("%d/%d healthy", healthy, len(targets)),
		Status:   overall,
		Services: results,
	}, nil
}

// aggregateAllStatus computes the project-wide verify status + healthy count
// from per-service results. overall is `unhealthy` ONLY on a total outage (a
// hard-down service AND nothing healthy left); a partial outage (some healthy)
// or purely cosmetic degradation (e.g. a lone http_public 4xx, no hard-down) is
// `degraded`, never `unhealthy` — so the project status can't contradict the
// lifecycle accepting the deploy (see VerifyResult.PassedForLifecycle).
func aggregateAllStatus(results []VerifyResult) (overall string, healthy int) {
	unhealthy := 0
	for i := range results {
		switch results[i].Status {
		case StatusHealthy:
			healthy++
		case StatusUnhealthy:
			unhealthy++
		} // StatusDegraded counts as neither — captured by the default below
	}
	switch {
	case healthy == len(results):
		return StatusHealthy, healthy
	case unhealthy > 0 && healthy == 0:
		return StatusUnhealthy, healthy
	default:
		return StatusDegraded, healthy
	}
}
