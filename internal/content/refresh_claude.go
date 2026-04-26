package content

import (
	"fmt"
	"os"
	"strings"

	"github.com/zeropsio/zcp/internal/runtime"
)

// CLAUDE.md managed-section markers — invisible HTML comments in
// rendered markdown but exact textual anchors for the upsert logic.
// Identical to internal/init's mdMarker* — duplicated here on purpose so
// content/ stays self-contained (the dependency rule is content/ <-
// init/ <- server/, never the other way).
const (
	claudeMarkerBegin = "<!-- ZCP:BEGIN -->"
	claudeMarkerEnd   = "<!-- ZCP:END -->"
)

// RefreshClaudeMD writes the current embedded CLAUDE.md content to path
// when the on-disk managed section differs from what would be freshly
// composed for rt. Idempotent:
//
//   - Returns (false, nil) when the file is missing — `zcp init` owns
//     first-write; this helper is incremental refresh only.
//   - Returns (false, nil) when the file exists without ZCP:BEGIN/END
//     markers (legacy shape) — leave for `zcp init` to migrate.
//   - Returns (false, nil) when the managed section already matches.
//   - Returns (true, nil) when the file was rewritten.
//
// The marked section preserves any content outside the markers (REFLOG
// entries, user additions). Used by the MCP server at startup in
// container env so a long-lived container doesn't drift past the
// build's template version (G9 — pre-fix the deployed CLAUDE.md was
// stamped only at the last `zcp init`, leaving stale wording any
// description-drift lint would refuse to release today).
func RefreshClaudeMD(path string, rt runtime.Info) (refreshed bool, err error) {
	existing, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read CLAUDE.md: %w", err)
	}

	body, err := BuildClaudeMD(rt)
	if err != nil {
		return false, err
	}
	block := claudeMarkerBegin + "\n" + strings.TrimRight(body, "\n") + "\n" + claudeMarkerEnd + "\n"

	text := string(existing)
	beginIdx := strings.Index(text, claudeMarkerBegin)
	if beginIdx < 0 {
		// Legacy file without markers — `zcp init` migrates these in
		// place. Don't touch from the serve path; we have no anchor
		// for an idempotent rewrite.
		return false, nil
	}
	// End marker MUST appear after begin marker — anything else
	// (reversed, missing end, end-before-begin) is a malformed file.
	// Bail out without touching it; otherwise a slice with low > high
	// panics during MCP server startup.
	endRel := strings.Index(text[beginIdx+len(claudeMarkerBegin):], claudeMarkerEnd)
	if endRel < 0 {
		return false, nil
	}
	endIdx := beginIdx + len(claudeMarkerBegin) + endRel

	endLineEnd := endIdx + len(claudeMarkerEnd)
	if endLineEnd < len(text) && text[endLineEnd] == '\n' {
		endLineEnd++
	}
	if text[beginIdx:endLineEnd] == block {
		return false, nil
	}

	newText := text[:beginIdx] + block + text[endLineEnd:]
	if err := os.WriteFile(path, []byte(newText), 0o644); err != nil { //nolint:gosec // G306: managed config file
		return false, fmt.Errorf("write CLAUDE.md: %w", err)
	}
	return true, nil
}
