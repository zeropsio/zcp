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
	initialInterval: 3 * time.Second,
	stepUpInterval:  10 * time.Second,
	stepUpAfter:     60 * time.Second,
	timeout:         15 * time.Minute,
}

// PollBuild polls SearchAppVersions for a service until build reaches terminal state.
// Filters events by serviceStackID and checks the latest for ACTIVE or BUILD_FAILED.
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

		// Find latest event for this service.
		var latest *platform.AppVersionEvent
		for i := range events {
			if events[i].ServiceStackID == serviceStackID {
				if latest == nil || events[i].Sequence > latest.Sequence {
					latest = &events[i]
				}
			}
		}

		elapsed := time.Since(start)
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

		if latest != nil && isBuildTerminal(latest.Status) {
			return latest, nil
		}

		if elapsed > cfg.timeout {
			return nil, platform.NewPlatformError(
				platform.ErrAPITimeout,
				fmt.Sprintf("Build for service %s timed out after %s", serviceStackID, cfg.timeout),
				"Check build status manually with zerops_events",
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

func isBuildTerminal(status string) bool {
	return status == "ACTIVE" || status == "BUILD_FAILED"
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

		elapsed := time.Since(start)
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

		if isTerminal(proc.Status) {
			return proc, nil
		}

		if elapsed > cfg.timeout {
			return nil, platform.NewPlatformError(
				platform.ErrAPITimeout,
				fmt.Sprintf("Process %s timed out after %s", processID, cfg.timeout),
				"Check process status manually with zerops_process",
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
