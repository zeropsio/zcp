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

// copyFile overwrites dst with contents of src, preserving dst's path.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("open target: %w", err)
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("copy: %w", err)
	}
	return out.Close()
}

// CanWrite checks if the process can create files in dir.
func CanWrite(dir string) bool {
	f, err := os.CreateTemp(dir, ".zcp-write-test-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}

// Apply downloads the new binary and atomically replaces the current one.
// If info.Available is false, this is a no-op.
func Apply(ctx context.Context, info *Info, binaryPath string, client *http.Client) error {
	if !info.Available {
		return nil
	}

	if client == nil {
		client = &http.Client{Timeout: downloadTimeout}
	}

	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
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
	// If the directory is not writable (e.g. /usr/local/bin/ as non-root),
	// fall back to os.TempDir() and copy-based replacement.
	dir := filepath.Dir(binaryPath)
	tmp, err := os.CreateTemp(dir, "zcp-update-*")
	sameFS := err == nil
	if !sameFS {
		tmp, err = os.CreateTemp("", "zcp-update-*")
	}
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

	if sameFS {
		// Atomic rename (same filesystem).
		if err := os.Rename(tmpPath, binaryPath); err != nil {
			return fmt.Errorf("replace binary: %w", err)
		}
		tmpPath = "" // prevent deferred removal
		return nil
	}

	// Cross-filesystem fallback: overwrite binary in place.
	if err := copyFile(tmpPath, binaryPath); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}
	return nil
}
