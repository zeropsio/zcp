package farm

import (
	"context"
	"testing"
	"time"
)

// seedFinishedBatch writes a finished batch (manifest.json + summary.json)
// with one run whose done.json is present iff hasDone — the minimal bucket
// state GC reads to classify a zcp-farm-<runID> project.
func seedFinishedBatch(t *testing.T, sink *SinkClient, batch, runID string, hasDone bool) {
	t.Helper()
	ctx := context.Background()
	manifest := BatchManifest{
		Batch: batch, Set: "gate",
		Runs: []ManifestRun{{RunID: runID, Scenario: runID, ProjectName: ProjectPrefix + runID}},
	}
	if err := PutManifest(ctx, sink, batch, manifest); err != nil {
		t.Fatalf("PutManifest: %v", err)
	}
	if hasDone {
		if err := sink.Put(ctx, "runs/"+runID+"/done.json", []byte(`{"runId":"`+runID+`"}`)); err != nil {
			t.Fatalf("Put done.json: %v", err)
		}
	}
	summary := BatchSummary{
		Batch: batch, FinishedAt: time.Now().UTC().Format(time.RFC3339), EndedBy: "settled",
		Runs: []SummaryRun{{RunID: runID, Scenario: runID}},
	}
	if err := PutSummary(ctx, sink, batch, summary); err != nil {
		t.Fatalf("PutSummary: %v", err)
	}
}

// seedRunningBatch writes a batch with a manifest but no summary — a
// "running" batch per §3.6 FM-26.
func seedRunningBatch(t *testing.T, sink *SinkClient, batch, runID string) {
	t.Helper()
	manifest := BatchManifest{
		Batch: batch, Set: "gate",
		Runs: []ManifestRun{{RunID: runID, Scenario: runID, ProjectName: ProjectPrefix + runID}},
	}
	if err := PutManifest(context.Background(), sink, batch, manifest); err != nil {
		t.Fatalf("PutManifest: %v", err)
	}
}

// TestFarmGC_ForeignPrefix_NeverListed is the spec-named test
// (docs/spec-eval-farm.md §3.2, FM-19): a fake project list carrying
// zcp-farm-a1, zcp-telemetry, eval, and zcpfarm-x (no hyphen — not the real
// prefix) yields exactly one candidate, zcp-farm-a1; with --yes (GCApply),
// exactly one DELETE.
func TestFarmGC_ForeignPrefix_NeverListed(t *testing.T) {
	t.Parallel()
	const clientID = "client-gc-1"
	f := newControllerFixture(t, clientID)
	account, client, sink := f.account, f.client, f.sink

	account.seedProject("zcp-telemetry")
	account.seedProject("eval")
	account.seedProject("zcpfarm-x")
	account.seedProject(ProjectPrefix + "a1")
	seedFinishedBatch(t, sink, "batch-gc-1", "a1", true)

	candidates, err := GC(context.Background(), client, sink, GCOptions{ClientID: clientID})
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates = %+v, want exactly 1 (zcp-farm-a1)", candidates)
	}
	if candidates[0].Name != ProjectPrefix+"a1" || candidates[0].Exempt != "" {
		t.Errorf("candidates[0] = %+v, want Name=%s Exempt=\"\"", candidates[0], ProjectPrefix+"a1")
	}

	if errs := GCApply(context.Background(), client, candidates); len(errs) != 0 {
		t.Fatalf("GCApply errors: %v", errs)
	}
	if n := account.countMethod("DELETE", "/api/rest/public/project/"+candidates[0].ProjectID); n != 1 {
		t.Errorf("DELETE calls for %s = %d, want 1", candidates[0].ProjectID, n)
	}
	account.mu.Lock()
	defer account.mu.Unlock()
	for _, name := range []string{"zcp-telemetry", "eval", "zcpfarm-x"} {
		if !findProjectByFakeName(account, name) {
			t.Errorf("foreign-prefixed project %q was deleted, want untouched", name)
		}
	}
}

