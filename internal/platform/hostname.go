package platform

import (
	"fmt"
	"regexp"
)

var hostnameRe = regexp.MustCompile(`^[a-z][a-z0-9]{0,39}$`)

// ValidateHostname checks that a hostname matches Zerops constraints.
// Must start with a lowercase letter (a-z), followed by lowercase letters/digits, max 40 chars.
// E2E verified 2026-04-02: API accepts up to 40 chars (41 rejected). JSON schema description
// says 25 but has no maxLength constraint — actual enforcement is 40.
func ValidateHostname(hostname string) *PlatformError {
	if hostname == "" {
		return NewPlatformError(ErrInvalidHostname, "hostname is empty",
			"Hostname must be 1-40 lowercase letters/digits, starting with a letter")
	}
	if !hostnameRe.MatchString(hostname) {
		return NewPlatformError(ErrInvalidHostname,
			fmt.Sprintf("invalid hostname %q", hostname),
			"Hostname must be 1-40 lowercase letters/digits (a-z, 0-9), starting with a letter (a-z)")
	}
	return nil
}
