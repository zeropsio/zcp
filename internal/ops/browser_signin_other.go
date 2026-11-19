//go:build !unix

package ops

import (
	"fmt"
	"os"
)

// readBrowserFile reads a kept sign-in file, refusing a symlink in its
// place; this platform has no open flag that refuses one.
func readBrowserFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("browser state: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("browser state: %s is not a regular file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("browser state: %w", err)
	}
	return data, nil
}

// writeBrowserFile replaces a kept sign-in file's content, 0600, refusing a
// symlink in its place.
func writeBrowserFile(path string, data []byte) error {
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("browser state: %s is not a regular file", path)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("browser state: %w", err)
	}
	return nil
}
