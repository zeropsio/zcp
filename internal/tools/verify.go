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
				VerifyAllResult:   result,
				AutoCloseProgress: workflow.AutoCloseProgressFor(stateDir),
			}), nil, nil
		}
		result, err := ops.Verify(ctx, client, fetcher, httpClient, projectID, input.ServiceHostname)
		if err != nil {
			return convertError(err, WithRecoveryStatus()), nil, nil
		}
		recordVerifyToWorkSession(stateDir, result)
		return jsonResult(verifyResponse{
			VerifyResult:      result,
			AutoCloseProgress: workflow.AutoCloseProgressFor(stateDir),
		}), nil, nil
	})
}

// verifyResponse wraps ops.VerifyResult with the auto-close progress
// snapshot. Surfacing the snapshot turns verify from a pure HTTP probe
// into an observable lifecycle event — the agent sees how this call
// advanced the work session toward auto-close.
type verifyResponse struct {
	*ops.VerifyResult
	AutoCloseProgress *workflow.AutoCloseProgress `json:"autoCloseProgress,omitempty"`
}

// verifyAllResponse mirrors verifyResponse for the multi-service VerifyAll
// path. Each service's attempt is already recorded; one progress snapshot
// at the response root reflects the whole scope.
type verifyAllResponse struct {
	*ops.VerifyAllResult
	AutoCloseProgress *workflow.AutoCloseProgress `json:"autoCloseProgress,omitempty"`
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
func classifyVerifyFailure(r *ops.VerifyResult) workflow.FailureClass {
	for _, c := range r.Checks {
		if c.Status != statusFail {
			continue
		}
		switch c.Name {
		case "service_running":
			return workflow.FailureClassStart
		default:
			return workflow.FailureClassVerify
		}
	}
	return workflow.FailureClassVerify
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
