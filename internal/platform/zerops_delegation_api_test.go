//go:build api

package platform_test

import (
	"testing"

	"github.com/zeropsio/zcp/internal/platform/apitest"
)

// TestAPI_ListOwnTokenDelegations_WellFormed asserts the read-only
// delegation-list call succeeds against the real platform and any returned
// rows parse into the expected shape. MUST NOT assert a specific count — the
// eval token's delegation is legitimately consumed by other live
// verification (mint is one-shot, F4) and by other test runs sharing the
// token, so 0 rows is a valid outcome here.
func TestAPI_ListOwnTokenDelegations_WellFormed(t *testing.T) {
	h := apitest.New(t)
	delegations, err := h.Client().ListOwnTokenDelegations(h.Ctx())
	if err != nil {
		t.Fatalf("ListOwnTokenDelegations failed: %v", err)
	}
	for _, d := range delegations {
		if d.ID == "" {
			t.Error("delegation ID is empty")
		}
		if d.TokenID == "" {
			t.Error("delegation TokenID is empty")
		}
	}
}
