package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

func handleCICDComplete(_ context.Context, engine *workflow.Engine, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	if input.Step == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Step is required for cicd complete action",
			"Specify step name (e.g., step=\"choose\")")), nil, nil
	}
	if input.Attestation == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Attestation is required for cicd complete action",
			"Describe what was accomplished")), nil, nil
	}

	// Extract provider from attestation for choose step (look for "Provider:" prefix in WorkflowInput).
	// The provider field is passed via a convention in the attestation or as a separate field.
	provider := ""
	if input.Step == workflow.CICDStepChoose {
		// Try to extract provider from known patterns in attestation.
		for _, p := range []string{workflow.CICDProviderGitHub, workflow.CICDProviderGitLab, workflow.CICDProviderWebhook, workflow.CICDProviderGeneric} {
			if containsCI(input.Attestation, p) {
				provider = p
				break
			}
		}
	}

	resp, err := engine.CICDComplete(input.Step, input.Attestation, provider)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrBootstrapNotActive,
			fmt.Sprintf("CI/CD complete failed: %v", err),
			"Start cicd first with action=start workflow=cicd")), nil, nil
	}
	return jsonResult(resp), nil, nil
}

func handleCICDStatus(_ context.Context, engine *workflow.Engine) (*mcp.CallToolResult, any, error) {
	resp, err := engine.CICDStatus()
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrBootstrapNotActive,
			fmt.Sprintf("CI/CD status failed: %v", err),
			"")), nil, nil
	}
	return jsonResult(resp), nil, nil
}

// containsCI does a case-insensitive contains check.
func containsCI(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		len(s) > 0 && len(substr) > 0 &&
			indexOf(toLower(s), toLower(substr)) >= 0)
}

func toLower(s string) string {
	b := make([]byte, len(s))
	for i := range len(s) {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
