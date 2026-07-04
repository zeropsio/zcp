package content

import "strings"

// IndexMarkerLine returns the byte offset of the first occurrence of
// marker at or after from in text that occupies an ENTIRE line — i.e.
// preceded by start-of-text or '\n' and followed by end-of-text, '\n',
// or "\r\n". Returns -1 when no such occurrence exists.
//
// This is the single owner of marker-matching semantics for every
// ZCP-managed section and REFLOG lookup (init upsert/migration, serve
// refresh). Line anchoring is load-bearing: a literal marker string
// appearing MID-LINE is prose (an agent documenting ZCP behavior in
// CLAUDE.md writes exactly that), not structure. A raw strings.Index/
// strings.Contains match on such a mention makes the rewrite cut the
// sentence at the mention and swallow or relocate everything up to the
// next marker match — corrupted lines ending in `-->` on a real user
// install. Never match these markers with raw substring search.
//
// from is only a search start; anchoring is always validated against
// the full text, so callers may pass mid-line offsets safely.
func IndexMarkerLine(text, marker string, from int) int {
	if from < 0 {
		from = 0
	}
	for i := from; i+len(marker) <= len(text); {
		j := strings.Index(text[i:], marker)
		if j < 0 {
			return -1
		}
		pos := i + j
		if markerLineAnchored(text, pos, len(marker)) {
			return pos
		}
		i = pos + 1
	}
	return -1
}

// markerLineAnchored reports whether text[pos:pos+n] starts at a line
// start and is followed only by the line terminator (or end-of-text).
func markerLineAnchored(text string, pos, n int) bool {
	if pos > 0 && text[pos-1] != '\n' {
		return false
	}
	end := pos + n
	switch {
	case end == len(text):
		return true
	case text[end] == '\n':
		return true
	case text[end] == '\r' && (end+1 == len(text) || text[end+1] == '\n'):
		return true
	}
	return false
}
