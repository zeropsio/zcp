package update

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// LatestOptions steers how Latest reaches the release source.
type LatestOptions struct {
	// Refresh asks the release source even while the cache is fresh; a
	// source that fails then keeps the fresh cached answer.
	Refresh bool
	// CacheOnly reads the cache alone and never reaches the network.
	CacheOnly bool
}

// Latest is the newest published zcp release: the tag GitHub's
// releases/latest names, which is the release install.sh installs. A fresh
// cache entry (CacheTTL) answers without the network; otherwise the release
// source does, and its answer is cached for the next reader, Check included.
// An error means the release is unknown (offline, rate-limited, nothing
// fresh cached under CacheOnly), and the caller never guesses one.
func (c *Checker) Latest(ctx context.Context, opts LatestOptions) (string, error) {
	if !opts.Refresh || opts.CacheOnly {
		if cached, ok := c.readCache(); ok && cached.LatestVersion != "" {
			return cached.LatestVersion, nil
		}
		if opts.CacheOnly {
			return "", errors.New("latest zcp release: nothing fresh is cached")
		}
	}

	release, err := c.fetchLatestRelease(ctx)
	if err == nil && release.TagName == "" {
		err = errors.New("the release names no tag")
	}
	if err != nil {
		if opts.Refresh {
			if cached, ok := c.readCache(); ok && cached.LatestVersion != "" {
				return cached.LatestVersion, nil
			}
		}
		return "", fmt.Errorf("latest zcp release: %w", err)
	}

	c.writeCache(cacheEntry{
		CheckedAt:     time.Now(),
		LatestVersion: release.TagName,
		DownloadURL:   c.downloadURL(release.TagName),
	})
	return release.TagName, nil
}

// IsRelease reports whether v names a published release: major.minor.patch,
// with an optional leading "v". A dev build ("dev"), a git-describe build
// (v9.220.0-3-gabc1234-dirty) or a bare commit is not one.
func IsRelease(v string) bool {
	_, ok := releaseParts(v)
	return ok
}

// Behind reports whether installed is a release older than latest — what a
// restart, which installs the latest release, would change. A build no
// release names is never behind: a restart would replace a build someone
// placed on purpose. Nor is anything behind an unknown or unparsable latest.
func Behind(installed, latest string) bool {
	have, ok := releaseParts(installed)
	if !ok {
		return false
	}
	want, ok := releaseParts(latest)
	if !ok {
		return false
	}
	for i := range have {
		if have[i] != want[i] {
			return have[i] < want[i]
		}
	}
	return false
}

// releaseParts is v's major, minor and patch when IsRelease(v).
func releaseParts(v string) ([3]int, bool) {
	var parts [3]int
	fields := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(fields) != len(parts) {
		return parts, false
	}
	for i, field := range fields {
		if field == "" || strings.Trim(field, "0123456789") != "" {
			return parts, false
		}
		n, err := strconv.Atoi(field)
		if err != nil {
			return parts, false
		}
		parts[i] = n
	}
	return parts, true
}
