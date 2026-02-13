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

const downloadTimeout = 30 * time.Second

// Apply downloads the new binary and atomically replaces the current one.
// If info.Available is false, this is a no-op.
func Apply(info *Info, binaryPath string, client *http.Client) error {
	if !info.Available {
		return nil
	}

	if client == nil {
		client = &http.Client{Timeout: downloadTimeout}
	}

	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, info.DownloadURL, nil)
	if err != nil {
		return fmt.Errorf("download: create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}

	// Write to temp file in same directory for atomic rename.
	dir := filepath.Dir(binaryPath)
	tmp, err := os.CreateTemp(dir, "zcp-update-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	// Clean up temp file on any error.
	defer func() {
		if tmpPath != "" {
			os.Remove(tmpPath)
		}
	}()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return fmt.Errorf("write binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}

	// Atomic rename.
	if err := os.Rename(tmpPath, binaryPath); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}

	tmpPath = "" // prevent deferred removal
	return nil
}
