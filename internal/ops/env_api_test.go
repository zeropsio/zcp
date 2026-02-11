//go:build api

// Tests for: plans/analysis/ops.md § ops/env.go (API verification)
package ops

import "testing"

func TestAPI_EnvGet_Service(t *testing.T) {
	t.Skip("requires ZCP_API_KEY")
}

func TestAPI_EnvSet_Delete_Cycle(t *testing.T) {
	t.Skip("requires ZCP_API_KEY")
}
