// Tests for: internal/platform/mock.go MockLogFetcher filter fidelity.
//
// The mock must apply the same filters the real ZeropsLogFetcher applies
// post-fetch, and must simulate the server-side filters (Severity, Facility,
// Tags, ContainerID) as if the backend had filtered. Without this, consumer
// unit tests that use the mock cannot actually verify filter behaviour —
// they pass regardless of whether the production code sets Since etc.
package platform

import (
	"context"
	"testing"
	"time"
)

// --- server-side-simulated filters ----------------------------------------

func TestMockLogFetcher_SeverityFilter(t *testing.T) {
	t.Parallel()

	f := NewMockLogFetcher().WithEntries([]LogEntry{
		{ID: "1", Timestamp: "2026-01-01T00:00:00Z", Severity: "Informational", Message: "info"},
		{ID: "2", Timestamp: "2026-01-01T00:00:01Z", Severity: "Warning", Message: "warn"},
		{ID: "3", Timestamp: "2026-01-01T00:00:02Z", Severity: "Error", Message: "err"},
		{ID: "4", Timestamp: "2026-01-01T00:00:03Z", Severity: "Debug", Message: "debug"},
	})

	entries, err := f.FetchLogs(context.Background(), &LogAccess{}, LogFetchParams{Severity: "warning"})
	if err != nil {
		t.Fatalf("FetchLogs: %v", err)
	}
	// warning = severity 4 → keep severity <= 4 (Warning, Error, + Emergency..Critical).
	// Informational (6) and Debug (7) should be dropped.
	ids := idsOf(entries)
	if !equalStrings(ids, []string{"2", "3"}) {
		t.Errorf("severity filter ids = %v, want [2 3]", ids)
	}
}

func TestMockLogFetcher_FacilityFilter(t *testing.T) {
	t.Parallel()

	f := NewMockLogFetcher().WithEntries([]LogEntry{
		{ID: "1", Timestamp: "2026-01-01T00:00:00Z", Severity: "Informational", Facility: "local0", Message: "app"},
		{ID: "2", Timestamp: "2026-01-01T00:00:01Z", Severity: "Informational", Facility: "daemon", Message: "daemon"},
		{ID: "3", Timestamp: "2026-01-01T00:00:02Z", Severity: "Informational", Facility: "local1", Message: "webserver"},
	})

	entries, err := f.FetchLogs(context.Background(), &LogAccess{}, LogFetchParams{Facility: "application"})
	if err != nil {
		t.Fatalf("FetchLogs: %v", err)
	}
	if ids := idsOf(entries); !equalStrings(ids, []string{"1"}) {
		t.Errorf("application-facility ids = %v, want [1]", ids)
	}

	entries, _ = f.FetchLogs(context.Background(), &LogAccess{}, LogFetchParams{Facility: "webserver"})
	if ids := idsOf(entries); !equalStrings(ids, []string{"3"}) {
		t.Errorf("webserver-facility ids = %v, want [3]", ids)
	}

	entries, _ = f.FetchLogs(context.Background(), &LogAccess{}, LogFetchParams{Facility: ""})
	if ids := idsOf(entries); !equalStrings(ids, []string{"1", "2", "3"}) {
		t.Errorf("no-facility ids = %v, want [1 2 3]", ids)
	}
}

func TestMockLogFetcher_TagsFilter(t *testing.T) {
	t.Parallel()

	f := NewMockLogFetcher().WithEntries([]LogEntry{
		{ID: "1", Timestamp: "2026-01-01T00:00:00Z", Tag: "zbuilder@A", Message: "build A"},
		{ID: "2", Timestamp: "2026-01-01T00:00:01Z", Tag: "zbuilder@B", Message: "build B"},
		{ID: "3", Timestamp: "2026-01-01T00:00:02Z", Tag: "other", Message: "other"},
	})

	// Single tag.
	entries, _ := f.FetchLogs(context.Background(), &LogAccess{}, LogFetchParams{Tags: []string{"zbuilder@B"}})
	if ids := idsOf(entries); !equalStrings(ids, []string{"2"}) {
		t.Errorf("single-tag ids = %v, want [2]", ids)
	}

	// Multiple tags — OR semantics.
	entries, _ = f.FetchLogs(context.Background(), &LogAccess{}, LogFetchParams{Tags: []string{"zbuilder@A", "zbuilder@B"}})
	if ids := idsOf(entries); !equalStrings(ids, []string{"1", "2"}) {
		t.Errorf("multi-tag ids = %v, want [1 2]", ids)
	}

	// Empty Tags omits filter.
	entries, _ = f.FetchLogs(context.Background(), &LogAccess{}, LogFetchParams{Tags: nil})
	if ids := idsOf(entries); !equalStrings(ids, []string{"1", "2", "3"}) {
		t.Errorf("no-tag ids = %v, want [1 2 3]", ids)
	}
}

func TestMockLogFetcher_ContainerIDFilter(t *testing.T) {
	t.Parallel()

	f := NewMockLogFetcher().WithEntries([]LogEntry{
		{ID: "1", Timestamp: "2026-01-01T00:00:00Z", ContainerID: "cont-A", Message: "A"},
		{ID: "2", Timestamp: "2026-01-01T00:00:01Z", ContainerID: "cont-B", Message: "B"},
	})

	entries, _ := f.FetchLogs(context.Background(), &LogAccess{}, LogFetchParams{ContainerID: "cont-A"})
	if ids := idsOf(entries); !equalStrings(ids, []string{"1"}) {
		t.Errorf("containerId filter ids = %v, want [1]", ids)
	}
}

