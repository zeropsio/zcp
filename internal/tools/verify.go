package tools

import (
	"context"
	"crypto/tls"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// VerifyInput is the input type for zerops_verify.
type VerifyInput struct {
	ServiceHostname string `json:"serviceHostname" jsonschema:"Hostname of the service to verify."`
}

// RegisterVerify registers the zerops_verify tool.
func RegisterVerify(srv *mcp.Server, client platform.Client, fetcher platform.LogFetcher, projectID string) {
	httpClient := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_verify",
		Description: "Run health verification checks on a service. Returns structured check results: service status, error logs, startup detection, HTTP health, and /status endpoint connectivity. For runtime services: 6 checks. For managed services (DB, cache): 1 check (service_running only).",
		Annotations: &mcp.ToolAnnotations{
			Title:          "Verify service health",
			ReadOnlyHint:   true,
			IdempotentHint: true,
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input VerifyInput) (*mcp.CallToolResult, any, error) {
		result, err := ops.Verify(ctx, client, fetcher, httpClient, projectID, input.ServiceHostname)
		if err != nil {
			return convertError(err), nil, nil
		}
		return jsonResult(result), nil, nil
	})
}
