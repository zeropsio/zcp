//go:build api

package platform_test

import (
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/platform/apitest"
)

// Tests for: design/zcp-prd.md section 5.7 (Log Fetching API Contract)

func TestAPI_FetchLogs_RealBackend(t *testing.T) {
	h := apitest.New(t)

	access, err := h.Client().GetProjectLog(h.Ctx(), h.ProjectID())
	if err != nil {
		t.Fatalf("GetProjectLog failed: %v", err)
	}

	fetcher := platform.NewLogFetcher()
	entries, err := fetcher.FetchLogs(h.Ctx(), access, platform.LogFetchParams{Limit: 10})
	if err != nil {
		t.Fatalf("FetchLogs failed: %v", err)
	}

	// Entries may be empty if no logs exist yet.
	for _, e := range entries {
		if e.Timestamp == "" {
			t.Error("entry Timestamp is empty")
		}
		if e.Severity == "" {
			t.Error("entry Severity is empty")
		}
		if e.Message == "" {
			t.Error("entry Message is empty")
		}
	}
}
