package farm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// GCCandidate is one zcp-farm-prefixed project `farm gc` considered.
// Exempt is empty for a project eligible for deletion, or names why it was
// skipped ("no bundle", "batch running", "too recent", "unknown run").
type GCCandidate struct {
	ProjectID string
	Name      string
	RunID     string
	Exempt    string
}

// GCOptions is the input to GC.
type GCOptions struct {
	ClientID string // ZCP_FARM_CLIENT_ID
	// OlderThan, when non-zero, additionally exempts a finished batch's
	// projects until its summary.json's finishedAt is at least this old
	// (§3.6 FM-26: "--older-than only applies to projects a batch has
	// already finished with").
	OlderThan time.Duration
	Now       func() time.Time
}

// batchRunInfo is what GC needs to know about one run to classify its
// project(s): whether the run's batch has finished (summary.json exists —
// a batch is "running" exactly when its manifest exists and its summary
// does not, §3.6 FM-26) and whether the run itself produced a bundle
// (runs/<runId>/done.json exists, FM-3).
type batchRunInfo struct {
	batch         string
	runID         string
	ambiguous     bool
	batchFinished bool
	hasDone       bool
	finishedAt    time.Time
}

// GC lists every zcp-farm-prefixed project and classifies each as a
// deletion candidate or exempt (§3.6 FM-26). Read-only — it never deletes;
// GCApply does that, through Guard, only for the candidates the caller
// chooses (the CLI's --yes gate). Every listing is filtered through the
// ProjectPrefix predicate before anything else looks at it (FM-20): a
// foreign-prefixed project is never even added to the returned slice, not
// just marked exempt.
func GC(ctx context.Context, client PlatformClient, sink *SinkClient, opts GCOptions) ([]GCCandidate, error) {
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	projects, err := client.ListProjects(ctx, opts.ClientID)
	if err != nil {
		return nil, fmt.Errorf("farm gc: list projects: %w", err)
	}

	index, err := buildBatchRunIndex(ctx, sink)
	if err != nil {
		return nil, fmt.Errorf("farm gc: index batches: %w", err)
	}

	var candidates []GCCandidate
	for _, p := range projects {
		if !strings.HasPrefix(p.Name, ProjectPrefix) {
			continue // never listed (FM-19/FM-20)
		}
		info, known := index[p.Name]

		c := GCCandidate{ProjectID: p.ID, Name: p.Name, RunID: info.runID}
		switch {
		case !known:
			c.Exempt = "unknown run"
		case info.ambiguous:
			c.Exempt = "ambiguous ownership"
		case !info.batchFinished:
			c.Exempt = "batch running"
		case !info.hasDone:
			c.Exempt = DetailNoBundle
		case opts.OlderThan > 0 && !sufficientFinishAge(now(), info.finishedAt, opts.OlderThan):
			c.Exempt = finishAgeExemption(now(), info.finishedAt)
		}
		candidates = append(candidates, c)
	}
	return candidates, nil
}

func sufficientFinishAge(now time.Time, finishedAt time.Time, olderThan time.Duration) bool {
	if finishedAt.IsZero() || finishedAt.After(now) {
		return false
	}
	return now.Sub(finishedAt) >= olderThan
}

func finishAgeExemption(now, finishedAt time.Time) string {
	if finishedAt.IsZero() || finishedAt.After(now) {
		return "unknown finish time"
	}
	return "too recent"
}

// buildBatchRunIndex walks every batch the bucket knows about
// (batches/<batch>/manifest.json) and maps each run id it lists to whether
// that run's batch has finished and whether the run itself produced a
// bundle.
func buildBatchRunIndex(ctx context.Context, sink *SinkClient) (map[string]batchRunInfo, error) {
	batches, err := ListBatches(ctx, sink)
	if err != nil {
		return nil, err
	}
	index := map[string]batchRunInfo{}
	for _, batch := range batches {
		manifest, err := GetManifest(ctx, sink, batch)
		if err != nil {
			continue // an unreadable manifest leaves its runs "unknown"
		}
		finished, err := SummaryExists(ctx, sink, batch)
		if err != nil {
			continue
		}
		var finishedAt time.Time
		if finished {
			if summary, err := GetSummary(ctx, sink, batch); err == nil {
				finishedAt, _ = time.Parse(time.RFC3339, summary.FinishedAt)
			}
		}
		for _, run := range manifest.Runs {
			hasDone, _, err := sink.Head(ctx, "runs/"+run.RunID+"/done.json")
			if err != nil {
				continue
			}
			info := batchRunInfo{batch: batch, runID: run.RunID, batchFinished: finished, hasDone: hasDone, finishedAt: finishedAt}
			addProjectOwner(index, run.ProjectName, info)
			if run.ProductionProjectName != "" {
				addProjectOwner(index, run.ProductionProjectName, info)
			}
		}
	}
	return index, nil
}

