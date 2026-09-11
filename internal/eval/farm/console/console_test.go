package console

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm"
	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// fakeStore is a minimal in-memory observer.ObjectStore double — this
// package's own equivalent of internal/eval/farm/observer/store_test.go's
// fakeObjectStore, kept local so console's tests never need a real HTTP
// server (per the brief: "httptest + an in-memory store fake").
type fakeStore struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func newFakeStore() *fakeStore {
	return &fakeStore{objects: map[string][]byte{}}
}

var errFakeStoreNotFound = errors.New("fakeStore: object not found")

func (f *fakeStore) Get(_ context.Context, key string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, ok := f.objects[key]
	if !ok {
		return nil, fmt.Errorf("%w: %s", errFakeStoreNotFound, key)
	}
	out := make([]byte, len(body))
	copy(out, body)
	return out, nil
}

func (f *fakeStore) Put(_ context.Context, key string, body []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	stored := make([]byte, len(body))
	copy(stored, body)
	f.objects[key] = stored
	return nil
}

func (f *fakeStore) Head(_ context.Context, key string) (bool, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, ok := f.objects[key]
	if !ok {
		return false, 0, nil
	}
	return true, int64(len(body)), nil
}

func (f *fakeStore) List(_ context.Context, prefix string) ([]string, error) {
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

func (f *fakeStore) putJSON(t *testing.T, key string, v any) {
	t.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %s: %v", key, err)
	}
	if err := f.Put(context.Background(), key, body); err != nil {
		t.Fatalf("put %s: %v", key, err)
	}
}

func (f *fakeStore) putText(t *testing.T, key, body string) {
	t.Helper()
	if err := f.Put(context.Background(), key, []byte(body)); err != nil {
		t.Fatalf("put %s: %v", key, err)
	}
}

// --- Fixtures: a batch + run bundle shaped exactly like the real farm
// bucket layout (docs/spec-eval-farm.md §1.1) — an independent oracle for
// every read-model test (values are hand-picked literals, never derived
// from the code under test).

const (
	testResultsTS = "20260911-104803"
)

// runFixture describes one run to seed into a fakeStore via seedBatch.
type runFixture struct {
	runID      string
	scenario   string
	startedAt  time.Time
	durationS  string // eval.Duration wire form, e.g. "10s"
	costUsd    float64
	taskResult string // ""=no task field; else "passed"|"failed"|"blocked"
	done       bool   // false => no done.json, no results/ (still "started")
	// checks: id -> {result, expected, observed, source}
	checks [][5]string
	// selfReview is optional; "" omits self-review.md entirely.
	selfReview string
}

// seedBatch writes batches/<batch>/manifest.json (+ summary.json when
// withSummary), plus every fixture run's bundle, into store.
func seedBatch(t *testing.T, store *fakeStore, batch, observerModel string, runs []runFixture, withSummary bool, summaryResults map[string]string) {
	t.Helper()

	manifest := farm.BatchManifest{
		Batch: batch, CreatedAt: "2026-09-01T00:00:00Z", StartedAt: "2026-09-01T00:00:00Z",
		Set: "gate", CandidateSha256: "cand-sha", EvaluatorSha256: "eval-sha",
		ScenariosDigest: "scn-sha", Observer: observerModel,
	}
	for _, rf := range runs {
		manifest.Runs = append(manifest.Runs, farm.ManifestRun{
			RunID: rf.runID, Scenario: rf.scenario, ProjectName: "zcp-farm-" + rf.runID,
		})
	}
	store.putJSON(t, "batches/"+batch+"/manifest.json", manifest)

	if withSummary {
		summary := farm.BatchSummary{Batch: batch, FinishedAt: "2026-09-01T01:00:00Z", EndedBy: "settled"}
		for _, rf := range runs {
			result := summaryResults[rf.runID]
			if result == "" {
				continue
			}
			summary.Runs = append(summary.Runs, farm.SummaryRun{RunID: rf.runID, Scenario: rf.scenario, Result: result})
		}
		store.putJSON(t, "batches/"+batch+"/summary.json", summary)
	}

	for _, rf := range runs {
		seedRun(t, store, rf)
	}
}

