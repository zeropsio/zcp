package update

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	bgInitialDelay = 1 * time.Hour
	bgInterval     = 4 * time.Hour
	bgApplyTimeout = 2 * time.Minute
)

// Background starts a goroutine that periodically checks for updates and applies
// them by replacing the binary on disk and calling shutdown for graceful restart.
// Returns immediately if currentVersion is "dev".
// forceCh triggers an immediate check (e.g., from SIGUSR1); nil is safe.
func Background(ctx context.Context, currentVersion string, shutdown func(), logOutput io.Writer, forceCh <-chan struct{}) {
	if currentVersion == "dev" {
		return
	}

	go backgroundLoop(ctx, currentVersion, shutdown, logOutput, forceCh)
}

func backgroundLoop(ctx context.Context, currentVersion string, shutdown func(), logOutput io.Writer, forceCh <-chan struct{}) {
	timer := time.NewTimer(bgInitialDelay)
	defer timer.Stop()

	// Nil forceCh: create a channel that never receives.
	if forceCh == nil {
		forceCh = make(chan struct{})
	}

	binaryPath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(logOutput, "zcp: background update: resolve executable: %v\n", err)
		return
	}

	updateURL := os.Getenv("ZCP_UPDATE_URL")

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			fmt.Fprintf(logOutput, "zcp: background update: checking for updates...\n")
			if tryApplyUpdate(ctx, currentVersion, binaryPath, shutdown, logOutput, updateURL, nil) {
				return
			}
			timer.Reset(bgInterval)
		case <-forceCh:
			fmt.Fprintf(logOutput, "zcp: background update: force checking for updates...\n")
			if tryApplyUpdate(ctx, currentVersion, binaryPath, shutdown, logOutput, updateURL, nil) {
				return
			}
			// Don't reset the periodic timer — let it fire on its own schedule.
		}
	}
}

// tryApplyUpdate checks for an update and applies it if available.
// Returns true if an update was applied (and shutdown was called).
func tryApplyUpdate(ctx context.Context, currentVersion, binaryPath string, shutdown func(), logOutput io.Writer, updateURL string, httpClient *http.Client) bool {
	checker := NewChecker(currentVersion)
	checker.CacheTTL = 0 // always fresh API call
	if updateURL != "" {
		checker.GitHubAPIURL = updateURL + "/repos/zeropsio/zcp/releases/latest"
		checker.DownloadBaseURL = updateURL + "/download"
	}
	if httpClient != nil {
		checker.HTTPClient = httpClient
	}

	info := checker.Check(ctx)
	if !info.Available {
		fmt.Fprintf(logOutput, "zcp: background update: up to date (%s)\n", currentVersion)
		return false
	}

	dir := filepath.Dir(binaryPath)
	if !CanWrite(dir) {
		fmt.Fprintf(logOutput, "zcp: background update: %s not writable, skipping\n", dir)
		return false
	}

	// Use a client with the apply timeout for the download.
	client := &http.Client{Timeout: bgApplyTimeout}

	if err := Apply(ctx, info, binaryPath, client); err != nil {
		fmt.Fprintf(logOutput, "zcp: background update: apply failed: %v\n", err)
		return false
	}

	fmt.Fprintf(logOutput, "zcp: background update: updated %s → %s, restarting...\n", currentVersion, info.LatestVersion)
	shutdown()
	return true
}
