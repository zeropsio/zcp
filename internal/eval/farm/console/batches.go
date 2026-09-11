package console

import (
	"context"
	"sort"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm"
	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// VerdictCount is one (verdict, count) pair in a BatchRow's per-verdict
// breakdown.
type VerdictCount struct {
	Verdict string
	Count   int
}

// BatchRow is one row of the "/" batches page (§8.3 FM-51): id, created,
// candidate sha (12 chars), set, count per verdict, total cost, observed
// runs n/m.
type BatchRow struct {
	BatchID        string
	CreatedAt      time.Time
	CandidateSha12 string
	Set            string
	VerdictCounts  []VerdictCount
	TotalCostUsd   float64
	ObservedN      int
	ObservedM      int
	RunVerdicts    []string // one per run, in run-id order — the batch card's dots
	High           int      // high-severity findings across the batch's current observations
}

// verdictDisplayOrder fixes the order a batch row's per-verdict counts
// render in: verdictRunning plus §5.1's row/run vocabulary first, any other
// value (a verdict this slice does not know about yet) appended
// alphabetically so it is shown, never dropped.
var verdictDisplayOrder = []string{verdictRunning, farm.VerdictPassed, farm.VerdictFailed, farm.VerdictBlocked, farm.VerdictNotRun}

// candidateSha12 takes the first 12 characters of a candidate sha256
// (§8.3 FM-51: "candidate sha (12 chars)"); a shorter value (a test fixture)
// is returned unchanged rather than panicking on the slice bound.
func candidateSha12(sha string) string {
	if len(sha) <= 12 {
		return sha
	}
	return sha[:12]
}

// orderedVerdictCounts renders counts in verdictDisplayOrder, omitting a
// verdict with zero rows, then appends any verdict outside that list
// alphabetically.
func orderedVerdictCounts(counts map[string]int) []VerdictCount {
	seen := make(map[string]bool, len(counts))
	out := make([]VerdictCount, 0, len(counts))
	for _, v := range verdictDisplayOrder {
		if n, ok := counts[v]; ok {
			out = append(out, VerdictCount{Verdict: v, Count: n})
			seen[v] = true
		}
	}
	var extra []string
	for v := range counts {
		if !seen[v] {
			extra = append(extra, v)
		}
	}
	sort.Strings(extra)
	for _, v := range extra {
		out = append(out, VerdictCount{Verdict: v, Count: counts[v]})
	}
	return out
}

// loadBatchRows resolves every batch's "/" row (§8.3 FM-51), newest
// manifest.createdAt first (ties broken by batch id, descending, so the
// order is deterministic). It reads view.go's RunRow per run (read-only:
// this file adds no new resolution logic beyond aggregating those rows) for
// per-verdict counts, total cost and the observed-n count; m is the batch's
// run count. Each batch's manifest is loaded exactly once (item 7c) and
// reused for both its own fields and the per-run scan; a batch that fails
// to load (a corrupt manifest, item 5) is skipped and logged rather than
// failing the whole page — only the top-level batches/ listing itself can
// fail the call outright.
func loadBatchRows(ctx context.Context, store observer.ObjectStore, consoleObserverDisabled bool, queueState func(runID string) string) ([]BatchRow, error) {
	ids, err := listBatchIDs(ctx, store)
	if err != nil {
		return nil, err
	}

	rows := make([]BatchRow, 0, len(ids))
	for _, id := range ids {
		manifest, err := loadManifest(ctx, store, id)
		if err != nil {
			viewLogf("skip batch %s: load manifest: %v", id, err)
			continue
		}
		runRows, err := batchWindowRowsWithManifest(ctx, store, consoleObserverDisabled, id, manifest, queueState)
		if err != nil {
			viewLogf("skip batch %s: %v", id, err)
			continue
		}
		createdAt, _ := time.Parse(time.RFC3339, manifest.CreatedAt)

		counts := make(map[string]int)
		var totalCost float64
		observedN, high := 0, 0
		sort.Slice(runRows, func(i, j int) bool { return runRows[i].RunID < runRows[j].RunID })
		verdicts := make([]string, 0, len(runRows))
		for _, r := range runRows {
			counts[r.Verdict]++
			totalCost += r.CostUsd
			verdicts = append(verdicts, r.Verdict)
			if r.ObserverState == observerStateObserved {
				observedN++
			}
			if r.Observation != nil {
				for _, f := range r.Observation.Findings {
					if f.Severity == observer.SeverityHigh {
						high++
					}
				}
			}
		}

		rows = append(rows, BatchRow{
			BatchID:        id,
			CreatedAt:      createdAt,
			CandidateSha12: candidateSha12(manifest.CandidateSha256),
			Set:            manifest.Set,
			VerdictCounts:  orderedVerdictCounts(counts),
			TotalCostUsd:   totalCost,
			ObservedN:      observedN,
			ObservedM:      len(runRows),
			RunVerdicts:    verdicts,
			High:           high,
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		if !rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].CreatedAt.After(rows[j].CreatedAt)
		}
		return rows[i].BatchID > rows[j].BatchID
	})
	return rows, nil
}
