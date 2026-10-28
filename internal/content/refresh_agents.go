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
// AGENTS.md (canonical body) and CLAUDE.md (the @AGENTS.md wrapper with
// the repositories' imports, RefreshClaudeWrapper) to the current
// embedded template content. Idempotent on per-file
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
func RefreshAgentContext(agentsPath, claudePath string, rt runtime.Info, guided bool, repos []string) (agentsChanged, claudeChanged bool, err error) {
	agentsChanged, err = RefreshAgentsMD(agentsPath, rt, guided)
	if err != nil {
		return agentsChanged, false, err
	}
	claudeChanged, err = RefreshClaudeWrapper(agentsPath, claudePath, rt, repos)
	return agentsChanged, claudeChanged, err
}

// RefreshAgentsMD refreshes AGENTS.md's ZCP-managed section alone, to the
// body BuildAgentsMD composes for rt and guided. Same per-file semantics as
// RefreshAgentContext.
func RefreshAgentsMD(agentsPath string, rt runtime.Info, guided bool) (bool, error) {
	agentsBody, err := BuildAgentsMD(rt, guided)
	if err != nil {
		return false, err
	}
	changed, err := refreshManagedFile(agentsPath, wrapManagedBlock(agentsBody))
	if err != nil {
		return changed, fmt.Errorf("refresh AGENTS.md: %w", err)
	}
	return changed, nil
}

// RefreshClaudeWrapper refreshes CLAUDE.md's ZCP-managed section alone:
// the @AGENTS.md include and, where WorkspaceImports holds, one include per
// repository in repos (BuildClaudeWrapper). zcp serve runs it at start
// (through RefreshAgentContext) and after every tool call — where pairs are
// bootstrapped, adopted, stood up, moved onto a pool or deleted — so the
// next session reads the list current (docs/spec-workflows.md §5.1). Same
// per-file semantics as RefreshAgentContext; content outside the markers is
// preserved verbatim.
//
// If AGENTS.md doesn't exist, CLAUDE.md is NOT refreshed — a pre-upgrade
// Claude user has the doctrine in CLAUDE.md's managed section, and
// rewriting it to a wrapper would orphan an @AGENTS.md include with no
// target file. `zcp init` migrates atomically.
func RefreshClaudeWrapper(agentsPath, claudePath string, rt runtime.Info, repos []string) (bool, error) {
	if _, statErr := os.Stat(agentsPath); os.IsNotExist(statErr) {
		return false, nil
	}
	changed, err := refreshManagedFile(claudePath, wrapManagedBlock(BuildClaudeWrapper(rt, repos)))
	if err != nil {
		return changed, fmt.Errorf("refresh CLAUDE.md: %w", err)
	}
	return changed, nil
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
// to migrate. Markers count only when they occupy an entire line
// (IndexMarkerLine) — a mid-line mention in prose is content.
func refreshManagedFile(path, block string) (bool, error) {
	existing, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read %s: %w", path, err)
	}

	text := string(existing)
	beginIdx := IndexMarkerLine(text, agentMarkerBegin, 0)
	if beginIdx < 0 {
		return false, nil
	}
	endIdx := IndexMarkerLine(text, agentMarkerEnd, beginIdx+len(agentMarkerBegin))
	if endIdx < 0 {
		return false, nil
	}

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
