package farm

import (
	"context"
	"encoding/json"
	"fmt"
)

// BatchManifest is batches/<batch>/manifest.json, written by `farm run`
// before any run project is created (docs/spec-eval-farm.md §1.4 FM-9,
// §3.3 FM-22). Not the registry of truth — `farm status`/`gc` always
// recompute from the bucket listing and the live project list — but every
// report cites which manifest it read.
type BatchManifest struct {
	Batch     string `json:"batch"`
	CreatedAt string `json:"createdAt"` // RFC3339
	// StartedAt is set once, at the same moment as CreatedAt, for this
	// slice — S7's `coverage --since` reads it to bound a batch's window.
	StartedAt       string        `json:"startedAt"` // RFC3339
	Set             string        `json:"set"`       // "gate" | "all" | "<id,id,...>"
	CandidateSha256 string        `json:"candidateSha256"`
	EvaluatorSha256 string        `json:"evaluatorSha256"`
	ScenariosDigest string        `json:"scenariosDigest"`
	CredentialMode  string        `json:"credentialMode"` // "api-key" | "oauth-token"
	Runs            []ManifestRun `json:"runs"`
}

// ManifestRun is one batches/<batch>/manifest.json run entry.
type ManifestRun struct {
	RunID       string `json:"runId"`
	Scenario    string `json:"scenario"`
	ProjectName string `json:"projectName"`
}

// BatchSummary is batches/<batch>/summary.json, written by `farm run` after
// the last run in the batch settles (done, budget-expired, or exempted —
// §3.3 FM-22).
type BatchSummary struct {
	Batch      string       `json:"batch"`
	FinishedAt string       `json:"finishedAt"` // RFC3339
	EndedBy    string       `json:"endedBy"`    // "settled" | "budget" | "interrupt"
	Runs       []SummaryRun `json:"runs"`
}

// SummaryRun is one batches/<batch>/summary.json run entry.
type SummaryRun struct {
	RunID    string `json:"runId"`
	Scenario string `json:"scenario"`
	// ProjectID is empty once the run's project has been deleted (the
	// common case for a settled run).
	ProjectID string `json:"projectId,omitempty"`
	Result    string `json:"result"` // "passed" | "failed" | "blocked" | "not-run"
	Detail    string `json:"detail,omitempty"`
	// LaunchTokenID is the id (never the value, §3.4 FM-23) of the launch
	// token minted for this run, when it has not yet been revoked — the
	// no-bundle exemption (FM-21) records it here so `gc` can revoke it
	// once the run's project is finally gone.
	LaunchTokenID string `json:"launchTokenId,omitempty"`
}

// manifestKey and summaryKey are the bucket keys for a batch's manifest and
// summary (§1.1).
func manifestKey(batch string) string { return "batches/" + batch + "/manifest.json" }
func summaryKey(batch string) string  { return "batches/" + batch + "/summary.json" }

// PutManifest writes batch's manifest.json.
func PutManifest(ctx context.Context, client *SinkClient, batch string, m BatchManifest) error {
	body, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("farm: marshal manifest: %w", err)
	}
	if err := client.Put(ctx, manifestKey(batch), body); err != nil {
		return fmt.Errorf("farm: put manifest: %w", err)
	}
	return nil
}

// GetManifest reads batch's manifest.json.
func GetManifest(ctx context.Context, client *SinkClient, batch string) (BatchManifest, error) {
	body, err := client.Get(ctx, manifestKey(batch))
	if err != nil {
		return BatchManifest{}, fmt.Errorf("farm: get manifest: %w", err)
	}
	var m BatchManifest
	if err := json.Unmarshal(body, &m); err != nil {
		return BatchManifest{}, fmt.Errorf("farm: parse manifest: %w", err)
	}
	return m, nil
}

// PutSummary writes batch's summary.json.
func PutSummary(ctx context.Context, client *SinkClient, batch string, s BatchSummary) error {
	body, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("farm: marshal summary: %w", err)
	}
	if err := client.Put(ctx, summaryKey(batch), body); err != nil {
		return fmt.Errorf("farm: put summary: %w", err)
	}
	return nil
}

// GetSummary reads batch's summary.json.
func GetSummary(ctx context.Context, client *SinkClient, batch string) (BatchSummary, error) {
	body, err := client.Get(ctx, summaryKey(batch))
	if err != nil {
		return BatchSummary{}, fmt.Errorf("farm: get summary: %w", err)
	}
	var s BatchSummary
	if err := json.Unmarshal(body, &s); err != nil {
		return BatchSummary{}, fmt.Errorf("farm: parse summary: %w", err)
	}
	return s, nil
}

// SummaryExists reports whether batch has a summary.json (a batch is
// "running" — §3.6 FM-26 — exactly when its manifest exists and its summary
// does not).
func SummaryExists(ctx context.Context, client *SinkClient, batch string) (bool, error) {
	exists, _, err := client.Head(ctx, summaryKey(batch))
	if err != nil {
		return false, fmt.Errorf("farm: head summary: %w", err)
	}
	return exists, nil
}