// TestFarmGC_NoDoneJSON_Exempt pins §3.6 FM-26: a run whose bucket has no
// done.json (its project was kept under FM-21's exemption) stays exempt
// from gc too — gc never deletes a no-bundle run's project on a timer
// alone.
func TestFarmGC_NoDoneJSON_Exempt(t *testing.T) {
	t.Parallel()
	const clientID = "client-gc-2"
	f := newControllerFixture(t, clientID)
	account, client, sink := f.account, f.client, f.sink

	account.seedProject(ProjectPrefix + "stuck1")
	seedFinishedBatch(t, sink, "batch-gc-2", "stuck1", false /* no done.json */)

	candidates, err := GC(context.Background(), client, sink, GCOptions{ClientID: clientID})
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates = %+v, want exactly 1", candidates)
	}
	if candidates[0].Exempt != DetailNoBundle {
		t.Errorf("candidates[0].Exempt = %q, want %q", candidates[0].Exempt, DetailNoBundle)
	}

	if errs := GCApply(context.Background(), client, candidates); len(errs) != 0 {
		t.Fatalf("GCApply errors: %v", errs)
	}
	account.mu.Lock()
	defer account.mu.Unlock()
	if !findProjectByFakeName(account, ProjectPrefix+"stuck1") {
		t.Errorf("exempt project %s was deleted", ProjectPrefix+"stuck1")
	}
}

// TestFarmGC_RunningBatchReferenced_Skipped pins §3.6 FM-26: a project
// belonging to a batch that has a manifest but no summary yet ("running")
// is skipped — gc never deletes a project a live batch might still need.
func TestFarmGC_RunningBatchReferenced_Skipped(t *testing.T) {
	t.Parallel()
	const clientID = "client-gc-3"
	f := newControllerFixture(t, clientID)
	account, client, sink := f.account, f.client, f.sink

	account.seedProject(ProjectPrefix + "inflight1")
	seedRunningBatch(t, sink, "batch-gc-3", "inflight1")

	candidates, err := GC(context.Background(), client, sink, GCOptions{ClientID: clientID})
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates = %+v, want exactly 1", candidates)
	}
	if candidates[0].Exempt != "batch running" {
		t.Errorf("candidates[0].Exempt = %q, want %q", candidates[0].Exempt, "batch running")
	}

	if errs := GCApply(context.Background(), client, candidates); len(errs) != 0 {
		t.Fatalf("GCApply errors: %v", errs)
	}
	account.mu.Lock()
	defer account.mu.Unlock()
	if !findProjectByFakeName(account, ProjectPrefix+"inflight1") {
		t.Errorf("running-batch project %s was deleted", ProjectPrefix+"inflight1")
	}
}

func TestGC_UnknownFinishTime_RetentionExempts(t *testing.T) {
	t.Parallel()
	f := newControllerFixture(t, "client-s4-gc-age")
	f.account.seedProject(ProjectPrefix + "unknown-age")
	batch, runID := "batch-s4-gc-age", "unknown-age"
	if err := PutManifest(context.Background(), f.sink, batch, BatchManifest{Batch: batch, Runs: []ManifestRun{{RunID: runID, Scenario: runID, ProjectName: ProjectPrefix + runID}}}); err != nil {
		t.Fatalf("PutManifest: %v", err)
	}
	if err := f.sink.Put(context.Background(), "runs/"+runID+"/done.json", []byte(`{"runId":"unknown-age"}`)); err != nil {
		t.Fatalf("Put done: %v", err)
	}
	if err := PutSummary(context.Background(), f.sink, batch, BatchSummary{Batch: batch, FinishedAt: "", EndedBy: "settled", Runs: []SummaryRun{{RunID: runID, Scenario: runID}}}); err != nil {
		t.Fatalf("PutSummary: %v", err)
	}
	candidates, err := GC(context.Background(), f.client, f.sink, GCOptions{ClientID: "client-s4-gc-age", OlderThan: time.Hour, Now: func() time.Time { return time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if len(candidates) != 1 || candidates[0].Exempt != "unknown finish time" {
		t.Fatalf("candidates = %+v, want one unknown finish time exemption", candidates)
	}
}
