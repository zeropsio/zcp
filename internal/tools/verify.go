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
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// VerifyInput is the input type for zerops_verify.
type VerifyInput struct {
	ServiceHostname string `json:"serviceHostname,omitempty" jsonschema:"Hostname of the service to verify. Omit to verify all services."`
}

// RegisterVerify registers the zerops_verify tool.
func RegisterVerify(srv *mcp.Server, client platform.Client, fetcher platform.LogFetcher, projectID, stateDir string) {
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
			result, err := ops.VerifyAll(ctx, client, fetcher, httpClient, projectID)
			if err != nil {
				return convertError(err, WithRecoveryStatus()), nil, nil
			}
			recordVerifyAllToWorkSession(stateDir, result)
			return jsonResult(verifyAllResponse{
				VerifyAllResult:  result,
				WorkSessionState: sessionAnnotations(stateDir),
			}), nil, nil
		}
		// Phase 4: if the input hostname is a push source whose builds land
		// on a different runtime (dev half of a standard pair under
		// git-push), verify against the BUILD TARGET instead. Self/cross
		// (auto close-mode) modes resolve to self → no redirect.
		// Recorded under the build-target hostname so the work session's
		// auto-close gate (which evaluates resolved targets after Phase 2)
		// sees the green tick on the right key.
		host := input.ServiceHostname
		var redirectedFrom string
		if buildHost, _ := resolveBuildTargetForHost(stateDir, host); buildHost != "" && buildHost != host {
			redirectedFrom = host
			host = buildHost
		}
		result, err := ops.Verify(ctx, client, fetcher, httpClient, projectID, host)
		if err != nil {
			return convertError(err, WithRecoveryStatus()), nil, nil
		}
		recordVerifyToWorkSession(stateDir, result)
		resp := verifyResponse{
			VerifyResult:     result,
			WorkSessionState: sessionAnnotations(stateDir),
		}
		if redirectedFrom != "" {
			resp.Note = fmt.Sprintf("verify: input serviceHostname=%q is a push source; verified build target %q instead (git-push standard pair builds on stage). Pass the build-target hostname directly next time.", redirectedFrom, host)
		}
		// RC-A′: a passing verify on a deferred-start (dev-mode dynamic)
		// service reflects a LIVE dev-server process, not a durable state —
		// the URL 502s after a container cycle. Annotate so the agent does
		// not report it as durably shipped (the e2 failure).
		if note := deferredStartDurabilityNote(stateDir, host, result); note != "" {
			if resp.Note != "" {
				resp.Note += " "
			}
			resp.Note += note
		}
		return jsonResult(resp), nil, nil
	})
}

// verifyResponse wraps ops.VerifyResult with the structured
// WorkSessionState lifecycle signal (F5 closure). Surfacing the
// session state turns verify from a pure HTTP probe into an observable
// lifecycle event — the agent sees whether this call landed inside an
// open session, on an auto-closed one, or with no session tracking.
//
// Note carries any user-facing hint about the verify call's routing —
// Phase 4 uses it to surface that the input hostname was redirected to
// the build target (e.g. dev → stage under git-push) so the agent learns
// the right hostname for next time.
type verifyResponse struct {
	*ops.VerifyResult
	Note             string            `json:"note,omitempty"`
	WorkSessionState *WorkSessionState `json:"workSessionState,omitempty"`
}

// verifyAllResponse mirrors verifyResponse for the multi-service VerifyAll
// path. Each service's attempt is already recorded; one session-state
// snapshot at the response root reflects the whole scope.
type verifyAllResponse struct {
	*ops.VerifyAllResult
	WorkSessionState *WorkSessionState `json:"workSessionState,omitempty"`
}

// deferredStartDurabilityNote returns a durability caveat when a passing
// verify reflects a deferred-start runtime — a dev-mode dynamic service whose
// app runs ONLY via the ephemeral zerops_dev_server (RC-A′). For those, an
// HTTP 200 is a live-process snapshot, not proof of a durable supervised
// state: the URL 502s after a container cycle. Returns "" for non-deferred or
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
	return fmt.Sprintf("Durability: %q is dev-mode — this 200 is served by the zerops_dev_server process, NOT a supervised app. The URL 502s after any container cycle until you restart the dev server. For an always-on service switch to simple mode; this is not a durable deployment.", host)
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
	passed := r.Status == statusHealthy
	if passed {
		attempt.Passed = true
		attempt.PassedAt = attempt.AttemptedAt
		attempt.Summary = statusHealthy
	} else {
		attempt.Summary = verifyFailureSummary(r)
		attempt.FailureClass = classifyVerifyFailure(r)
	}
	_ = workflow.RecordVerifyAttempt(stateDir, r.Hostname, attempt)
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
