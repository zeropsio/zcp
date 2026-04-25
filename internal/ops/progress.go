package ops

import (
	"context"
	"fmt"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// ProgressCallback is called by PollProcess to report progress.
type ProgressCallback func(message string, progress, total float64)

type pollConfig struct {
	initialInterval time.Duration
	stepUpInterval  time.Duration
	stepUpAfter     time.Duration
	timeout         time.Duration
}

var defaultPollConfig = pollConfig{
	initialInterval: 2 * time.Second,
	stepUpInterval:  5 * time.Second,
	stepUpAfter:     30 * time.Second,
	timeout:         10 * time.Minute,
}

// PollProcess polls a process until terminal state.
// onProgress may be nil (no notifications sent).
func PollProcess(
	ctx context.Context,
	client platform.Client,
	processID string,
	onProgress ProgressCallback,
) (*platform.Process, error) {
	return pollProcess(ctx, client, processID, onProgress, defaultPollConfig)
}

var defaultBuildPollConfig = pollConfig{
	initialInterval: 1 * time.Second,
	stepUpInterval:  5 * time.Second,
	stepUpAfter:     30 * time.Second,
	timeout:         15 * time.Minute,
}

// PollBuild polls SearchAppVersions for a service until build reaches terminal state.
// Filters events by serviceStackID and checks the latest for ACTIVE or BUILD_FAILED.
// Skips startWithoutCode events (Source="NONE", no build info) which are pre-existing
// ACTIVE events that would cause the poll to return immediately without waiting for
// the actual build.
// onProgress may be nil (no notifications sent).
func PollBuild(
	ctx context.Context,
	client platform.Client,
	projectID string,
	serviceStackID string,
	onProgress ProgressCallback,
) (*platform.AppVersionEvent, error) {
	return pollBuild(ctx, client, projectID, serviceStackID, onProgress, defaultBuildPollConfig)
}

func pollBuild(
	ctx context.Context,
	client platform.Client,
	projectID string,
	serviceStackID string,
	onProgress ProgressCallback,
	cfg pollConfig,
) (*platform.AppVersionEvent, error) {
	start := time.Now()
	interval := cfg.initialInterval

	for {
		events, err := client.SearchAppVersions(ctx, projectID, 10)
		if err != nil {
			return nil, fmt.Errorf("poll build for service %s: %w", serviceStackID, err)
		}

		// Find latest event for this service, skipping startWithoutCode events.
		// startWithoutCode creates an ACTIVE event with Source="NONE" and no build
		// info. Without filtering, pollBuild sees this pre-existing ACTIVE and
		// returns immediately — thinking the deploy just succeeded.
		var latest *platform.AppVersionEvent
		for i := range events {
			if events[i].ServiceStackID != serviceStackID {
				continue
			}
			if isStartWithoutCodeEvent(&events[i]) {
				continue
			}
			if latest == nil || events[i].Sequence > latest.Sequence {
				latest = &events[i]
			}
		}

		// Terminal states return before onProgress to avoid the Claude Code MCP
		// JS client race (same-chunk progress+response → "unknown token" error
		// and transport teardown). See pollProcess for the full rationale.
		if latest != nil {
			// Layer 2: PipelineFailed timestamp is a hard signal — stop regardless of status string.
			if latest.Build != nil && latest.Build.PipelineFailed != nil {
				return latest, nil
			}
			// Layer 1: Inverted check — whitelist in-progress states, treat everything else as terminal.
			if !isBuildInProgress(latest.Status) {
				return latest, nil
			}
		}

		// Race avoidance — see pollProcess for the full rationale. Every return
		// path in this loop runs BEFORE onProgress so progress notifications are
		// always at least one poll interval before any response on the wire.
		elapsed := time.Since(start)
		if elapsed > cfg.timeout {
			return nil, platform.NewPlatformError(
				platform.ErrAPITimeout,
				fmt.Sprintf("Build for service %s timed out after %s", serviceStackID, cfg.timeout),
				"Check build status manually with zerops_events",
			)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if onProgress != nil {
			status := "waiting"
			if latest != nil {
				status = latest.Status
			}
			progress := float64(elapsed) / float64(cfg.timeout) * 100
			if progress > 100 {
				progress = 100
			}
			onProgress(
				fmt.Sprintf("Build %s: %s", serviceStackID, status),
				progress, 100,
			)
		}

		if elapsed > cfg.stepUpAfter {
			interval = cfg.stepUpInterval
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// isStartWithoutCodeEvent returns true if an AppVersionEvent was created by
// startWithoutCode (no real build). These events have Source="NONE" and no
// build pipeline info. They must be skipped during poll to avoid treating
// a pre-existing ACTIVE as a successful deploy result.
func isStartWithoutCodeEvent(ev *platform.AppVersionEvent) bool {
	return ev.Source == "NONE" && ev.Build == nil
}

// isBuildInProgress returns true only for known in-progress build states.
// Unknown statuses are treated as terminal (fail-safe: stop polling immediately).
// Full lifecycle: UPLOADING → WAITING_TO_BUILD → BUILDING → PREPARING_RUNTIME → WAITING_TO_DEPLOY → DEPLOYING → ACTIVE
func isBuildInProgress(status string) bool {
	switch status {
	case "UPLOADING", "WAITING_TO_BUILD", "BUILDING", "PREPARING_RUNTIME", "WAITING_TO_DEPLOY", "DEPLOYING":
		return true
	default:
		return false
	}
}

func pollProcess(
	ctx context.Context,
	client platform.Client,
	processID string,
	onProgress ProgressCallback,
	cfg pollConfig,
) (*platform.Process, error) {
	start := time.Now()
	interval := cfg.initialInterval

	for {
		proc, err := client.GetProcess(ctx, processID)
		if err != nil {
			return nil, fmt.Errorf("poll process %s: %w", processID, err)
		}

		// Race avoidance — every return path in this loop runs BEFORE onProgress.
		//
		// User-visible symptom this prevents: recurring "shutdown: client
		// disconnected (err=<nil>)" entries in ~/.zcp/serve.log appearing
		// shortly after a long-running poll completes (manage/scale/delete/
		// subdomain/import/deploy). PRE-FIX: the iteration that detected
		// timeout emitted progress, then ~µs later returned the error response;
		// PRE-FIX measured gap = 1.7–4.9µs; POST-FIX gap = ≥ initialInterval
		// (1–5s). The bug crashed an interactive Claude session at exactly the
		// 10-min mark on 2026-04-25 (zerops_manage start appdev) — see commit
		// log for the full forensics.
		//
		// Mechanism: Claude Code's MCP TS client has a documented race
		// (modelcontextprotocol/typescript-sdk #245-class) where _onresponse
		// synchronously deletes the progress handler while _onnotification
		// dispatches via microtask. If a progress notification and its tool
		// response land in the same stdin data chunk, the microtask fires after
		// the delete and the client errors with "Received a progress notification
		// for an unknown token", tearing down the stdio transport. Empirically
		// reproduced 7/7 times in 2026-04 testing against mcptest with 200ms-
		// interval progress; pinned by TestPollProcess_TimeoutSkipsProgressEmit
		// and TestPollBuild_TimeoutSkipsProgressEmit.
		//
		// Return paths handled:
		//   - terminal status (this if-block)
		//   - timeout (below, before onProgress)
		//   - ctx canceled at iteration boundary (below, before onProgress)
		//   - ctx canceled during interval wait (post-emit select; gap is the
		//     remaining wait, ≥ a few ms in practice — corner case accepted)
		//
		// If recurrence is suspected:
		//  1. Confirm symptom — is the disconnect closely following a long poll?
		//     Look for "shutdown: client disconnected" in ~/.zcp/serve.log with
		//     uptime matching cfg.timeout (10m/15m by default).
		//  2. Run the pinning tests:
		//       go test -run "TestPoll(Process|Build)_TimeoutSkipsProgressEmit" \
		//             ./internal/ops/ -count=1
		//     Failure = workaround was reverted; gap is back in microseconds.
		//  3. Audit new emit sites — grep "Session.NotifyProgress\|onProgress("
		//     across internal/. The only choke-point is convert.go::
		//     buildProgressCallback fed exclusively by pollProcess/pollBuild;
		//     any new direct caller is a regression risk.
		//  4. Reproduce upstream race independently — build a minimal go-sdk
		//     server tool that emits progress every 200ms then returns a
		//     non-nil result; drive it via interactive Claude Code; observe
		//     "client disconnected" within ~ms of tool completion.
		if !isProcessInProgress(proc.Status) {
			return proc, nil
		}

		elapsed := time.Since(start)
		if elapsed > cfg.timeout {
			return nil, platform.NewPlatformError(
				platform.ErrAPITimeout,
				fmt.Sprintf("Process %s timed out after %s", processID, cfg.timeout),
				"Check process status manually with zerops_process",
			)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if onProgress != nil {
			progress := float64(elapsed) / float64(cfg.timeout) * 100
			if progress > 100 {
				progress = 100
			}
			onProgress(
				fmt.Sprintf("Process %s: %s", processID, proc.Status),
				progress, 100,
			)
		}

		if elapsed > cfg.stepUpAfter {
			interval = cfg.stepUpInterval
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}
