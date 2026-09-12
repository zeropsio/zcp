package console

import (
	"encoding/base64"
	"strings"
)

// safeFragment returns a stable browser-safe fragment for identity. Existing
// simple identities retain their exact spelling. When normalization replaces
// any character, a reversible encoding of the complete source identity keeps
// otherwise identical readable slugs distinct. The '~' separator cannot occur
// in an unchanged identity, so encoded and preserved fragments are disjoint.
func safeFragment(prefix, identity string) string {
	var normalized strings.Builder
	normalized.Grow(len(prefix) + len(identity)*2 + 1)
	normalized.WriteString(prefix)
	changed := false
	for _, r := range identity {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			normalized.WriteRune(r)
		default:
			normalized.WriteByte('-')
			changed = true
		}
	}
	if !changed {
		return normalized.String()
	}
	normalized.WriteByte('~')
	normalized.WriteString(base64.RawURLEncoding.EncodeToString([]byte(identity)))
	return normalized.String()
}
