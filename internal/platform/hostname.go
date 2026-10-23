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

// leaseAliasRe is the shape of a dev-pool lease alias: <pool>-<container
// number>. The pool part is a platform hostname; the hyphen keeps an alias from
// ever equalling one. The alias names a file, an ssh Host and a mount path, so
// nothing outside this shape may reach any of them.
var leaseAliasRe = regexp.MustCompile(`^[a-z][a-z0-9]{0,39}-[0-9]+$`)

// containerHostnameRe is the hostname a pool container answers ssh on.
var containerHostnameRe = regexp.MustCompile(`^node-id-[0-9]+\.runtime\.[a-z0-9]+\.zerops$`)

// ValidateLeaseAlias checks a dev-pool lease alias (<pool>-<number>).
func ValidateLeaseAlias(alias string) *PlatformError {
	if !leaseAliasRe.MatchString(alias) {
		return NewPlatformError(ErrInvalidHostname,
			fmt.Sprintf("invalid lease alias %q", alias),
			"A lease alias is <pool hostname>-<container number>, e.g. apppool-3")
	}
	return nil
}

// ValidateContainerHostname checks the hostname of a pool container
// (node-id-<n>.runtime.<pool>.zerops). It is rendered into root-read ssh
// config, so it is checked strictly and never trusted from a record.
func ValidateContainerHostname(hostname string) *PlatformError {
	if !containerHostnameRe.MatchString(hostname) {
		return NewPlatformError(ErrInvalidHostname,
			fmt.Sprintf("invalid pool container hostname %q", hostname),
			"A pool container answers on node-id-<n>.runtime.<pool>.zerops")
	}
	return nil
}

// ValidateMountName checks the name of an sshfs mount: a service hostname or a
// lease alias.
func ValidateMountName(name string) *PlatformError {
	if leaseAliasRe.MatchString(name) {
		return nil
	}
	return ValidateHostname(name)
}
