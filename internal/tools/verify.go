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
	ServiceHostname string `json:"serviceHostname,omitempty" jsonschema:"Hostname of the service to verify. Omit to verify all services."`
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
				return convertError(err), nil, nil
			}
			return jsonResult(result), nil, nil
		}
		result, err := ops.Verify(ctx, client, fetcher, httpClient, projectID, input.ServiceHostname)
		if err != nil {
			return convertError(err), nil, nil
		}
		return jsonResult(result), nil, nil
	})
}
