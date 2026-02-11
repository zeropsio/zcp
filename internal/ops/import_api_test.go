//go:build api

// API tests for: plans/analysis/ops.md § import
package ops

import "testing"

func TestAPI_Import_DryRun(t *testing.T) {
	t.Skip("requires ZCP_API_KEY")
}
