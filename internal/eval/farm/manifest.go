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
	StartedAt       string `json:"startedAt"` // RFC3339
	Set             string `json:"set"`       // "gate" | "all" | "<id,id,...>"
	CandidateSha256 string `json:"candidateSha256"`
	EvaluatorSha256 string `json:"evaluatorSha256"`
	ScenariosDigest string `json:"scenariosDigest"`
	// Observer is `farm run`'s --observer choice ("<model>" or "off",
	// §3.3/§7.7) — a missing field (batches older than §7) means "never
	// observed automatically" (§1.4).
	Observer string `json:"observer,omitempty"`
	// Note is `farm run --note`: why the batch ran (§3.3), at most 200 chars.
	Note string `json:"note,omitempty"`
	// RunBudgetSec is the run budget, so a reader can tell a stalled run
	// from a running one (§3.3, §8.8).
	RunBudgetSec int `json:"runBudgetSec,omitempty"`
	// CandidateInfo is the candidate binary's embedded Go build info, when
	// `farm push --candidate` recorded it (§3.3).
	CandidateInfo *CandidateInfo `json:"candidateInfo,omitempty"`
	Runs          []ManifestRun  `json:"runs"`
}

// CandidateInfo is read from the candidate binary's debug/buildinfo by
// `farm push --candidate` and stored as candidates/<sha256>.info.json (§3.3).
type CandidateInfo struct {
	Revision  string `json:"revision,omitempty"`
	Modified  bool   `json:"modified,omitempty"`
	Time      string `json:"time,omitempty"`
	GoVersion string `json:"goVersion,omitempty"`
}

// ManifestRun is one batches/<batch>/manifest.json run entry.
type ManifestRun struct {
	RunID       string `json:"runId"`
	Scenario    string `json:"scenario"`
	ProjectName string `json:"projectName"`
	// ProductionProjectName is the explicit production target for a launch
	// run. It is optional so legacy manifests remain readable.
	ProductionProjectName string `json:"productionProjectName,omitempty"`
	// RunTokenID is the id (never the value) of the project-scoped
	// ZCP_API_KEY the controller mints for this run (§2.1 FM-10,
	// platform.MintProjectScopedToken) — empty until the mint succeeds.
	// Deleting the run's project deletes the token automatically, so
	// there is no matching revoke step to track (unlike LaunchTokenID on
	// SummaryRun).
	RunTokenID string `json:"runTokenId,omitempty"`
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
	ProjectID             string `json:"projectId,omitempty"`
	ProductionProjectName string `json:"productionProjectName,omitempty"`
	Result                string `json:"result"` // "passed" | "failed" | "blocked" | "not-run"
	Detail                string `json:"detail,omitempty"`
	// Error carries the wrapped error message for a run RunBatch blocked
	// before or during creation (D10) — never populated together with a
	// settle-time Detail (a run either fails during creation, before any
	// project exists to poll, or reaches waitForDone and gets a Detail).
	Error string `json:"error,omitempty"`
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

// CreateManifest reserves a batch identity. The conditional write is the
// ownership boundary: a conflict or ambiguous response is returned to the
// controller and no caller may adopt the existing bytes.
func CreateManifest(ctx context.Context, client *SinkClient, batch string, m BatchManifest) error {
	body, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("farm: marshal manifest: %w", err)
	}
	if err := client.PutIfAbsent(ctx, manifestKey(batch), body); err != nil {
		return fmt.Errorf("farm: create manifest: %w", err)
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
