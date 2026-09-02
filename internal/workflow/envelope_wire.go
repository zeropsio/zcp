package workflow

import (
	"encoding/json"
	"strings"
)

// EnvelopeFence is the info string of the fenced code block that carries a
// StateEnvelope inside a tool result's text. It is deliberately a two-word
// info string: `json` keeps the block syntax-highlighted and ignorable for a
// human reader, `zcp-envelope` is the selector a machine reducer matches on.
//
// The envelope rides in the TEXT rather than in MCP's `structuredContent`
// because Claude Code replaces the model-facing tool result with
// `structuredContent` when it is present, which would strip every atom of
// guidance a workflow result renders. Both Claude Code and Codex forward the
// text block verbatim. Contract: docs/spec-mate.md, "Envelope on the wire";
// guard: tools.TestNoStructuredContentOnToolResults.
const EnvelopeFence = "json zcp-envelope"

const (
	envelopeOpenFence  = "```" + EnvelopeFence
	envelopeCloseFence = "```"
)

// AppendEnvelope returns text with env serialized as a trailing
// `json zcp-envelope` block. The envelope is compact single-line JSON, so the
// block is three lines and the whole result stays greppable line-by-line.
//
// The result ends with exactly one such block: when text already ends with
// one (a producer that appended, then re-rendered), the trailing block is
// REPLACED rather than duplicated. A block embedded earlier in the text is
// content, not structure, and is left alone — the reducer's "last block wins"
// rule resolves it.
//
// A marshal failure returns text unchanged. An envelope is an optional
// machine-readable addendum; it must never fail or alter the tool's own
// result.
func AppendEnvelope(text string, env StateEnvelope) string {
	payload, err := json.Marshal(env)
	if err != nil {
		return text
	}

	if start, ok := trailingEnvelopeBlock(text); ok {
		text = text[:start]
	}
	text = strings.TrimRight(text, "\n")

	var b strings.Builder
	b.Grow(len(text) + len(payload) + len(envelopeOpenFence) + 8)
	b.WriteString(text)
	if text != "" {
		b.WriteString("\n\n")
	}
	b.WriteString(envelopeOpenFence)
	b.WriteByte('\n')
	b.Write(payload)
	b.WriteByte('\n')
	b.WriteString(envelopeCloseFence)
	b.WriteByte('\n')
	return b.String()
}

// EnvelopeJSONField is the top-level key a JSON-document tool result carries
// its StateEnvelope under. Most mutating tools answer with one JSON document
// rather than prose; a trailing markdown fence would stop that parsing as
// JSON, so those results carry the envelope as a sibling key instead.
const EnvelopeJSONField = "envelope"

// ExtractEnvelope reads the StateEnvelope back out of a tool result's text.
// It implements the reducer contract a mate client follows in TypeScript, over
// the two carriers a result may use:
//
//  1. JSON document — the whole text parses as a JSON object; the envelope is
//     its top-level `envelope` key. Used by the mutating tools.
//  2. Prose — the envelope is the LAST complete fenced `json zcp-envelope`
//     block (a transcript may concatenate several tool results; the newest
//     state is the last one). Used by the lifecycle-status carriers.
//
// The JSON carrier is tried first: a prose result never parses as an object,
// and trying it first means a fence appearing inside one of the document's
// string values (a captured log tail, say) cannot outrank the real envelope.
//
// Returns ok=false when neither carrier yields one — including a JSON result
// whose envelope computation failed and therefore omitted the key, and a
// fenced block that is unterminated or does not parse. A malformed envelope
// is ignored rather than treated as state.
func ExtractEnvelope(text string) (StateEnvelope, bool) {
	if env, ok := extractJSONFieldEnvelope(text); ok {
		return env, true
	}
	return extractFencedEnvelope(text)
}

// extractJSONFieldEnvelope handles carrier 1: the text is one JSON object
// carrying the envelope under EnvelopeJSONField.
func extractJSONFieldEnvelope(text string) (StateEnvelope, bool) {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "{") {
		return StateEnvelope{}, false
	}
	var doc struct {
		Envelope *StateEnvelope `json:"envelope"`
	}
	if err := json.Unmarshal([]byte(trimmed), &doc); err != nil || doc.Envelope == nil {
		return StateEnvelope{}, false
	}
	return *doc.Envelope, true
}

// extractFencedEnvelope handles carrier 2: the last complete fenced block.
func extractFencedEnvelope(text string) (StateEnvelope, bool) {
	for open := len(text); ; {
		idx := lastFenceOpen(text[:open])
		if idx < 0 {
			return StateEnvelope{}, false
		}
		body, _, ok := envelopeBlockBody(text, idx)
		if !ok {
			// Unterminated block: keep looking further back.
			open = idx
			continue
		}
		var env StateEnvelope
		if err := json.Unmarshal([]byte(body), &env); err != nil {
			return StateEnvelope{}, false
		}
		return env, true
	}
}

// trailingEnvelopeBlock reports the byte offset at which the envelope block
// that terminates text begins, if text ends with one (trailing whitespace
// allowed). Used by AppendEnvelope to replace rather than duplicate.
func trailingEnvelopeBlock(text string) (int, bool) {
	idx := lastFenceOpen(text)
	if idx < 0 {
		return 0, false
	}
	_, end, ok := envelopeBlockBody(text, idx)
	if !ok || strings.TrimSpace(text[end:]) != "" {
		return 0, false
	}
	return idx, true
}

// lastFenceOpen returns the offset of the last line that consists solely of
// the opening fence, or -1. The match is line-anchored: a fence MENTIONED
// mid-line (prose describing the format) is text, not structure.
func lastFenceOpen(text string) int {
	for search := text; ; {
		idx := strings.LastIndex(search, envelopeOpenFence)
		if idx < 0 {
			return -1
		}
		if atLineStart(text, idx) && restOfLineBlank(text, idx+len(envelopeOpenFence)) {
			return idx
		}
		search = search[:idx]
	}
}

// envelopeBlockBody returns the trimmed body of the block whose opening fence
// starts at openIdx, plus the offset just past its closing fence. ok is false
// when the block has no closing fence.
func envelopeBlockBody(text string, openIdx int) (body string, blockEnd int, ok bool) {
	nl := strings.IndexByte(text[openIdx:], '\n')
	if nl < 0 {
		return "", 0, false
	}
	bodyStart := openIdx + nl + 1

	for pos := bodyStart; pos <= len(text); {
		lineEnd, next := lineBounds(text, pos)
		if strings.TrimRight(text[pos:lineEnd], " \t\r") == envelopeCloseFence {
			return strings.TrimSpace(text[bodyStart:pos]), min(next, len(text)), true
		}
		if next > len(text) {
			break
		}
		pos = next
	}
	return "", 0, false
}

// lineBounds returns the end offset of the line starting at pos (exclusive of
// its newline) and the offset of the next line. next exceeds len(text) when
// the line is the last one and unterminated.
func lineBounds(text string, pos int) (lineEnd, next int) {
	if i := strings.IndexByte(text[pos:], '\n'); i >= 0 {
		return pos + i, pos + i + 1
	}
	return len(text), len(text) + 1
}

func atLineStart(text string, idx int) bool {
	return idx == 0 || text[idx-1] == '\n'
}

// restOfLineBlank reports whether everything from idx to the end of its line
// is horizontal whitespace — i.e. nothing follows the info string.
func restOfLineBlank(text string, idx int) bool {
	lineEnd, _ := lineBounds(text, idx)
	return strings.TrimRight(text[idx:lineEnd], " \t\r") == ""
}
