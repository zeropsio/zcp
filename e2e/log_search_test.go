//go:build e2e

// Tests for: e2e — log search timeout fix.
//
// Verifies that zerops_logs with search param completes without timeout.
// The log proxy backend does full-text scan which is slower than plain log fetch.
//
// Prerequisites:
//   - ZCP_API_KEY set
//   - existing zcpx service in the project
//
// Run: go test ./e2e/ -tags e2e -run TestE2E_LogSearch -v -timeout 300s

package e2e_test

import (
	"strings"
	"testing"
)

func TestE2E_LogSearch(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	const hostname = "zcpx"

	tests := []struct {
		name   string
		args   map[string]any
		wantOK bool // expect no MCP error
	}{
		{
			name: "basic log fetch without search",
			args: map[string]any{
				"serviceHostname": hostname,
				"since":           "10m",
			},
			wantOK: true,
		},
		{
			name: "log fetch with search term",
			args: map[string]any{
				"serviceHostname": hostname,
				"search":          "zerops",
				"since":           "30m",
			},
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := s.callTool("zerops_logs", tt.args)
			text := getE2ETextContent(t, result)

			if tt.wantOK && result.IsError {
				if strings.Contains(text, "API_TIMEOUT") || strings.Contains(text, "deadline exceeded") {
					t.Fatalf("log search timed out (this is the bug): %s", text)
				}
				t.Fatalf("zerops_logs returned unexpected error: %s", text)
			}

			t.Logf("  Response length: %d chars", len(text))
			t.Logf("  Preview: %.200s", text)
		})
	}
}