// --- client-side filters --------------------------------------------------

func TestMockLogFetcher_SinceFilter(t *testing.T) {
	t.Parallel()

	since := time.Date(2026, 4, 22, 6, 4, 29, 440767629, time.UTC)
	f := NewMockLogFetcher().WithEntries([]LogEntry{
		{ID: "a", Timestamp: "2026-04-22T06:04:29Z", Severity: "info", Message: "x"},
		{ID: "b", Timestamp: "2026-04-22T06:04:29.1Z", Severity: "info", Message: "x"},
		{ID: "c", Timestamp: "2026-04-22T06:04:29.5Z", Severity: "info", Message: "x"},
		{ID: "d", Timestamp: "2026-04-22T06:04:30.000001Z", Severity: "info", Message: "x"},
	})

	entries, _ := f.FetchLogs(context.Background(), &LogAccess{}, LogFetchParams{Since: since})
	if ids := idsOf(entries); !equalStrings(ids, []string{"c", "d"}) {
		t.Errorf("since filter ids = %v, want [c d] (parse-compare, not lex)", ids)
	}
}

func TestMockLogFetcher_SearchFilter(t *testing.T) {
	t.Parallel()

	f := NewMockLogFetcher().WithEntries([]LogEntry{
		{ID: "1", Timestamp: "2026-01-01T00:00:00Z", Message: "foo happens"},
		{ID: "2", Timestamp: "2026-01-01T00:00:01Z", Message: "nothing"},
		{ID: "3", Timestamp: "2026-01-01T00:00:02Z", Message: "foo again"},
	})

	entries, _ := f.FetchLogs(context.Background(), &LogAccess{}, LogFetchParams{Search: "foo"})
	if ids := idsOf(entries); !equalStrings(ids, []string{"1", "3"}) {
		t.Errorf("search filter ids = %v, want [1 3]", ids)
	}
}

// --- limit ----------------------------------------------------------------

func TestMockLogFetcher_LimitTailTrim(t *testing.T) {
	t.Parallel()

	entries := make([]LogEntry, 10)
	for i := range entries {
		entries[i] = LogEntry{
			ID:        string(rune('a' + i)),
			Timestamp: time.Date(2026, 1, 1, 0, 0, i, 0, time.UTC).Format(time.RFC3339),
			Message:   "x",
		}
	}
	f := NewMockLogFetcher().WithEntries(entries)

	got, _ := f.FetchLogs(context.Background(), &LogAccess{}, LogFetchParams{Limit: 3})
	if len(got) != 3 {
		t.Fatalf("Limit=3 returned %d entries, want 3", len(got))
	}
	// Tail = newest = ids h, i, j.
	wantIDs := []string{"h", "i", "j"}
	if ids := idsOf(got); !equalStrings(ids, wantIDs) {
		t.Errorf("tail-trim ids = %v, want %v", ids, wantIDs)
	}
}

func TestMockLogFetcher_Error(t *testing.T) {
	t.Parallel()

	f := NewMockLogFetcher().WithError(&PlatformError{Code: ErrAPIError, Message: "boom"})
	_, err := f.FetchLogs(context.Background(), &LogAccess{}, LogFetchParams{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --- combined --------------------------------------------------------------

// TestMockLogFetcher_CombinedFilters_Mirrors_FetchBuildWarnings_Shape pins the
// exact shape FetchBuildWarnings will use in Phase 3. Add this now so Phase 3
// consumer tests can rely on the mock returning the semantically correct set.
func TestMockLogFetcher_CombinedFilters_Mirrors_FetchBuildWarnings_Shape(t *testing.T) {
	t.Parallel()

	f := NewMockLogFetcher().WithEntries([]LogEntry{
		// Stale build's warning — must be dropped by tag filter.
		{ID: "stale", Timestamp: "2026-04-22T06:00:00Z", Severity: "Warning", Facility: "local0", Tag: "zbuilder@OLD", Message: "stale warning from previous build"},
		// Current build's warning — must be kept.
		{ID: "fresh", Timestamp: "2026-04-22T06:04:35Z", Severity: "Warning", Facility: "local0", Tag: "zbuilder@NEW", Message: "fresh warning"},
		// Current build's info — dropped by severity filter.
		{ID: "info", Timestamp: "2026-04-22T06:04:36Z", Severity: "Informational", Facility: "local0", Tag: "zbuilder@NEW", Message: "info log"},
		// Daemon noise — dropped by facility filter.
		{ID: "noise", Timestamp: "2026-04-22T06:04:37Z", Severity: "Warning", Facility: "daemon", Tag: "sshfs", Message: "sshfs: no such mount point"},
	})

	entries, _ := f.FetchLogs(context.Background(), &LogAccess{}, LogFetchParams{
		Severity: "warning",
		Facility: "application",
		Tags:     []string{"zbuilder@NEW"},
		Limit:    100,
	})
	if ids := idsOf(entries); !equalStrings(ids, []string{"fresh"}) {
		t.Errorf("combined-filter ids = %v, want [fresh]", ids)
	}
}

func idsOf(entries []LogEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.ID
	}
	return out
}
