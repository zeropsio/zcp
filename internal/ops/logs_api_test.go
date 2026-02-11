//go:build api

// Tests for: plans/analysis/ops.md § ops/logs.go (API verification)
package ops

import "testing"

func TestAPI_FetchLogs(t *testing.T) {
	t.Skip("requires ZCP_API_KEY")
}
