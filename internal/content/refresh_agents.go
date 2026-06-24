package content

import (
	"fmt"
	"os"
	"strings"

	"github.com/zeropsio/zcp/internal/runtime"
)

// Managed-section markers — invisible HTML comments in rendered
// markdown but exact textual anchors for the upsert logic. Identical
// to internal/init's mdMarker* — duplicated here on purpose so
// content/ stays self-contained (the dependency rule is content/ <-
// init/ <- server/, never the other way).
const (
	agentMarkerBegin = "<!-- ZCP:BEGIN -->"
	agentMarkerEnd   = "<!-- ZCP:END -->"
)

// RefreshAgentContext refreshes the ZCP-managed section in both
// AGENTS.md (canonical body) and CLAUDE.md (thin @AGENTS.md wrapper)
// to the current embedded template content. Idempotent on per-file
// basis: when the on-disk managed section already matches what would
// be freshly composed for rt, the file is left untouched.
//
// Returns (agentsChanged, claudeChanged, err). Used by the MCP server
// at startup in BOTH envs (local and container) so a long-lived
// install doesn't drift past the build's template version.
//
// Per-file semantics:
//
//   - File missing → returns false for that file (zcp init owns
//     first-write; this helper is incremental refresh only).
//   - File exists without ZCP:BEGIN/END markers → returns false
//     (legacy shape; zcp init's migration path handles in-place
//     upgrade, this helper has no anchor).
//   - Managed section already matches → returns false.
//   - Managed section rewritten → returns true.
//
// Content outside the markers (REFLOG entries, user additions) is
// preserved verbatim.
//
// Backward-compat safeguard: when AGENTS.md is MISSING (pre-upgrade
// install that hasn't yet run multi-agent `zcp init`), CLAUDE.md is
// left untouched. Without this, serve startup would rewrite the
// old full-body CLAUDE.md into a thin @AGENTS.md wrapper that points
// at a file that doesn't exist — Claude would silently lose all
// workflow doctrine until the operator runs `zcp init`. `zcp init`
// is the only place that owns the migration (writes AGENTS.md AND
// rewrites CLAUDE.md atomically), so serve never partially-migrates.
func RefreshAgentContext(agentsPath, claudePath string, rt runtime.Info, guided bool) (agentsChanged, claudeChanged bool, err error) {
	agentsBody, buildErr := BuildAgentsMD(rt, guided)
	if buildErr != nil {
		return false, false, buildErr
	}
	agentsBlock := wrapManagedBlock(agentsBody)

	agentsChanged, err = refreshManagedFile(agentsPath, agentsBlock)
	if err != nil {
		return agentsChanged, false, fmt.Errorf("refresh AGENTS.md: %w", err)
	}

	// If AGENTS.md doesn't exist, do NOT refresh CLAUDE.md — a
	// pre-upgrade Claude user has the doctrine in CLAUDE.md's managed
	// section, and rewriting it to a wrapper would orphan an
	// @AGENTS.md include with no target file. Wait for `zcp init` to
	// migrate atomically.
	if _, statErr := os.Stat(agentsPath); os.IsNotExist(statErr) {
		return agentsChanged, false, nil
	}

	claudeBlock := wrapManagedBlock(BuildClaudeWrapper())
	claudeChanged, err = refreshManagedFile(claudePath, claudeBlock)
	if err != nil {
		return agentsChanged, claudeChanged, fmt.Errorf("refresh CLAUDE.md: %w", err)
	}

	return agentsChanged, claudeChanged, nil
}

// RefreshClaudeMD is a deprecated alias for RefreshAgentContext that
// refreshes only CLAUDE.md and ignores AGENTS.md. New callers should
// use RefreshAgentContext to refresh both files atomically.
//
// Deprecated: use RefreshAgentContext. Guided is always off on this path —
// new guided installs go through RefreshAgentContext, which resolves it.
func RefreshClaudeMD(path string, rt runtime.Info) (refreshed bool, err error) {
	body, err := BuildAgentsMD(rt, false)
	if err != nil {
		return false, err
	}
	return refreshManagedFile(path, wrapManagedBlock(body))
}

// wrapManagedBlock returns the body wrapped in ZCP:BEGIN/END markers
// with deterministic line endings.
func wrapManagedBlock(body string) string {
	return agentMarkerBegin + "\n" + strings.TrimRight(body, "\n") + "\n" + agentMarkerEnd + "\n"
}

// refreshManagedFile rewrites the ZCP:BEGIN/END section of path with
// block when it differs. Soft-fails (returns false, nil) on missing
// file or legacy/malformed marker shape — those are zcp init's job
// to migrate.
func refreshManagedFile(path, block string) (bool, error) {
	existing, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read %s: %w", path, err)
	}

	text := string(existing)
	beginIdx := strings.Index(text, agentMarkerBegin)
	if beginIdx < 0 {
		return false, nil
	}
	endRel := strings.Index(text[beginIdx+len(agentMarkerBegin):], agentMarkerEnd)
	if endRel < 0 {
		return false, nil
	}
	endIdx := beginIdx + len(agentMarkerBegin) + endRel

	endLineEnd := endIdx + len(agentMarkerEnd)
	if endLineEnd < len(text) && text[endLineEnd] == '\n' {
		endLineEnd++
	}
	if text[beginIdx:endLineEnd] == block {
		return false, nil
	}

	newText := text[:beginIdx] + block + text[endLineEnd:]
	if err := os.WriteFile(path, []byte(newText), 0o644); err != nil { //nolint:gosec // G306: managed config file
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return true, nil
}