// seedRun writes one run's started.json/results/.../done.json.
func seedRun(t *testing.T, store *fakeStore, rf runFixture) {
	t.Helper()
	store.putJSON(t, "runs/"+rf.runID+"/started.json", map[string]any{
		"runId": rf.runID, "scenarioId": rf.scenario, "startedAt": rf.startedAt.UTC().Format(time.RFC3339),
	})
	if !rf.done {
		return
	}

	resultsDir := fmt.Sprintf("runs/%s/results/%s/%s", rf.runID, testResultsTS, rf.scenario)
	store.putText(t, resultsDir+"/task-prompt.txt", "do the thing for "+rf.scenario)
	store.putText(t, resultsDir+"/transcript.jsonl", fixtureTranscript())
	store.putText(t, resultsDir+"/self-review.md", rf.selfReview)

	meta := map[string]any{
		"scenarioId": rf.scenario, "suiteId": "gate", "mode": "two-shot-resume",
		"startedAt": rf.startedAt.UTC().Format(time.RFC3339Nano), "duration": rf.durationS,
		"evaluatorSha256": "eval-sha", "candidateSha256": "cand-sha",
		"usage": map[string]any{"totalCostUsd": rf.costUsd},
	}
	if rf.taskResult != "" {
		meta["task"] = map[string]any{"mode": "required", "result": rf.taskResult, "frozenAt": rf.startedAt.UTC().Format(time.RFC3339Nano)}
	}
	store.putJSON(t, resultsDir+"/meta.json", meta)

	var checks []map[string]any
	for _, c := range rf.checks {
		checks = append(checks, map[string]any{
			"id": c[0], "check": "x", "scope": "y", "result": c[1],
			"expected": c[2], "observed": c[3], "observedAt": rf.startedAt.UTC().Format(time.RFC3339Nano), "source": c[4],
		})
	}
	store.putJSON(t, resultsDir+"/verification.json", map[string]any{
		"formatVersion": "zcp-eval-verification-2", "mode": "required",
		"result": rf.taskResult, "frozenAt": rf.startedAt.UTC().Format(time.RFC3339Nano),
		"checks": checks, "advisory": []any{},
	})

	store.putJSON(t, "runs/"+rf.runID+"/done.json", map[string]any{
		"runId": rf.runID, "scenarioId": rf.scenario,
		"runnerDimensions": map[string]any{"execution": "ok", "task": "required " + rf.taskResult, "taskEnd": "persisted, settled"},
	})
}

// fixtureTranscript is a minimal, valid Claude Code stream-json transcript:
// one assistant text block (step "agent"), one assistant tool_use block
// (step "tool") with a matching tool_result on the following user event.
// BuildSteps (§7.2) numbers this, with the synthesized task-prompt step 1,
// as: #1 user, #2 agent, #3 tool.
func fixtureTranscript() string {
	lines := []string{
		`{"type":"system","subtype":"init"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"looking into it"},{"type":"tool_use","id":"tu1","name":"zerops_discover","input":{"project":"p1"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu1","content":[{"type":"text","text":"discovered ok"}]}]}}`,
	}
	return strings.Join(lines, "\n") + "\n"
}

// seedObservation stores one observation for runID via observer.Store, so
// it is retrievable through the same path production code uses.
func seedObservation(t *testing.T, store *fakeStore, obs observer.Observation) {
	t.Helper()
	if err := observer.NewStore(store).PutObservation(context.Background(), obs.RunID, obs); err != nil {
		t.Fatalf("seed observation: %v", err)
	}
}

// testFixedTime is every test's shared "now" — a fixed instant so window
// math (§8.4) is deterministic.
const testFixedTime = "2026-09-11T12:00:00Z"

func fixedNow(t *testing.T) func() time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, testFixedTime)
	if err != nil {
		t.Fatalf("parse fixed now %q: %v", testFixedTime, err)
	}
	return func() time.Time { return ts }
}

// testServer builds a Server + fakeStore wired for a fast test: testToken
// as the console token, a tiny LoginFailDelay and a recording Sleep so the
// ">=1s per failure" mechanism (FM-50) is exercised without a real sleep.
// A test needing a different Config (e.g. token rotation) builds its own
// Server directly instead of calling this helper.
func testServer(t *testing.T) (*Server, *fakeStore, *[]time.Duration) {
	t.Helper()
	store := newFakeStore()
	var sleeps []time.Duration
	cfg := Config{
		Store: store, Token: testToken,
		Now:            fixedNow(t),
		LoginFailDelay: time.Millisecond,
		Sleep:          func(d time.Duration) { sleeps = append(sleeps, d) },
	}
	return NewServer(cfg), store, &sleeps
}

// TestFileCache_EvictsOldestPastByteBudget pins the files/ passthrough
// cache's bound: a long-lived console must not grow without limit, so past
// the byte budget the oldest entries go first.
func TestFileCache_EvictsOldestPastByteBudget(t *testing.T) {
	t.Parallel()
	c := newFileCacheWithBudget(10)
	c.set("a", []byte("1234"))
	c.set("b", []byte("5678"))
	c.set("c", []byte("90ab")) // 12 bytes > 10: "a" must go
	if _, ok := c.get("a"); ok {
		t.Error(`"a" still cached past the byte budget, want evicted first`)
	}
	for _, k := range []string{"b", "c"} {
		if _, ok := c.get(k); !ok {
			t.Errorf("%q evicted, want kept", k)
		}
	}
	c.set("big", make([]byte, 11)) // larger than the whole budget: never cached
	if _, ok := c.get("big"); ok {
		t.Error("an entry larger than the whole budget was cached")
	}
}
