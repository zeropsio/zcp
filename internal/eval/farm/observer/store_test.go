package observer

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm"
)

// fakeObjectStore is a minimal in-memory ObjectStore double — the observer
// package's own equivalent of internal/eval/farm/fakes3_test.go's fakeS3,
// kept local so this package's tests never need a real HTTP server.
type fakeObjectStore struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func newFakeObjectStore() *fakeObjectStore {
	return &fakeObjectStore{objects: map[string][]byte{}}
}

func (f *fakeObjectStore) Get(_ context.Context, key string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, ok := f.objects[key]
	if !ok {
		return nil, farmErrObjectNotFound(key)
	}
	out := make([]byte, len(body))
	copy(out, body)
	return out, nil
}

func (f *fakeObjectStore) Put(_ context.Context, key string, body []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	stored := make([]byte, len(body))
	copy(stored, body)
	f.objects[key] = stored
	return nil
}

func (f *fakeObjectStore) Head(_ context.Context, key string) (bool, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, ok := f.objects[key]
	if !ok {
		return false, 0, nil
	}
	return true, int64(len(body)), nil
}

func (f *fakeObjectStore) List(_ context.Context, prefix string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var keys []string
	for key := range f.objects {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

// farmErrObjectNotFound wraps the real farm.ErrObjectNotFound the way
// SinkClient.Get does (internal/eval/farm/sink.go), so a fakeObjectStore
// miss is indistinguishable — including under errors.Is — from a real
// bucket 404. TestSinkBundle_MissingOptionalFileIsNotRecorded relies on
// that identity: it is what SinkClient.Get actually returns, satisfying
// errors.Is(err, fs.ErrNotExist).
func farmErrObjectNotFound(key string) error {
	return fmt.Errorf("farm: GET %s: %w", key, farm.ErrObjectNotFound)
}

// TestStore_RefusesKeyOutsideObserverPrefix pins §7.6 FM-47: observer code
// writes only keys under runs/<runId>/observer/, for a runId matching the
// FM-47 grammar — refused before any network call. Independent oracle: every
// (runID, key) pair below is hand-built from the spec's own prefix rule and
// FM-47's grammar, never derived from validateObserverKey's own logic.
func TestStore_RefusesKeyOutsideObserverPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		runID string
		key   string
	}{
		{"done.json instead of observer/", "x", "runs/x/done.json"},
		{"results/ instead of observer/", "x", "runs/x/results/a"},
		{"path traversal inside observer/", "x", "runs/x/observer/../done.json"},
		{"key names a different run than the context runId", "x", "runs/other/observer/2026-obs.json"},
		{"empty runId", "", "runs//observer/2026-obs.json"},
		{"runId contains a slash", "x/results", "runs/x/results/observer/2026-obs.json"},
		{"runId contains dot-dot", "a/../b", "runs/a/../b/observer/2026-obs.json"},
		{"runId contains uppercase", "A_B", "runs/A_B/observer/2026-obs.json"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := validateObserverKey(tc.runID, tc.key); err == nil {
				t.Errorf("validateObserverKey(%q, %q) = nil, want an error", tc.runID, tc.key)
			}
		})
	}
}

// TestStore_RefusesOverwrite pins §7.5: "the store never overwrites an
// existing observation key" — a second PutObservation at the same
// runID/obsId fails, and the fake's stored object stays exactly what the
// first Put wrote (the independent oracle here: the raw bytes read back
// off the fake, never Store's own accessor).
func TestStore_RefusesOverwrite(t *testing.T) {
	t.Parallel()

	fake := newFakeObjectStore()
	store := NewStore(fake)
	ctx := context.Background()
	const runID = "final1"

	first := Observation{FormatVersion: ObservationFormat1, RunID: runID, ObsID: "20260911T100000000Z-claude-sonnet-5", Headline: "first"}
	if err := store.PutObservation(ctx, runID, first); err != nil {
		t.Fatalf("first PutObservation: %v", err)
	}

	second := Observation{FormatVersion: ObservationFormat1, RunID: runID, ObsID: first.ObsID, Headline: "second"}
	if err := store.PutObservation(ctx, runID, second); err == nil {
		t.Fatal("second PutObservation = nil error, want a refusal (FM-45: never overwrites)")
	}

	key := "runs/" + runID + "/observer/" + first.ObsID + ".json"
	body, err := fake.Get(ctx, key)
	if err != nil {
		t.Fatalf("fake.Get: %v", err)
	}
	if !strings.Contains(string(body), `"headline":"first"`) {
		t.Errorf("stored object = %s, want the first Put's body (headline=first) untouched", body)
	}
}

