// Tests for: plans/analysis/platform.md §8 — auth.Resolve API contract
//go:build api

package auth

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestAPI_Resolve_FullFlow verifies auth.Resolve works with a real Zerops API token.
// Requires ZCP_API_KEY env var set to a valid PAT.
func TestAPI_Resolve_FullFlow(t *testing.T) {
	token := os.Getenv("ZCP_API_KEY")
	if token == "" {
		t.Skip("ZCP_API_KEY not set")
	}

	// This test needs a real platform.Client (ZeropsClient).
	// Since ZeropsClient is not yet available as a dependency here,
	// we validate via the mock path that proves the contract.
	// The real API test will be enabled once platform.NewZeropsClient is available.
	t.Skip("requires platform.NewZeropsClient — enable after Task 6")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_ = ctx
}
