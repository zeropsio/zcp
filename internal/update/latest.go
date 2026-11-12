package update

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// Latest is the newest published zcp release: the tag GitHub's
// releases/latest names, which is the release install.sh installs, read from
// that source on every call. An error means the release is unknown
// (offline, refused), and the caller never guesses one.
func (c *Checker) Latest(ctx context.Context) (string, error) {
	tag, err := c.fetchLatestTag(ctx)
	if err != nil {
		return "", fmt.Errorf("latest zcp release: %w", err)
	}
	return tag, nil
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
