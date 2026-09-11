package console

import (
	"context"
	"fmt"
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

// Batch kinds (§8.8: "Evaluation batch — at least one run finished. Empty
// batch — no run finished").
const (
	batchKindEvaluation = "evaluation"
	batchKindEmpty      = "empty"
)

// BatchRow is one row of the "/" batches page (§8.3 FM-51): id, created,
// candidate sha (12 chars), set, count per verdict, total cost, observed
// runs n/m.
//
// The fields below High are slice MODEL's additive §8.3 item 2 facts
// (plans/farm-console-clarity-2026-09-11-briefs/MODEL.md).
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
	// High mixes every cause class (the bug item 2's brief names: "5 high
	// findings" mixes 1 ZCP with 4 agent/test) — kept exactly as-is because
	// assets/batches.html already renders it; ZCPHigh/ZCPMedium below are
	// the correctly-scoped replacement a later page slice switches to.
	High int

	Build               BuildInfo
	Note                string
	Kind                string // batchKindEvaluation | batchKindEmpty
	CostUnknownN        int    // count of runs whose cost is unknown (§8.3: "$2.19 + 7 unknown")
	ZCPHigh             int
	ZCPMedium           int
	DisputedCount       int
	GoalYes             int
	GoalPartly          int
	GoalNo              int
	OutcomeOK           int
	OutcomeProblem      int
	OutcomeInconclusive int
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
// fail the call outright. cache and sc are nil-safe (cache.go).
func loadBatchRows(ctx context.Context, store observer.ObjectStore, consoleObserverDisabled bool, queueState func(runID string) string, cache *runCache, sc *summaryCache, logf func(format string, args ...any)) ([]BatchRow, error) {
	if logf == nil {
		logf = defaultLogf
	}
	ids, err := listBatchIDs(ctx, store)
	if err != nil {
		return nil, fmt.Errorf("console: load batch rows: %w", err)
	}

	rows := make([]BatchRow, 0, len(ids))
	for _, id := range ids {
		manifest, err := loadManifest(ctx, store, id)
		if err != nil {
			logf("skip batch %s: load manifest: %v", id, err)
			continue
		}
		runRows, err := batchWindowRowsWithManifest(ctx, store, consoleObserverDisabled, id, manifest, queueState, cache, sc, logf)
		if err != nil {
			logf("skip batch %s: %v", id, err)
			continue
		}
		createdAt, _ := time.Parse(time.RFC3339, manifest.CreatedAt)
		bc := newBatchContext(manifest)

		counts := make(map[string]int)
		var totalCost float64
		observedN, high, costUnknown := 0, 0, 0
		anyDone := false
		disputed, goalYes, goalPartly, goalNo := 0, 0, 0, 0
		outcomeOK, outcomeProblem, outcomeInconclusive := 0, 0, 0
		causeCounts := newCauseClassCounts()
		sort.Slice(runRows, func(i, j int) bool { return runRows[i].RunID < runRows[j].RunID })
		verdicts := make([]string, 0, len(runRows))
		for _, r := range runRows {
			counts[r.Verdict]++
			totalCost += r.CostUsd
			verdicts = append(verdicts, r.Verdict)
			if !r.CostKnown {
				costUnknown++
			}
			if r.DoneExists {
				anyDone = true
			}
			// Outcome != "" implies a current ok observation (computeOutcome,
			// view.go) — the same fact ObservedN now uses (the "assessed n/m"
			// fix: a failed current observation is not assessed, item 1).
			if r.Outcome != "" {
				observedN++
			}
			if r.Disputed {
				disputed++
			}
			causeCounts = mergeCauseClassCounts(causeCounts, r.CauseCounts)
			switch r.Outcome {
			case observer.OutcomeOK:
				outcomeOK++
			case observer.OutcomeProblem:
				outcomeProblem++
			case observer.OutcomeInconclusive:
				outcomeInconclusive++
			}
			if r.Observation != nil && r.Observation.Status == observationStatusOK {
				switch r.Observation.Goal.Reached {
				case "yes":
					goalYes++
				case "partly":
					goalPartly++
				case "no":
					goalNo++
				}
				for _, f := range r.Observation.Findings {
					if f.Severity == observer.SeverityHigh {
						high++
					}
				}
			}
		}

		kind := batchKindEmpty
		if anyDone {
			kind = batchKindEvaluation
		}
		var zcpHigh, zcpMedium int
		for _, c := range causeCounts {
			if c.Class == CauseClassZCP {
				zcpHigh, zcpMedium = c.High, c.Medium
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

			Build: bc.build(), Note: manifest.Note, Kind: kind, CostUnknownN: costUnknown,
			ZCPHigh: zcpHigh, ZCPMedium: zcpMedium, DisputedCount: disputed,
			GoalYes: goalYes, GoalPartly: goalPartly, GoalNo: goalNo,
			OutcomeOK: outcomeOK, OutcomeProblem: outcomeProblem, OutcomeInconclusive: outcomeInconclusive,
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

// batchIsOlder is loadBatchRows' own newest-first total order (CreatedAt
// desc, then BatchID desc), read the other way: true when a sorts strictly
// after b in that order, i.e. a is older.
func batchIsOlder(a, b BatchRow) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.Before(b.CreatedAt)
	}
	return a.BatchID < b.BatchID
}

// PreviousSameSet returns the newest batch in rows that is older than
// batch, shares its Set, and has a finished run (Kind == batchKindEvaluation)
// — §8.3's "vs <previous batch of the same set>" (Overview, /b/<batch>,
// /problems' member status). found is false when no such batch exists.
func PreviousSameSet(rows []BatchRow, batch BatchRow) (BatchRow, bool) {
	var best BatchRow
	found := false
	for _, r := range rows {
		if r.BatchID == batch.BatchID || r.Set != batch.Set || r.Kind != batchKindEvaluation {
			continue
		}
		if !batchIsOlder(r, batch) {
			continue
		}
		if !found || batchIsOlder(best, r) {
			best, found = r, true
		}
	}
	return best, found
}

// BatchDiff is CompareBatches' result: scenario names newly failing (was
// passing or unseen in prev, fails now), fixed (was failing in prev,
// passes now) and still failing (failed in both) — §8.3's batch-vs-batch
// line. Blocked/not-started/running/stalled all count as "not passing"
// (only farm.VerdictPassed counts as passing); each list is sorted by
// scenario name.
type BatchDiff struct {
	NewlyFailing []string
	Fixed        []string
	StillFailing []string
}

// scenarioPassing reduces rows to one passing/failing verdict per scenario:
// a scenario passes only when every one of its runs in rows passed (a
// batch that ran a scenario more than once is stricter than "at least
// one passed" — any non-passing run makes the scenario not-passing,
// matching §8.8's "blocked/not-started count as not passing").
func scenarioPassing(rows []RunRow) map[string]bool {
	passing := make(map[string]bool)
	seen := make(map[string]bool)
	for _, r := range rows {
		p := r.Verdict == farm.VerdictPassed
		if !seen[r.Scenario] {
			passing[r.Scenario], seen[r.Scenario] = p, true
			continue
		}
		passing[r.Scenario] = passing[r.Scenario] && p
	}
	return passing
}

// CompareBatches implements §8.3's batch-vs-batch line over two already-
// resolved run sets: every scenario present in curRuns is newly failing
// (currently not passing, and either unseen in prevRuns or was passing
// there), fixed (passing now, was failing in prevRuns), or still failing
// (not passing in both) — a scenario passing in both, or absent from
// curRuns, is in none of the three lists.
func CompareBatches(prevRuns, curRuns []RunRow) BatchDiff {
	prev := scenarioPassing(prevRuns)
	cur := scenarioPassing(curRuns)

	scenarios := make([]string, 0, len(cur))
	for s := range cur {
		scenarios = append(scenarios, s)
	}
	sort.Strings(scenarios)

	var diff BatchDiff
	for _, s := range scenarios {
		isPassing := cur[s]
		wasPassing, hadPrev := prev[s]
		switch {
		case isPassing:
			if hadPrev && !wasPassing {
				diff.Fixed = append(diff.Fixed, s)
			}
		case !hadPrev || wasPassing:
			diff.NewlyFailing = append(diff.NewlyFailing, s)
		default:
			diff.StillFailing = append(diff.StillFailing, s)
		}
	}
	return diff
}

// --- Overview batches list (§8.7 item 5) -----------------------------------

// batchListSpec is the Overview batches list's query surface: kind=
// evaluation|empty|all (default evaluation), since — the table names no
// default for this list's `since` (§8.7 states a default only for
// /problems and /findings), and batches are rare enough events that a
// silent 24h default would hide most of them; NoSinceDefault leaves the
// window unbounded (every batch shown) until an operator asks for one.
func batchListSpec() ListSpec {
	return ListSpec{
		Closed: []ClosedFilter{
			{Name: paramKind, Allowed: []string{batchKindEvaluation, batchKindEmpty, "all"}},
		},
		Sorts: []SortKey{
			{Name: "newest", DefaultDir: "desc"},
			{Name: "zcp", DefaultDir: "desc"},
			{Name: "failed", DefaultDir: "desc"},
			{Name: "cost", DefaultDir: "desc"},
		},
		DefaultSort: "newest",
		Defaults:    map[string]string{paramKind: batchKindEvaluation},
		HasSince:    true, NoSinceDefault: true,
	}
}

// batchFailedCount is the `failed` sort key's field: failed + blocked run
// count (§8.7).
func batchFailedCount(b BatchRow) int {
	n := 0
	for _, vc := range b.VerdictCounts {
		if vc.Verdict == farm.VerdictFailed || vc.Verdict == farm.VerdictBlocked {
			n += vc.Count
		}
	}
	return n
}

// batchEngine wires batchListSpec to BatchRow: `kind=all` matches every
// row (it is a filter pseudo-value, not a stored Kind); every sort key's
// tie-break is `newest` except `newest` itself, whose own tie-break is the
// batch id (§8.7 table).
func batchEngine() Engine[BatchRow] {
	return Engine[BatchRow]{
		Spec: batchListSpec(),
		Match: func(b BatchRow, name, value string) bool {
			if name == paramKind {
				return value == "all" || b.Kind == value
			}
			return false
		},
		Time: func(b BatchRow) time.Time { return b.CreatedAt },
		Sorts: map[string]SortSpec[BatchRow]{
			"newest": {
				Primary:  func(a, b BatchRow) int { return cmpTime(a.CreatedAt, b.CreatedAt) },
				Tiebreak: func(a, b BatchRow) int { return cmpString(a.BatchID, b.BatchID) },
			},
			"zcp": {
				Primary: func(a, b BatchRow) int {
					if c := cmpInt(a.ZCPHigh, b.ZCPHigh); c != 0 {
						return c
					}
					return cmpInt(a.ZCPMedium, b.ZCPMedium)
				},
				Tiebreak: func(a, b BatchRow) int { return cmpTime(a.CreatedAt, b.CreatedAt) },
			},
			"failed": {
				Primary:  func(a, b BatchRow) int { return cmpInt(batchFailedCount(a), batchFailedCount(b)) },
				Tiebreak: func(a, b BatchRow) int { return cmpTime(a.CreatedAt, b.CreatedAt) },
			},
			"cost": {
				Primary:  func(a, b BatchRow) int { return cmpFloat(a.TotalCostUsd, b.TotalCostUsd) },
				Tiebreak: func(a, b BatchRow) int { return cmpTime(a.CreatedAt, b.CreatedAt) },
			},
		},
	}
}
