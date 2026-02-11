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
