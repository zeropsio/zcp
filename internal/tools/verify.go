package tools

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// VerifyInput is the input type for zerops_verify.
type VerifyInput struct {
	ServiceHostname string `json:"serviceHostname,omitempty" jsonschema:"Hostname of the service to verify. Omit to verify all services."`
}

// RegisterVerify registers the zerops_verify tool. ssh is used ONLY to
// freshen the single-hostname path's DeferredStart classification against
// a live dev-server liveness read (ops.DevServerRunning, §8 O3 PA-4) — nil
// is accepted (local-only mode has no SSH deployer) and degrades to the
// static (mode, class) classification.
func RegisterVerify(srv *mcp.Server, client platform.Client, fetcher platform.LogFetcher, projectID, stateDir string, rt runtime.Info, ssh ops.SSHDeployer) {
	httpClient := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_verify",
		Description: "Run health checks on a service. Returns structured results: service status, error logs, startup detection, HTTP connectivity. Check statuses: pass, fail, skip, info (advisory, not failure). Omit serviceHostname to verify all services.",
		Annotations: &mcp.ToolAnnotations{
			Title:          "Verify service health",
			ReadOnlyHint:   true,
			IdempotentHint: true,
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input VerifyInput) (*mcp.CallToolResult, any, error) {
		if input.ServiceHostname == "" {
			result, err := ops.VerifyAllWithMeta(ctx, client, fetcher, httpClient, projectID, runtimeMetaResolver(stateDir), publicAccessResolver(stateDir))
			if err != nil {
				return convertError(err, WithRecoveryStatus()), nil, nil
			}
			recordVerifyAllToWorkSession(stateDir, result)
			return jsonResult(verifyAllResponse{
				VerifyAllResult:  result,
				WorkSessionState: sessionAnnotations(stateDir),
				Envelope:         freshEnvelope(ctx, stateDir, client, projectID, rt),
			}), nil, nil
		}
		// The service asked for is the one checked: a git-push pair's dev
		// half is a runtime of its own, and the work session's gate needs its
		// own verify. Its stage is verified by naming it.
		host := input.ServiceHostname
		result, err := ops.VerifyWithMeta(ctx, client, fetcher, httpClient, projectID, host, runtimeMetaForHost(stateDir, host), publicAccessInputForHost(ctx, client, ssh, projectID, stateDir, host))
		if err != nil {
			return convertError(err, WithRecoveryStatus()), nil, nil
		}
		recordVerifyToWorkSession(stateDir, result)
		resp := verifyResponse{
			VerifyResult:     result,
			WorkSessionState: sessionAnnotations(stateDir),
			Envelope:         freshEnvelope(ctx, stateDir, client, projectID, rt),
		}
		// RC-A′: a passing verify on a deferred-start (dev-mode dynamic)
		// service reflects a LIVE dev-server process, not the app's own
		// start — kept across restarts and redeploys, down after a crash.
		// Annotate so the agent does not report it as an always-on service
		// (the e2 failure).
		resp.Note = deferredStartDurabilityNote(stateDir, host, result)
		return jsonResult(resp), nil, nil
	})
}

// verifyResponse wraps ops.VerifyResult with the structured
// WorkSessionState lifecycle signal (F5 closure). Surfacing the
// session state turns verify from a pure HTTP probe into an observable
// lifecycle event — the agent sees whether this call landed inside an
// open session, on an auto-closed one, or with no session tracking.
//
// Note carries a user-facing hint about what the verify's pass means.
type verifyResponse struct {
	*ops.VerifyResult
	Note             string            `json:"note,omitempty"`
	WorkSessionState *WorkSessionState `json:"workSessionState,omitempty"`
	// Envelope is the post-mutation lifecycle state (docs/spec-mate.md §1.3).
	// Absent when its computation failed — the rest of the response is
	// unaffected.
	Envelope *workflow.StateEnvelope `json:"envelope,omitempty"`
}

// verifyAllResponse mirrors verifyResponse for the multi-service VerifyAll
// path. Each service's attempt is already recorded; one session-state
// snapshot at the response root reflects the whole scope.
type verifyAllResponse struct {
	*ops.VerifyAllResult
	WorkSessionState *WorkSessionState `json:"workSessionState,omitempty"`
	// Envelope is the post-mutation lifecycle state (docs/spec-mate.md §1.3).
	// Absent when its computation failed — the rest of the response is
	// unaffected.
	Envelope *workflow.StateEnvelope `json:"envelope,omitempty"`
}

func runtimeMetaResolver(stateDir string) ops.RuntimeMetaResolver {
	if stateDir == "" {
		return nil
	}
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil || len(metas) == 0 {
		return nil
	}
	idx := workflow.ManagedRuntimeIndex(metas)
	return func(hostname string) ops.RuntimeMeta {
		return runtimeMetaFromServiceMeta(idx[hostname], hostname)
	}
}