func addProjectOwner(index map[string]batchRunInfo, name string, info batchRunInfo) {
	if name == "" {
		return
	}
	if previous, ok := index[name]; ok && (previous.batch != info.batch || previous.runID != info.runID || previous.batchFinished != info.batchFinished || previous.hasDone != info.hasDone) {
		index[name] = batchRunInfo{ambiguous: true}
		return
	}
	if !info.ambiguous {
		index[name] = info
	}
}

// ListBatches returns every distinct batch id under batches/ in the
// bucket.
func ListBatches(ctx context.Context, sink *SinkClient) ([]string, error) {
	keys, err := sink.List(ctx, "batches/")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var ids []string
	for _, key := range keys {
		rel := strings.TrimPrefix(key, "batches/")
		id, _, found := strings.Cut(rel, "/")
		if !found || id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids, nil
}

// GCApply deletes every non-exempt candidate through Guard (§3.2
// FM-19/FM-20 — the same sole call site RunBatch uses) and returns one
// error per candidate that failed to delete (nil entries are omitted).
func GCApply(ctx context.Context, client PlatformClient, candidates []GCCandidate) []error {
	var errs []error
	for _, c := range candidates {
		if c.Exempt != "" {
			continue
		}
		if err := Guard(ctx, client, c.ProjectID, c.Name); err != nil {
			errs = append(errs, fmt.Errorf("farm gc: delete %s (%s): %w", c.Name, c.ProjectID, err))
		}
	}
	return errs
}

// RevokeOrphanedLaunchTokens revokes launch tokens recorded in finished
// batch summaries whose explicitly recorded primary and production projects
// are both gone from the account's live project list — the run's own settle
// pass (RunBatch) never got to revoke it because FM-21's no-bundle
// exemption kept the project around past that point, and a later gc finally
// removed it.
func RevokeOrphanedLaunchTokens(ctx context.Context, client PlatformClient, sink *SinkClient, clientID string) error {
	live, err := client.ListProjects(ctx, clientID)
	if err != nil {
		return fmt.Errorf("farm gc: list projects: %w", err)
	}
	liveNames := make(map[string]bool, len(live))
	for _, p := range live {
		liveNames[p.Name] = true
	}

	batches, err := ListBatches(ctx, sink)
	if err != nil {
		return fmt.Errorf("farm gc: list batches: %w", err)
	}
	var cleanupErrs []error
	for _, batch := range batches {
		finished, err := SummaryExists(ctx, sink, batch)
		if err != nil {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("farm gc: inspect batch %s summary: %w", batch, err))
			continue
		}
		if !finished {
			continue
		}
		summary, err := GetSummary(ctx, sink, batch)
		if err != nil {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("farm gc: read batch %s summary: %w", batch, err))
			continue
		}
		manifest, err := GetManifest(ctx, sink, batch)
		if err != nil {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("farm gc: read batch %s manifest: %w", batch, err))
			continue
		}
		summaryRunCounts := make(map[string]int, len(summary.Runs))
		launchTokenCounts := make(map[string]int, len(summary.Runs))
		for _, run := range summary.Runs {
			summaryRunCounts[run.RunID]++
			if run.LaunchTokenID != "" {
				launchTokenCounts[run.LaunchTokenID]++
			}
		}
		for _, run := range summary.Runs {
			if run.LaunchTokenID == "" || summaryRunCounts[run.RunID] != 1 || launchTokenCounts[run.LaunchTokenID] != 1 {
				continue
			}
			var owners []ManifestRun
			for _, candidate := range manifest.Runs {
				if candidate.RunID == run.RunID {
					owners = append(owners, candidate)
				}
			}
			if len(owners) != 1 || owners[0].ProjectName == "" || owners[0].ProductionProjectName == "" || run.ProductionProjectName != owners[0].ProductionProjectName {
				continue // incomplete or ambiguous same-batch ownership; retain token
			}
			runName := owners[0].ProjectName
			productionName := owners[0].ProductionProjectName
			if liveNames[runName] || liveNames[productionName] {
				continue // still has a project — not this pass's job
			}
			if err := client.RevokeIntegrationToken(ctx, clientID, run.LaunchTokenID); err != nil {
				cleanupErrs = append(cleanupErrs, fmt.Errorf("farm gc: revoke launch token %s for run %s: %w", run.LaunchTokenID, run.RunID, err))
			}
		}
	}
	return errors.Join(cleanupErrs...)
}
