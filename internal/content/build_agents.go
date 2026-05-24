package content

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/runtime"
)

// BuildAgentsMD composes the env-rendered AGENTS.md body from three
// embedded templates: agents_shared.md (env-agnostic body) plus exactly
// one env-specific preamble (agents_container.md or agents_local.md).
//
// Container preamble carries a {{.SelfHostname}} template var, resolved
// to rt.ServiceName at composition time. The composed output is wrapped
// in <!-- ZCP:BEGIN/END --> markers by the caller (init.generateAgentContext).
//
// Render is install-time: zcp init detects rt.InContainer and freezes
// the env into the disk file. Subsequent zcp serve runs use
// RefreshAgentContext to re-render the marked section in BOTH envs
// (local and container) so a long-lived install doesn't drift past the
// build's template version. Env is stable per install; if the install
// moves between envs, zcp init must be re-run to refresh AGENTS.md.
//
// AGENTS.md is the cross-tool canonical context file consumed by Codex,
// Cursor, Gemini, Antigravity, and ~17 other agents on the agents.md
// Linux Foundation standard. Claude Code consumes CLAUDE.md, which the
// Claude adapter writes as a thin @AGENTS.md include wrapper — so both
// agents see the same content from one source.
func BuildAgentsMD(rt runtime.Info) (string, error) {
	shared, err := GetTemplate("agents_shared.md")
	if err != nil {
		return "", fmt.Errorf("read agents_shared.md: %w", err)
	}

	var preamble string
	if rt.InContainer {
		tmpl, err := GetTemplate("agents_container.md")
		if err != nil {
			return "", fmt.Errorf("read agents_container.md: %w", err)
		}
		preamble = strings.ReplaceAll(tmpl, "{{.SelfHostname}}", rt.ServiceName)
	} else {
		tmpl, err := GetTemplate("agents_local.md")
		if err != nil {
			return "", fmt.Errorf("read agents_local.md: %w", err)
		}
		preamble = tmpl
	}

	return "# Zerops\n\n" +
		strings.TrimSpace(preamble) + "\n\n" +
		strings.TrimSpace(shared) + "\n", nil
}

// BuildClaudeMD is a deprecated alias for BuildAgentsMD. AGENTS.md
// became the canonical context file in the multi-agent migration
// (plans/multi-agent-container-support-2026-05-22.md); CLAUDE.md is
// now a thin @AGENTS.md wrapper. New code should call BuildAgentsMD.
//
// Deprecated: use BuildAgentsMD.
func BuildClaudeMD(rt runtime.Info) (string, error) {
	return BuildAgentsMD(rt)
}

// BuildClaudeWrapper returns the body content of CLAUDE.md's
// ZCP-managed section: an @AGENTS.md include that pulls the canonical
// body into Claude's context (Claude Code's native @-include syntax).
//
// The wrapper is intentionally minimal today — the corpus + boot shim
// content is agent-neutral after the genericization in commit d0f8a449,
// so there's nothing Claude-specific to add here. The deltas section is
// reserved for future Claude-only guidance (e.g. an explicit
// `run_in_background=true` example block) that would mislead non-Claude
// agents if it lived in AGENTS.md.
//
// Caller wraps the returned body in <!-- ZCP:BEGIN/END --> markers.
func BuildClaudeWrapper() string {
	return "@AGENTS.md\n"
}
