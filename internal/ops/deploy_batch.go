package ops

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/platform"
)

// DeployBatchTarget is one entry in a batched deploy — names the source and
// target service plus the zerops.yaml setup block. Each target is deployed
// independently in its own goroutine inside DeployBatchSSH. v8.94 §5.9.
type DeployBatchTarget struct {
	SourceService string `json:"sourceService,omitempty"`
	TargetService string `json:"targetService"`
	Setup         string `json:"setup,omitempty"`
	WorkingDir    string `json:"workingDir,omitempty"`
	IncludeGit    bool   `json:"includeGit,omitempty"`
}

// DeployBatchEntryResult is the outcome for one target inside a batch. The
// Result pointer carries the same DeployResult shape a single zerops_deploy
// call would have produced; Error is set when the kickoff itself failed
// before the platform could return a build ID.
type DeployBatchEntryResult struct {
	Target    DeployBatchTarget `json:"target"`
	Result    *DeployResult     `json:"result,omitempty"`
	Error     string            `json:"error,omitempty"`
	StartedAt string            `json:"startedAt"`
	EndedAt   string            `json:"endedAt"`
}

// DeployBatchResult aggregates per-target outcomes for a zerops_deploy_batch
// call. Summary is a short human-readable line ("3/3 succeeded" /
// "2/3 succeeded, 1 failed") suitable for direct agent consumption.
type DeployBatchResult struct {
	BatchID   string                   `json:"batchId"`
	Entries   []DeployBatchEntryResult `json:"entries"`
	Succeeded int                      `json:"succeeded"`
	Failed    int                      `json:"failed"`
	Summary   string                   `json:"summary"`
	DurationS float64                  `json:"durationSeconds"`
}

// DeployBatchSSH deploys each target in its own goroutine and blocks until
// all return. Closes the MCP STDIO serialization penalty v23 discovered: an
// agent calling zerops_deploy three times in parallel causes two to return
// "Not connected" mid-build because the channel is busy. Batch keeps the
// parallelism server-side behind a single MCP call.
//
// Failure semantics: per-target failures do NOT cancel sibling deploys —
// each target runs to completion independently. A target whose kickoff
// errors records the error in its entry and the batch continues. The
// aggregate summary reports succeeded/failed counts so the agent can apply
// targeted fixes to specific failing targets rather than rolling back the
// whole cluster.
//
// Platform-layer safety: the platform.Client is a thin HTTP wrapper safe
// for concurrent calls; SSHDeployer backs onto independent `ssh` subprocess
// invocations per hostname so concurrency across distinct hostnames is safe
// (the deployer-wide 5-min ceiling applies per-call, not per-process).
// RecordDeployAttempt is protected by workSessionMu in the workflow layer.
func DeployBatchSSH(
	ctx context.Context,
	client platform.Client,
	projectID string,
	sshDeployer SSHDeployer,
	authInfo auth.Info,
	targets []DeployBatchTarget,
	logFetcher platform.LogFetcher,
	onProgress ProgressCallback,
	pollBuild func(context.Context, *DeployResult, ProgressCallback, platform.LogFetcher, SSHDeployer),
) *DeployBatchResult {
	startedAt := time.Now()
	batchID := fmt.Sprintf("batch-%d", startedAt.UnixNano())

	if len(targets) == 0 {
		return &DeployBatchResult{
			BatchID: batchID,
			Summary: "no targets",
		}
	}

	entries := make([]DeployBatchEntryResult, len(targets))
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		go func(idx int, tgt DeployBatchTarget) {
			defer wg.Done()
			entry := DeployBatchEntryResult{
				Target:    tgt,
				StartedAt: time.Now().UTC().Format(time.RFC3339),
			}
			defer func() {
				entry.EndedAt = time.Now().UTC().Format(time.RFC3339)
				entries[idx] = entry
			}()

			result, err := DeploySSH(
				ctx, client, projectID, sshDeployer, authInfo,
				tgt.SourceService, tgt.TargetService, tgt.Setup, tgt.WorkingDir, tgt.IncludeGit,
			)
			if err != nil {
				entry.Error = err.Error()
				return
			}
			if pollBuild != nil {
				pollBuild(ctx, result, onProgress, logFetcher, sshDeployer)
			}
			entry.Result = result
		}(i, t)
	}
	wg.Wait()

	out := &DeployBatchResult{
		BatchID: batchID,
		Entries: entries,
	}
	for i := range entries {
		e := entries[i]
		if e.Error != "" || e.Result == nil || e.Result.Status != "DEPLOYED" {
			out.Failed++
			continue
		}
		out.Succeeded++
	}
	out.DurationS = time.Since(startedAt).Seconds()
	if out.Failed == 0 {
		out.Summary = fmt.Sprintf("%d/%d succeeded", out.Succeeded, len(targets))
	} else {
		out.Summary = fmt.Sprintf("%d/%d succeeded, %d failed", out.Succeeded, len(targets), out.Failed)
	}
	return out
}
