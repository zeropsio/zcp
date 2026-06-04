package ops

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// TestBatchLogChecks_FiltersBenignBootNoise pins the Wave 1-3 recurring-noise
// fix: error-severity container boot artifacts ("Failed to start Commit a
// transient machine-id", "/etc/fstab does not exist") must NOT surface in the
// error_logs detail, while a real app error in the same batch still does.
func TestBatchLogChecks_FiltersBenignBootNoise(t *testing.T) {
	t.Parallel()
	logAccess := &platform.LogAccess{URL: "http://logs.test"}
	fetcher := &callbackLogFetcher{fn: func(params platform.LogFetchParams) ([]platform.LogEntry, error) {
		if params.Severity == "error" {
			return []platform.LogEntry{
				{Message: "Failed to start Commit a transient machine-id on disk."},
				{Message: "panic: runtime error: invalid memory address"},
				{Message: "/etc/fstab does not exist"},
			}, nil
		}
		return nil, nil
	}}

	got := batchLogChecks(context.Background(), fetcher, logAccess, nil, "svc-1")
	if len(got) != 1 {
		t.Fatalf("want 1 check, got %d", len(got))
	}
	c := got[0]
	if c.Status != CheckInfo {
		t.Fatalf("want info (a real error is present), got %s: %q", c.Status, c.Detail)
	}
	if strings.Contains(c.Detail, "machine-id") || strings.Contains(c.Detail, "fstab") {
		t.Errorf("benign boot noise leaked into error_logs detail: %q", c.Detail)
	}
	if !strings.Contains(c.Detail, "panic: runtime error") {
		t.Errorf("real app error dropped from error_logs detail: %q", c.Detail)
	}
}

// TestBatchLogChecks_AllBenign_Passes pins that a batch of ONLY benign boot
// noise yields a clean pass (no real errors), not an info-level noise dump.
func TestBatchLogChecks_AllBenign_Passes(t *testing.T) {
	t.Parallel()
	logAccess := &platform.LogAccess{URL: "http://logs.test"}
	fetcher := &callbackLogFetcher{fn: func(params platform.LogFetchParams) ([]platform.LogEntry, error) {
		if params.Severity == "error" {
			return []platform.LogEntry{
				{Message: "Failed to start Commit a transient machine-id on disk."},
				{Message: "/etc/fstab does not exist"},
			}, nil
		}
		return nil, nil
	}}

	got := batchLogChecks(context.Background(), fetcher, logAccess, nil, "svc-1")
	if len(got) != 1 || got[0].Status != CheckPass {
		t.Fatalf("all-benign batch should PASS (no real errors), got %+v", got)
	}
}