func runtimeMetaForHost(stateDir, host string) ops.RuntimeMeta {
	if stateDir == "" || host == "" {
		return ops.RuntimeMeta{}
	}
	meta, err := workflow.FindServiceMeta(stateDir, host)
	if err != nil {
		return ops.RuntimeMeta{}
	}
	return runtimeMetaFromServiceMeta(meta, host)
}

func runtimeMetaFromServiceMeta(meta *workflow.ServiceMeta, host string) ops.RuntimeMeta {
	if meta == nil || meta.ServesHTTP == nil {
		return ops.RuntimeMeta{}
	}
	return ops.RuntimeMeta{
		ServesHTTP: *meta.ServesHTTP,
		Recorded:   true,
		Setup:      meta.SetupNameFor(host),
	}
}

// defaultPublicAccessInput is what a meta-less caller passes: auto intent,
// not deferred-start — mirrors ops.defaultPublicAccessInput (unexported
// there; tools has no access to it and constructs the equivalent directly).
func defaultPublicAccessInput() ops.PublicAccessInput {
	return ops.PublicAccessInput{Record: topology.PublicAccessRecord{Intent: topology.PublicAccessAuto}}
}

// publicAccessInputForHost builds the ops.PublicAccessInput for the
// single-hostname verify path (§8 O3 PA-4) from host's persisted ServiceMeta
// record plus a fresh (mode, runtime-class) DeferredStart classification —
// mirroring tools/subdomain.go's skipDeferredStartProbe pattern (one extra
// ops.LookupService call to get the live TypeVersion needed for
// topology.RuntimeClassFor; ops itself has no Mode of its own to compute
// this internally). A lookup failure degrades to DeferredStart=false (probe
// runs) rather than blocking verify on an unrelated read.
//
// A statically deferred-start classification (dev-mode dynamic runtime) is
// then freshened against the dev-server supervisor (§8 O4): a declared server
// with a port it runs means the runtime DOES have a listener, so DeferredStart
// flips to false and http_internal probes
// for real instead of skipping with the "start it with zerops_dev_server"
// message. A dev service that declares several servers with a port has each
// checked (ops.PublicAccessInput.DevPorts, TEST-PLAN E1 #11). An SSH error
// (no deployer, unreachable container) falls back to
// the static classification — verify must never fail because the listener
// probe itself failed.
func publicAccessInputForHost(ctx context.Context, client platform.Client, ssh ops.SSHDeployer, projectID, stateDir, host string) ops.PublicAccessInput {
	if stateDir == "" || host == "" {
		return defaultPublicAccessInput()
	}
	meta, err := workflow.FindServiceMeta(stateDir, host)
	if err != nil || meta == nil {
		return defaultPublicAccessInput()
	}
	devServer := servedByDevServer(meta, host)
	deferredStart := false
	var devPorts []ops.DevPort
	if svc, lookupErr := ops.LookupService(ctx, client, projectID, host); lookupErr == nil && svc != nil {
		class := topology.RuntimeClassFor(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName)
		deferredStart = topology.IsDeferredStart(meta.ModeFor(host), class)
		if deferredStart && ssh != nil {
			if ds, readErr := ops.ReadDevServers(ctx, ssh, host, meta.WorkingDirFor(host)); readErr == nil {
				deferredStart = !ds.Listening()
				devPorts = ds.DevPorts()
			}
		}
	}
	return ops.PublicAccessInput{Record: meta.PublicAccessFor(host), DeferredStart: deferredStart, DevServer: devServer, DevPorts: devPorts}
}

// servedByDevServer reports a host whose mode leaves a dynamic runtime to a
// dev server (topology.IsDeferredStart's mode half) — whatever that server's
// live state; ops gives its HTTP probes a page's first compile when the
// runtime is dynamic.
func servedByDevServer(meta *workflow.ServiceMeta, host string) bool {
	return topology.IsDeferredStart(meta.ModeFor(host), topology.RuntimeDynamic)
}

// publicAccessResolver builds a per-hostname ops.PublicAccessResolver for
// the all-services verify path from ServiceMeta's persisted intent only —
// DeferredStart is left at its default (false) here rather than paying one
// ops.LookupService per service (VerifyAll already reads every service once
// internally; duplicating that N times in the tool layer would defeat the
// "single ListServices call" property VerifyAll is pinned on). A deferred-
// start dev runtime scanned via the all-services path therefore reads
// http_internal as a real probe (not skipped) until it's verified by
// hostname directly, where the DeferredStart classification is exact.
func publicAccessResolver(stateDir string) ops.PublicAccessResolver {
	if stateDir == "" {
		return nil
	}
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil || len(metas) == 0 {
		return nil
	}
	idx := workflow.ManagedRuntimeIndex(metas)
	return func(hostname string) ops.PublicAccessInput {
		meta := idx[hostname]
		if meta == nil {
			return defaultPublicAccessInput()
		}
		return ops.PublicAccessInput{Record: meta.PublicAccessFor(hostname), DevServer: servedByDevServer(meta, hostname)}
	}
}

