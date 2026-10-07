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

// ReadableByDesign reports a secret-shaped name that stays plain unless
// someone says otherwise, even when zcp generates its value: a name public by
// design — PUBLIC or PUBLISHABLE a word of it and no SECRET, PASSWORD or
// PRIVATE beside it — because a browser bundle ships its value anyway.
// Nothing else needs to: a sensitive value is hidden, not lost — Mate's vault
// shows it to the person who asks (Zerops reveals it to their personal access
// token, which is always in sudo mode; measured 2026-10-07), so an admin's
// sign-in password is sensitive like any other.
func ReadableByDesign(key string) bool {
	words := strings.FieldsFunc(strings.ToUpper(key), func(r rune) bool { return r == '_' || r == '-' || r == '.' })
	has := func(set ...string) bool {
		return slices.ContainsFunc(words, func(word string) bool { return slices.Contains(set, word) })
	}
	return has("PUBLIC", "PUBLISHABLE") && !has("SECRET", "PASSWORD", "PRIVATE")
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
