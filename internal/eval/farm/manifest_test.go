package farm

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// TestBatchManifest_JSONShape_MatchesFM9 pins docs/spec-eval-farm.md §1.4
// FM-9's manifest.json shape (as pinned by the S4 brief's Outcome section,
// plus the orchestrator's startedAt addition read by S7's `coverage
// --since`). Independent oracle: the field set and names are copied
// verbatim from the brief/spec text, never derived from this package's own
// marshal output.
func TestBatchManifest_JSONShape_MatchesFM9(t *testing.T) {
	t.Parallel()
	m := BatchManifest{
		Batch:           "batch-2026-09-10",
		CreatedAt:       "2026-09-10T12:00:00Z",
		StartedAt:       "2026-09-10T12:00:01Z",
		Set:             "gate",
		CandidateSha256: "cand-sha",
		EvaluatorSha256: "eval-sha",
		ScenariosDigest: "scen-digest",
		CredentialMode:  "api-key",
		Runs: []ManifestRun{
			{RunID: "run-1", Scenario: "recipe-first-deploy", ProjectName: "zcp-farm-run-1"},
		},
	}
	body, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	wantKeys := []string{
		"batch", "createdAt", "startedAt", "set", "candidateSha256",
		"evaluatorSha256", "scenariosDigest", "credentialMode", "runs",
	}
	for _, k := range wantKeys {
		if _, ok := got[k]; !ok {
			t.Errorf("manifest JSON missing key %q, got: %s", k, body)
		}
	}
	runs, ok := got["runs"].([]any)
	if !ok || len(runs) != 1 {
		t.Fatalf("manifest JSON runs = %v, want one entry", got["runs"])
	}
	run, ok := runs[0].(map[string]any)
	if !ok {
		t.Fatalf("manifest JSON runs[0] not an object: %v", runs[0])
	}
	for _, k := range []string{"runId", "scenario", "projectName"} {
		if _, ok := run[k]; !ok {
			t.Errorf("manifest JSON runs[0] missing key %q, got: %s", k, body)
		}
	}
}

// TestBatchSummary_JSONShape_MatchesFM9 pins summary.json's shape, same
// independent-oracle discipline as the manifest test above.
func TestBatchSummary_JSONShape_MatchesFM9(t *testing.T) {
	t.Parallel()
	s := BatchSummary{
		Batch:      "batch-2026-09-10",
		FinishedAt: "2026-09-10T13:00:00Z",
		EndedBy:    "settled",
		Runs: []SummaryRun{
			{RunID: "run-1", Scenario: "recipe-first-deploy", ProjectID: "proj-1", Result: "passed"},
		},
	}
	body, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for _, k := range []string{"batch", "finishedAt", "endedBy", "runs"} {
		if _, ok := got[k]; !ok {
			t.Errorf("summary JSON missing key %q, got: %s", k, body)
		}
	}
	runs, ok := got["runs"].([]any)
	if !ok || len(runs) != 1 {
		t.Fatalf("summary JSON runs = %v, want one entry", got["runs"])
	}
	run, ok := runs[0].(map[string]any)
	if !ok {
		t.Fatalf("summary JSON runs[0] not an object: %v", runs[0])
	}
	for _, k := range []string{"runId", "scenario", "projectId", "result"} {
		if _, ok := run[k]; !ok {
			t.Errorf("summary JSON runs[0] missing key %q, got: %s", k, body)
		}
	}
}

// TestPutManifest_GetManifest_RoundTrip proves the sink read/write helpers
// against a minimal fake S3 (batches/<batch>/manifest.json, §1.1).
func TestPutManifest_GetManifest_RoundTrip(t *testing.T) {
	t.Parallel()

	fake := newFakeS3("zcp-farm")
	server := httptest.NewServer(fake.handler(t))
	defer server.Close()
	client := NewSinkClient(Config{URL: server.URL, Bucket: "zcp-farm", Key: "k", Secret: "s"})
	ctx := context.Background()

	want := BatchManifest{Batch: "b1", Set: "gate", Runs: []ManifestRun{{RunID: "r1"}}}
	if err := PutManifest(ctx, client, "b1", want); err != nil {
		t.Fatalf("PutManifest: %v", err)
	}
	got, err := GetManifest(ctx, client, "b1")
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	if got.Batch != want.Batch || got.Set != want.Set || len(got.Runs) != 1 || got.Runs[0].RunID != "r1" {
		t.Errorf("GetManifest round-trip = %+v, want %+v", got, want)
	}
}
