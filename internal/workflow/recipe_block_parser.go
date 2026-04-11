package workflow

import (
	"regexp"
	"strings"
)

// Block represents one <block name="..."> child inside a recipe.md section.
// An empty Name denotes the preamble — content before the first block tag,
// which composeSection emits unconditionally when present.
type Block struct {
	Name string
	Body string
}

var (
	blockOpenRe  = regexp.MustCompile(`(?m)^<block name="([^"]+)">\s*$`)
	blockCloseRe = regexp.MustCompile(`(?m)^</block>\s*$`)
)

// ExtractBlocks parses a recipe.md <section> body for <block name="..."> tags
// and returns them as an ordered slice. Content before the first block tag
// becomes a synthetic block with Name == "" (the preamble). If the body has
// no block tags at all, the whole body is returned as a single preamble
// block — so callers that invoke this on a section not yet converted to
// blocks see verbatim passthrough.
//
// The parser is line-oriented and tolerant: malformed tags (no matching
// close, nested opens) are handled by falling through to the next open tag
// or EOF, never panicking. Unknown content inside a block is passed through
// verbatim — callers filter by name later.
func ExtractBlocks(sectionBody string) []Block {
	var blocks []Block

	matches := blockOpenRe.FindAllStringIndex(sectionBody, -1)
	if len(matches) == 0 {
		// No blocks — whole section is preamble.
		trimmed := strings.TrimSpace(sectionBody)
		if trimmed == "" {
			return nil
		}
		return []Block{{Name: "", Body: trimmed}}
	}

	firstOpen := matches[0][0]
	if firstOpen > 0 {
		preamble := strings.TrimSpace(sectionBody[:firstOpen])
		if preamble != "" {
			blocks = append(blocks, Block{Name: "", Body: preamble})
		}
	}

	for i, m := range matches {
		nameMatch := blockOpenRe.FindStringSubmatch(sectionBody[m[0]:m[1]])
		name := nameMatch[1]

		bodyStart := m[1]
		var bodyEnd int
		if i+1 < len(matches) {
			bodyEnd = matches[i+1][0]
		} else {
			bodyEnd = len(sectionBody)
		}

		bodyChunk := sectionBody[bodyStart:bodyEnd]
		// Strip the closing tag if present.
		if closeIdx := blockCloseRe.FindStringIndex(bodyChunk); closeIdx != nil {
			bodyChunk = bodyChunk[:closeIdx[0]]
		}

		blocks = append(blocks, Block{
			Name: name,
			Body: strings.TrimSpace(bodyChunk),
		})
	}

	return blocks
}
