package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// HoldAsset installs an asset of the running release at dest. Only verified
// bytes are published, so a later boot can use the finished file offline.
func (c *Checker) HoldAsset(ctx context.Context, name, dest string) error {
	if _, err := os.Stat(dest); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read held %s: %w", name, err)
	}

	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	client := *c.HTTPClient
	client.Timeout = downloadTimeout
	target := c.assetURL(c.CurrentVersion, name)
	sum, err := assetChecksum(ctx, &client, target+".sha256")
	if err != nil {
		return fmt.Errorf("read %s checksum: %w", name, err)
	}
	body, err := openReleaseAsset(ctx, &client, target)
	if err != nil {
		return fmt.Errorf("download %s: %w", name, err)
	}
	defer body.Close()

	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create release directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, name+"-*")
	if err != nil {
		return fmt.Errorf("stage %s: %w", name, err)
	}
	defer os.Remove(tmp.Name())
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(tmp, hash), body)
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close staged %s: %w", name, err)
	}
	if copyErr != nil {
		return fmt.Errorf("write %s: %w", name, copyErr)
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), sum) {
		return fmt.Errorf("SHA-256 mismatch for %s of %s", name, c.CurrentVersion)
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return fmt.Errorf("set %s permissions: %w", name, err)
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return fmt.Errorf("hold %s: %w", name, err)
	}
	return nil
}

func assetChecksum(ctx context.Context, client *http.Client, target string) (string, error) {
	body, err := openReleaseAsset(ctx, client, target)
	if err != nil {
		return "", fmt.Errorf("open release checksum: %w", err)
	}
	defer body.Close()
	raw, err := io.ReadAll(io.LimitReader(body, 1024))
	if err != nil {
		return "", fmt.Errorf("read checksum body: %w", err)
	}
	fields := strings.Fields(string(raw))
	if len(fields) == 0 {
		return "", errors.New("empty release checksum")
	}
	if sum, err := hex.DecodeString(fields[0]); err != nil || len(sum) != sha256.Size {
		return "", errors.New("invalid release SHA-256 checksum")
	}
	return fields[0], nil
}

func openReleaseAsset(ctx context.Context, client *http.Client, target string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("make release asset request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch release asset: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("release asset answered HTTP %d", resp.StatusCode)
	}
	return resp.Body, nil
}
