package topology

import (
	"regexp"
	"slices"
	"strings"
)

// sensitiveNameParts are the key fragments that make a name read as a
// secret (DefaultSensitive).
var sensitiveNameParts = []string{"SECRET", "TOKEN", "KEY", "PASSWORD", "PASS", "DSN", "PRIVATE", "CREDENTIAL"}

// DefaultSensitive is the name rule: the flag a value gets when nobody says.
// A key whose name reads as a secret (SECRET, TOKEN, KEY, PASSWORD, PASS,
// DSN, PRIVATE, CREDENTIAL, any case) is sensitive, any other plain — but a
// name ReadableByDesign stays plain.
func DefaultSensitive(key string) bool {
	if ReadableByDesign(key) {
		return false
	}
	upper := strings.ToUpper(key)
	for _, part := range sensitiveNameParts {
		if strings.Contains(upper, part) {
			return true
		}
	}
	return false
}

// ReadableByDesign reports the two secret-shaped names that stay plain unless
// someone says otherwise, even when zcp generates their value:
//   - a name public by design — PUBLIC or PUBLISHABLE a word of it and no
//     SECRET, PASSWORD or PRIVATE beside it — because a browser bundle ships
//     its value anyway;
//   - a password a person signs in with — ADMIN or SUPERADMIN beside
//     PASSWORD or PASS — because a sensitive value is write-only and the
//     vault is where the person finds it (Mate hides it on screen).
func ReadableByDesign(key string) bool {
	words := strings.FieldsFunc(strings.ToUpper(key), func(r rune) bool { return r == '_' || r == '-' || r == '.' })
	has := func(set ...string) bool {
		return slices.ContainsFunc(words, func(word string) bool { return slices.Contains(set, word) })
	}
	if has("PUBLIC", "PUBLISHABLE") && !has("SECRET", "PASSWORD", "PRIVATE") {
		return true
	}
	return has("ADMIN", "SUPERADMIN") && has("PASSWORD", "PASS")
}

// referenceOnly matches a value made only of `${name}` references.
var referenceOnly = regexp.MustCompile(`^(\$\{[^}]+\})+$`)

// IsReferenceOnly reports a value made only of `${name}` references: wiring
// that holds no secret of its own (`DB_PASSWORD: ${db_password}`).
func IsReferenceOnly(value string) bool {
	return referenceOnly.MatchString(strings.TrimSpace(value))
}

// DefaultSensitiveValue is the flag a value gets when nobody says: plain for
// wiring (IsReferenceOnly), so the agent can still read how a value is wired,
// else the name rule (DefaultSensitive).
func DefaultSensitiveValue(key, value string) bool {
	return !IsReferenceOnly(value) && DefaultSensitive(key)
}