// TestStore_CurrentIsNewestObsID pins §7.5: "the newest obsId is the run's
// current observation" — CurrentObservation returns the lexicographically
// last obsId (obsId's leading fixed-width UTC timestamp sorts
// chronologically), regardless of insertion order.
func TestStore_CurrentIsNewestObsID(t *testing.T) {
	t.Parallel()

	fake := newFakeObjectStore()
	store := NewStore(fake)
	ctx := context.Background()
	const runID = "final1"

	oldest := "20260911T090000000Z-claude-sonnet-5"
	middle := "20260911T100000000Z-claude-sonnet-5"
	newest := "20260911T110000000Z-claude-opus-5"

	// Inserted out of chronological order, to prove CurrentObservation
	// sorts rather than trusting insertion/Put order.
	for _, obsID := range []string{middle, oldest, newest} {
		obs := Observation{FormatVersion: ObservationFormat1, RunID: runID, ObsID: obsID}
		if err := store.PutObservation(ctx, runID, obs); err != nil {
			t.Fatalf("PutObservation(%s): %v", obsID, err)
		}
	}

	got, found, err := store.CurrentObservation(ctx, runID)
	if err != nil {
		t.Fatalf("CurrentObservation: %v", err)
	}
	if !found {
		t.Fatal("CurrentObservation found = false, want true")
	}
	if got != newest {
		t.Errorf("CurrentObservation = %q, want %q", got, newest)
	}

	ids, err := store.ListObservations(ctx, runID)
	if err != nil {
		t.Fatalf("ListObservations: %v", err)
	}
	want := []string{oldest, middle, newest}
	if len(ids) != len(want) {
		t.Fatalf("ListObservations = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Errorf("ListObservations = %v, want %v (oldest first)", ids, want)
			break
		}
	}
}

// TestStore_PutThenGetRoundTrips proves PutObservation/GetObservation carry
// an Observation's fields through JSON marshal/unmarshal unchanged — the
// independent oracle is the literal Observation struct built here, compared
// field by field against what GetObservation returns, never a value
// recomputed by Store's own code.
func TestStore_PutThenGetRoundTrips(t *testing.T) {
	t.Parallel()

	fake := newFakeObjectStore()
	store := NewStore(fake)
	ctx := context.Background()
	const runID = "final1-recover-failed-buildfromgit-missing-dep"

	createdAt := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	want := Observation{
		FormatVersion: ObservationFormat1,
		RunID:         runID,
		ObsID:         "20260911T100000000Z-claude-sonnet-5",
		Model:         "claude-sonnet-5",
		CreatedAt:     createdAt,
		DurationMs:    1234,
		CostUsd:       0.05,
		Status:        "ok",
		Headline:      "agent recovered the missing dep",
		Goal:          Goal{Reached: "yes", Why: "final state matches the goal"},
		Checks:        Checks{Verdict: "passed", Agree: true, Why: "checks line up"},
		Findings: []Finding{{
			Severity: "low", Owner: "agent", Title: "minor", What: "small nit",
			Evidence: []Evidence{{Step: 3, Quote: "some quoted text", Verified: true}},
			LookAt:   "step 3", Fix: "n/a",
		}},
		SelfReview: SelfReview{Accurate: "yes", Note: "matches"},
	}

	if err := store.PutObservation(ctx, runID, want); err != nil {
		t.Fatalf("PutObservation: %v", err)
	}

	got, err := store.GetObservation(ctx, runID, want.ObsID)
	if err != nil {
		t.Fatalf("GetObservation: %v", err)
	}

	if got.FormatVersion != want.FormatVersion || got.RunID != want.RunID || got.ObsID != want.ObsID ||
		got.Model != want.Model || !got.CreatedAt.Equal(want.CreatedAt) || got.DurationMs != want.DurationMs ||
		got.CostUsd != want.CostUsd || got.Status != want.Status || got.Headline != want.Headline {
		t.Errorf("GetObservation top-level fields = %+v, want %+v", got, want)
	}
	if got.Goal != want.Goal {
		t.Errorf("GetObservation.Goal = %+v, want %+v", got.Goal, want.Goal)
	}
	if got.Checks != want.Checks {
		t.Errorf("GetObservation.Checks = %+v, want %+v", got.Checks, want.Checks)
	}
	if got.SelfReview != want.SelfReview {
		t.Errorf("GetObservation.SelfReview = %+v, want %+v", got.SelfReview, want.SelfReview)
	}
	if len(got.Findings) != len(want.Findings) {
		t.Fatalf("GetObservation.Findings = %v, want %v", got.Findings, want.Findings)
	}
	for i := range want.Findings {
		if got.Findings[i].Severity != want.Findings[i].Severity || got.Findings[i].Owner != want.Findings[i].Owner ||
			got.Findings[i].Title != want.Findings[i].Title || len(got.Findings[i].Evidence) != len(want.Findings[i].Evidence) {
			t.Errorf("GetObservation.Findings[%d] = %+v, want %+v", i, got.Findings[i], want.Findings[i])
		}
	}
}