// deferredStartDurabilityNote returns a durability caveat when a passing
// verify reflects a deferred-start runtime — a dev-mode dynamic service whose
// app runs ONLY via the zerops_dev_server process (RC-A′). For those, an HTTP
// 200 is a live dev process, not the app's own start: the dev service's
// supervisor starts a declared server again after a restart or redeploy
// (spec-workflows §8 O4), but a crash leaves the URL down. Returns "" for non-deferred or
// failing verifies (the latter already carry their own failure detail).
// Pure over stable (mode, class) — no liveness read.
func deferredStartDurabilityNote(stateDir, host string, result *ops.VerifyResult) string {
	if result == nil || result.Status != ops.StatusHealthy {
		return ""
	}
	meta, err := workflow.FindServiceMeta(stateDir, host)
	if err != nil || meta == nil {
		return ""
	}
	class := topology.RuntimeClassFor(result.TypeVersion)
	if !topology.IsDeferredStart(meta.ModeFor(host), class) {
		return ""
	}
	return fmt.Sprintf("Durability: %q is dev-mode — this 200 is served by the zerops_dev_server process, not the app's own start. Its supervisor starts the dev server again after a restart or redeploy, but a crash leaves the URL down until you start it again. For an always-on service switch to simple mode; this is not a production deployment.", host)
}

// recordVerifyToWorkSession records one service verify result as a WorkSession attempt.
// Pass = summary "healthy" status; fail = first failing check detail.
//
// On failure the FailureClass is populated from the failing check's name —
// `service_running` → FailureClassStart (container not up); HTTP-shape
// checks → FailureClassVerify; everything else → FailureClassVerify as
// the catch-all for verify-time failures.
func recordVerifyToWorkSession(stateDir string, r *ops.VerifyResult) {
	if r == nil {
		return
	}
	attempt := workflow.VerifyAttempt{
		AttemptedAt: time.Now().UTC().Format(time.RFC3339),
	}
	// Auto-close counts a verify as passed when the service is operationally
	// up — healthy, OR degraded only by a cosmetic http_root 4xx (REST API
	// with no `/` route). The verify RESPONSE still reports the degraded
	// status; this only governs the lifecycle gate so a 404-at-/ no longer
	// forces a throwaway root route to auto-close. See PassedForLifecycle.
	passed := r.PassedForLifecycle()
	if passed {
		attempt.Passed = true
		attempt.PassedAt = attempt.AttemptedAt
		if r.Status == statusHealthy {
			attempt.Summary = statusHealthy
		} else {
			attempt.Summary = "reachable (root path not 2xx — accepted as cosmetic)"
		}
	} else {
		attempt.Summary = verifyFailureSummary(r)
		attempt.FailureClass = classifyVerifyFailure(r)
	}
	_ = workflow.RecordVerifyAttempt(stateDir, r.Hostname, attempt)
	if attempt.Passed {
		// Durable verified-setup evidence (F4): the session's Verifies map
		// dies with the session; this sidecar survives so the dev→prod
		// transition can ask "was this setup ever green-verified, when".
		if meta, _ := workflow.FindServiceMeta(stateDir, r.Hostname); meta != nil {
			if setup := meta.SetupNameFor(r.Hostname); setup != "" {
				_ = workflow.RecordVerifiedSetup(stateDir, r.Hostname, workflow.VerifiedSetupEvidence{
					SetupName:      setup,
					TargetHostname: r.Hostname,
					VerifiedAt:     attempt.PassedAt,
					Summary:        attempt.Summary,
				})
			}
		}
	}
}

// classifyVerifyFailure maps the first failing check name to the
// FailureClass that best describes the recovery path. service_running =
// container start issue (deploy may need redo). HTTP-shape checks fall
// under FailureClassVerify (route / app / config). Anything else is
// FailureClassVerify as a coarse catch-all — the Reason text still
// carries the specific check name + detail.
func classifyVerifyFailure(r *ops.VerifyResult) topology.FailureClass {
	for _, c := range r.Checks {
		if c.Status != statusFail {
			continue
		}
		switch c.Name {
		case "service_running":
			return topology.FailureClassStart
		default:
			return topology.FailureClassVerify
		}
	}
	return topology.FailureClassVerify
}

// recordVerifyAllToWorkSession records results for every service verified
// by VerifyAll. Each service appends one attempt.
func recordVerifyAllToWorkSession(stateDir string, r *ops.VerifyAllResult) {
	if r == nil {
		return
	}
	for i := range r.Services {
		recordVerifyToWorkSession(stateDir, &r.Services[i])
	}
}

func verifyFailureSummary(r *ops.VerifyResult) string {
	for _, c := range r.Checks {
		if c.Status == statusFail {
			if c.Detail != "" {
				return fmt.Sprintf("%s: %s", c.Name, c.Detail)
			}
			return c.Name
		}
	}
	return r.Status
}
