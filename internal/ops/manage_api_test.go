//go:build api

// Tests for: plans/analysis/ops.md § ops/manage.go (API verification)
package ops

import "testing"

func TestAPI_Restart_RunningService(t *testing.T) {
	t.Skip("requires ZCP_API_KEY")
}

func TestAPI_Scale_Service(t *testing.T) {
	t.Skip("requires ZCP_API_KEY")
}
