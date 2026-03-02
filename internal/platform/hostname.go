package platform

import (
	"fmt"
	"regexp"
)

var hostnameRe = regexp.MustCompile(`^[a-z][a-z0-9]{0,24}$`)

// ValidateHostname checks that a hostname matches Zerops constraints.
// Must start with a lowercase letter (a-z), followed by lowercase letters/digits, max 25 chars.
// Verified: Zerops API rejects hostnames starting with a digit, uppercase, hyphens, underscores.
func ValidateHostname(hostname string) *PlatformError {
	if hostname == "" {
		return NewPlatformError(ErrInvalidHostname, "hostname is empty",
			"Hostname must be 1-25 lowercase letters/digits, starting with a letter")
	}
	if !hostnameRe.MatchString(hostname) {
		return NewPlatformError(ErrInvalidHostname,
			fmt.Sprintf("invalid hostname %q", hostname),
			"Hostname must be 1-25 lowercase letters/digits (a-z, 0-9), starting with a letter (a-z)")
	}
	return nil
}
