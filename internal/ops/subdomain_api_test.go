//go:build api

// API tests for: plans/analysis/ops.md § subdomain
package ops

import "testing"

func TestAPI_Subdomain_EnableDisable(t *testing.T) {
	t.Skip("requires ZCP_API_KEY")
}
